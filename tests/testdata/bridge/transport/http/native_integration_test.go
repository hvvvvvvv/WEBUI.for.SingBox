package httptransport

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/config"
	"guiforcores/bridge/storage"
	"guiforcores/gen/native/daemon"
	"guiforcores/gen/native/daemon/daemonconnect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

func nativeRequest[T any](message *T) *connect.Request[T] {
	req := connect.NewRequest(message)
	req.Header().Set("Authorization", "Bearer browser-session")
	return req
}

func nativeFirst[T any](t *testing.T, stream *connect.ServerStreamForClient[T], err error) *T {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Receive() {
		t.Fatalf("native stream ended before a message: %v", stream.Err())
	}
	return stream.Msg()
}

// This opt-in acceptance test uses the released binary, never a Go dependency
// on sing-box. All traffic except the native single-node URLTest stays local.
func TestNativeAPIRealCore(t *testing.T) {
	binary := os.Getenv("SINGBOX_NATIVE_TEST_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_NATIVE_TEST_BINARY to the official sing-box 1.14.2 executable")
	}
	directory := t.TempDir()
	var tests atomic.Int32
	trafficServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/204" {
			tests.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Write([]byte("connection-start\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer trafficServer.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mixedPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	root := map[string]any{
		"log": map[string]any{"level": "debug"},
		"experimental": map[string]any{
			"clash_api":  map[string]any{"external_controller": "0.0.0.0:9090"},
			"cache_file": map[string]any{"enabled": true, "path": "cache.db"},
		},
		"inbounds": []any{map[string]any{"type": "mixed", "tag": "mixed", "listen": "127.0.0.1", "listen_port": mixedPort}},
		"outbounds": []any{
			map[string]any{"type": "selector", "tag": "group", "outbounds": []any{"direct-a", "direct-b"}},
			map[string]any{"type": "urltest", "tag": "auto", "outbounds": []any{"direct-a", "direct-b"}, "url": trafficServer.URL + "/204", "interval": "1m"},
			map[string]any{"type": "direct", "tag": "direct-a"},
			map[string]any{"type": "direct", "tag": "direct-b"},
		},
		"route": map[string]any{"final": "group", "rules": []any{
			map[string]any{"clash_mode": "Global", "outbound": "group"},
			map[string]any{"clash_mode": "Direct", "outbound": "direct-a"},
			map[string]any{"clash_mode": "Work", "outbound": "group"},
		}},
	}
	type runningCore struct {
		secret string
		ctx    context.Context
		cancel context.CancelFunc
		cmd    *exec.Cmd
		log    *os.File
	}
	var current atomic.Pointer[runningCore]
	stop := func() {
		if core := current.Swap(nil); core != nil {
			core.cancel()
			core.cmd.Process.Signal(os.Interrupt)
			done := make(chan struct{})
			go func() { core.cmd.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				core.cmd.Process.Kill()
				<-done
			}
			core.log.Close()
		}
	}
	defer stop()
	start := func() {
		if err := config.EnforceNativeAPIConfig(root); err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(root)
		if strings.Contains(string(payload), "clash_api") {
			t.Fatal("Clash API remains in the runtime config")
		}
		configPath := filepath.Join(directory, "config.json")
		if err := os.WriteFile(configPath, payload, 0600); err != nil {
			t.Fatal(err)
		}
		check := exec.Command(binary, "check", "-c", configPath)
		check.Dir = directory
		if out, err := check.CombinedOutput(); err != nil {
			t.Fatalf("real core config check: %s: %v", out, err)
		}
		logFile, err := os.Create(filepath.Join(directory, "core.log"))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		core := &runningCore{secret: config.NativeAPISecret(root), ctx: ctx, cancel: cancel, log: logFile}
		core.cmd = exec.Command(binary, "run", "-c", configPath)
		core.cmd.Dir = directory
		core.cmd.Stdout, core.cmd.Stderr = logFile, logFile
		if err := core.cmd.Start(); err != nil {
			cancel()
			logFile.Close()
			t.Fatal(err)
		}
		current.Store(core)
		direct := daemonconnect.NewStartedServiceClient(http.DefaultClient, "http://"+config.CoreAPIController, connect.WithGRPCWeb())
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			req := connect.NewRequest(&emptypb.Empty{})
			req.Header().Set("Authorization", "Bearer "+core.secret)
			version, err := direct.GetVersion(ctx, req)
			cancel()
			if err == nil && version.Msg.GetApiVersion() >= 4 && version.Msg.GetVersion() == "1.14.2" {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		out, _ := os.ReadFile(filepath.Join(directory, "core.log"))
		t.Fatalf("real core did not become ready: %s", out)
	}
	start()
	authService := auth.NewService(storage.NewPaths(directory))
	authService.SetSecret("application-secret")
	authService.AddSession("browser-session")
	server := &Server{options: Options{Auth: authService}}
	target, _ := url.Parse("http://" + config.CoreAPIController)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/kernel/", func(w http.ResponseWriter, r *http.Request) {
		core := current.Load()
		if core == nil {
			http.Error(w, "stopped", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		stop := context.AfterFunc(core.ctx, cancel)
		defer stop()
		proxyNativeAPI(w, r.WithContext(ctx), target, core.secret, func() bool {
			return authService.ValidateSessionWithoutTouch("browser-session")
		})
	})
	proxy := httptest.NewServer(server.buildRootHandler(http.NotFoundHandler(), mux))
	defer proxy.Close()
	client := daemonconnect.NewStartedServiceClient(proxy.Client(), proxy.URL+"/api/kernel", connect.WithGRPCWeb())
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	mode, err := client.GetClashModeStatus(ctx, nativeRequest(&emptypb.Empty{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(mode.Msg.ModeList, ","), "Work") {
		t.Fatalf("custom mode missing: %v", mode.Msg)
	}
	modeStream, err := client.SubscribeClashMode(ctx, nativeRequest(&emptypb.Empty{}))
	nativeFirst(t, modeStream, err)
	defer modeStream.Close()
	if _, err := client.SetClashMode(ctx, nativeRequest(&daemon.ClashMode{Mode: "Work"})); err != nil {
		t.Fatal(err)
	}
	if !modeStream.Receive() || modeStream.Msg().Mode != "Work" {
		t.Fatalf("mode subscription did not update: %v", modeStream.Err())
	}
	groupStream, err := client.SubscribeGroups(ctx, nativeRequest(&emptypb.Empty{}))
	initialGroups := nativeFirst(t, groupStream, err)
	defer groupStream.Close()
	if len(initialGroups.Group) != 2 || !initialGroups.Group[0].Selectable {
		t.Fatalf("native groups unavailable: %v", initialGroups)
	}
	if _, err := client.SelectOutbound(ctx, nativeRequest(&daemon.SelectOutboundRequest{GroupTag: "group", OutboundTag: "direct-b"})); err != nil {
		t.Fatal(err)
	}
	for groupStream.Receive() {
		if groupStream.Msg().Group[0].Selected == "direct-b" {
			break
		}
	}
	if groupStream.Err() != nil {
		t.Fatal(groupStream.Err())
	}
	outboundStream, err := client.SubscribeOutbounds(ctx, nativeRequest(&emptypb.Empty{}))
	nativeFirst(t, outboundStream, err)
	outboundStream.Close()
	if _, err := client.URLTest(ctx, nativeRequest(&daemon.URLTestRequest{OutboundTag: "auto"})); err != nil {
		t.Fatal(err)
	}
	if _, err := client.URLTest(ctx, nativeRequest(&daemon.URLTestRequest{OutboundTag: "direct-a"})); err != nil {
		t.Fatal(err)
	}
	for groupStream.Receive() {
		var updated bool
		for _, group := range groupStream.Msg().Group {
			for _, item := range group.Items {
				updated = updated || item.UrlTestTime > 0
			}
		}
		if updated && tests.Load() > 0 {
			break
		}
	}
	if groupStream.Err() != nil || tests.Load() == 0 {
		t.Fatalf("asynchronous URLTest did not publish results: %v", groupStream.Err())
	}
	connections, err := client.SubscribeConnections(ctx, nativeRequest(&daemon.SubscribeConnectionsRequest{Interval: int64(time.Second)}))
	first := nativeFirst(t, connections, err)
	defer connections.Close()
	if !first.GetReset_() {
		t.Fatal("connection stream did not begin with reset")
	}
	proxyURL, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(mixedPort))
	trafficTransport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer trafficTransport.CloseIdleConnections()
	trafficClient := &http.Client{Transport: trafficTransport, Timeout: 10 * time.Second}
	response, err := trafficClient.Get(trafficServer.URL + "/held")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var connectionID string
	for connections.Receive() {
		for _, event := range connections.Msg().Events {
			if event.Connection != nil && strings.Contains(event.Connection.Destination, strings.TrimPrefix(trafficServer.URL, "http://")) && event.Connection.ClosedAt == 0 {
				connectionID = event.Id
			}
		}
		if connectionID != "" {
			break
		}
	}
	if connectionID == "" {
		t.Fatalf("native connection event missing: %v", connections.Err())
	}
	statusStream, err := client.SubscribeStatus(ctx, nativeRequest(&daemon.SubscribeStatusRequest{Interval: int64(time.Second)}))
	status := nativeFirst(t, statusStream, err)
	statusStream.Close()
	if status.Memory == 0 || !status.TrafficAvailable || status.ConnectionsIn < 1 {
		t.Fatalf("native statistics missing: %v", status)
	}
	logStream, err := client.SubscribeLog(ctx, nativeRequest(&emptypb.Empty{}))
	logs := nativeFirst(t, logStream, err)
	logStream.Close()
	if !logs.GetReset_() || len(logs.Messages) == 0 {
		t.Fatalf("native log batch missing: %v", logs)
	}
	if _, err := client.CloseConnection(ctx, nativeRequest(&daemon.CloseConnectionRequest{Id: connectionID})); err != nil {
		t.Fatal(err)
	}
	for connections.Receive() {
		var closed bool
		for _, event := range connections.Msg().Events {
			closed = closed || event.Id == connectionID && event.Type == daemon.ConnectionEventType_CONNECTION_EVENT_CLOSED
		}
		if closed {
			break
		}
	}
	if connections.Err() != nil {
		t.Fatal(connections.Err())
	}
	if _, err := client.CloseAllConnections(ctx, nativeRequest(&emptypb.Empty{})); err != nil {
		t.Fatal(err)
	}
	oldSecret := current.Load().secret
	stop()
	if groupStream.Receive() {
		t.Fatal("old instance continued delivering group frames after stop")
	}
	start()
	if current.Load().secret == oldSecret {
		t.Fatal("restart reused native API secret")
	}
	mode, err = client.GetClashModeStatus(ctx, nativeRequest(&emptypb.Empty{}))
	if err != nil || mode.Msg.CurrentMode != "Work" {
		t.Fatalf("mode cache did not survive restart: %v, %v", mode, err)
	}
	groupsAfterRestart, err := client.SubscribeGroups(ctx, nativeRequest(&emptypb.Empty{}))
	groups := nativeFirst(t, groupsAfterRestart, err)
	groupsAfterRestart.Close()
	if groups.Group[0].Selected != "direct-b" {
		t.Fatalf("selection cache did not survive restart: %v", groups)
	}
	connectionsAfterRestart, err := client.SubscribeConnections(ctx, nativeRequest(&daemon.SubscribeConnectionsRequest{Interval: int64(time.Second)}))
	if !nativeFirst(t, connectionsAfterRestart, err).GetReset_() {
		t.Fatal("restarted connection stream did not reset")
	}
	connectionsAfterRestart.Close()
	response.Body.Close()
}
