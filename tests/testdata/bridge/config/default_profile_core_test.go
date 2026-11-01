package config

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/storage"
	"guiforcores/gen/native/daemon"
	"guiforcores/gen/native/daemon/daemonconnect"
	profilev1 "guiforcores/gen/profile/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"
)

func defaultQuickStartProfile(t *testing.T) *profilev1.Profile {
	t.Helper()
	path := os.Getenv("WEBUI_DEFAULT_PROFILE_FIXTURE")
	if path == "" {
		path = filepath.Join("..", "..", "tests", "fixtures", "router-default-profile.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	profile := new(profilev1.Profile)
	if err := protojson.Unmarshal(data, profile); err != nil {
		t.Fatal(err)
	}
	return profile
}

func defaultProfileSubscription(t *testing.T, root string, node map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "data", "subscribes"), 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := "- id: fixture-subscription\n  proxies:\n    - id: fixture-node-id\n      tag: fixture-node\n"
	if err := os.WriteFile(filepath.Join(root, "data", "subscribes.yaml"), []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal([]any{node})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, subscriptionContentPath("fixture-subscription")), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestQuickStartDefaultProfileGeneration(t *testing.T) {
	root := t.TempDir()
	defaultProfileSubscription(t, root, map[string]any{"type": "http", "tag": "fixture-node", "server": "127.0.0.1", "server_port": 1080})
	generated, err := NewService(storage.NewPaths(root), "test").Generate(defaultQuickStartProfile(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]map[string]any{}
	for _, value := range generated["outbounds"].([]any) {
		group := value.(map[string]any)
		groups[group["tag"].(string)] = group
	}
	for tag, want := range map[string][]any{
		"outbound.select":   {"outbound.urltest", "fixture-node"},
		"outbound.urltest":  {"fixture-node"},
		"outbound.fallback": {"outbound.select", "outbound.direct"},
		"GLOBAL":            {"outbound.select", "outbound.urltest", "outbound.direct", "outbound.block", "outbound.fallback"},
	} {
		if !reflect.DeepEqual(groups[tag]["outbounds"], want) {
			t.Fatalf("%s lost its default/subscription bindings: got %#v, want %#v", tag, groups[tag], want)
		}
	}
	route := generated["route"].(map[string]any)
	if route["final"] != "outbound.fallback" || len(route["rule_set"].([]any)) != 6 {
		t.Fatalf("default routing changed: %#v", route)
	}
}

// The frontend test executes real QuickStart/defaults/store/RPC conversion and
// verifies this fixture byte-for-byte. This test then uses the real Go generator
// and released core. Only transport endpoints are isolated: TUN becomes a
// loopback mixed inbound, DNS IDs resolve fixture names through hosts, rule-set
// URLs point at six local fixtures, and URLTest uses a local health endpoint.
// The default groups, bindings, rule order/actions, and route final stay intact.
func TestQuickStartDefaultProfileRuleGlobalAndCacheWithCore(t *testing.T) {
	corePath := os.Getenv("SING_BOX_TEST_BINARY")
	if corePath == "" {
		t.Skip("set SING_BOX_TEST_BINARY to run the local default-profile data-path test")
	}
	root := t.TempDir()
	profile := defaultQuickStartProfile(t)
	var targetRequests, targetProxyRequests, unexpectedProxyRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
		_, _ = io.WriteString(w, "default-profile:"+r.Host)
	}))
	t.Cleanup(target.Close)
	targetAddress := target.Listener.Addr().String()
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(health.Close)

	ruleFixtures := map[string]string{
		"Category-Ads":    `{"domain":["ads.default.test"]}`,
		"GeoSite-Private": `{"domain":["private.default.test"]}`,
		"GeoSite-CN":      `{"domain":["cn.default.test"]}`,
		// Documentation-only networks avoid classifying the local test target
		// as private while retaining all six default rule-set references.
		"GeoIP-Private":   `{"ip_cidr":["192.0.2.0/24"]}`,
		"GeoIP-CN":        `{"ip_cidr":["198.51.100.0/24"]}`,
		"GeoLocation-!CN": `{"domain":["foreign.default.test"]}`,
	}
	downloads := map[string]*atomic.Int64{}
	for tag := range ruleFixtures {
		downloads[tag] = new(atomic.Int64)
	}
	rules := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tag := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".json")
		body, ok := ruleFixtures[tag]
		if !ok {
			http.NotFound(w, r)
			return
		}
		downloads[tag].Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"version":3,"rules":[%s]}`, body)
	}))
	t.Cleanup(rules.Close)
	allowed := map[string]bool{targetAddress: true, health.Listener.Addr().String(): true, rules.Listener.Addr().String(): true}
	proxy := defaultProfileProxy(t, allowed, targetAddress, &targetProxyRequests, &unexpectedProxyRequests)
	proxyHost, proxyPort, _ := net.SplitHostPort(proxy.Listener.Addr().String())
	port, _ := strconv.Atoi(proxyPort)
	defaultProfileSubscription(t, root, map[string]any{"type": "http", "tag": "fixture-node", "server": proxyHost, "server_port": port})

	proxyAddress := defaultProfileUnusedAddress(t)
	_, mixedPortText, _ := net.SplitHostPort(proxyAddress)
	mixedPort, _ := strconv.Atoi(mixedPortText)
	for _, inbound := range profile.Inbounds {
		inbound.Enable = inbound.GetType() == profilev1.InboundType_INBOUND_TYPE_MIXED
		if inbound.Enable {
			inbound.Mixed.Listen.Listen = "127.0.0.1"
			inbound.Mixed.Listen.ListenPort = int32(mixedPort)
		}
	}
	for _, group := range profile.Outbounds {
		if group.GetType() == profilev1.OutboundType_OUTBOUND_TYPE_URLTEST {
			group.Url = health.URL + "/generate_204"
		}
	}
	for _, server := range profile.Dns.Servers {
		if server.GetType() == profilev1.DnsServerType_DNS_SERVER_TYPE_FAKEIP {
			continue
		}
		// DNS transport availability is independent of the routing regression.
		// Retain IDs so all default DNS/route references are still resolved.
		server.Type = profilev1.DnsServerType_DNS_SERVER_TYPE_HOSTS
		server.Detour, server.DomainResolver, server.Server, server.ServerPort = "", "", "", ""
		server.Predefined = map[string]string{}
		for _, domain := range []string{"cn", "foreign", "unmatched", "ads", "private"} {
			server.Predefined[domain+".default.test"] = "127.0.0.1"
		}
	}
	for _, ruleSet := range profile.Route.RuleSet {
		if _, ok := ruleFixtures[ruleSet.Tag]; !ok {
			t.Fatalf("new default rule set needs a local fixture: %q", ruleSet.Tag)
		}
		ruleSet.Url = rules.URL + "/" + ruleSet.Tag + ".json"
		ruleSet.Format = profilev1.RulesetFormat_RULESET_FORMAT_SOURCE
		ruleSet.UpdateInterval = "24h"
	}
	cachePath := filepath.Join(root, "cache.db")
	profile.Experimental.CacheFile.Path = cachePath
	previousController := CoreAPIController
	CoreAPIController = defaultProfileUnusedAddress(t)
	t.Cleanup(func() { CoreAPIController = previousController })
	generated, err := NewService(storage.NewPaths(root), "test").Generate(profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(generated, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	secret := NativeAPISecret(generated)
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	client := daemonconnect.NewStartedServiceClient(&http.Client{Transport: transport}, "http://"+CoreAPIController, connect.WithGRPCWeb())
	setMode := func(mode string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := client.SetClashMode(ctx, defaultProfileNativeRequest(&daemon.ClashMode{Mode: mode}, secret)); err != nil {
			t.Fatal(err)
		}
		status, err := client.GetClashModeStatus(ctx, defaultProfileNativeRequest(&emptypb.Empty{}, secret))
		if err != nil || !strings.EqualFold(status.Msg.GetCurrentMode(), mode) {
			t.Fatalf("mode not applied: status=%v error=%v", status, err)
		}
	}
	checkPath := func(domain string, wantProxy, wantSuccess bool, logs *httpClientCoreLog) {
		t.Helper()
		beforeTarget, beforeProxy := targetRequests.Load(), targetProxyRequests.Load()
		err := requestDefaultProfileTarget(proxyAddress, targetAddress, domain+".default.test")
		wantTargetRequests := int64(0)
		if wantSuccess {
			wantTargetRequests = 1
		}
		if (err == nil) != wantSuccess || targetRequests.Load()-beforeTarget != wantTargetRequests {
			t.Fatalf("%s success=%v, want %v, target requests=%d: %v\n%s", domain, err == nil, wantSuccess, targetRequests.Load()-beforeTarget, err, logs.String())
		}
		if got := targetProxyRequests.Load() - beforeProxy; (got == 1) != wantProxy || got > 1 {
			t.Fatalf("%s used proxy %d times, want proxy=%v\n%s", domain, got, wantProxy, logs.String())
		}
	}

	stop, logs := startRouterRulesCore(t, corePath, configPath)
	for tag, count := range downloads {
		if count.Load() != 1 {
			t.Fatalf("default rule set %s downloads=%d, want 1\n%s", tag, count.Load(), logs.String())
		}
	}
	setMode("rule")
	checkPath("cn", false, true, logs)
	checkPath("foreign", true, true, logs)
	checkPath("unmatched", true, true, logs)
	checkPath("ads", false, false, logs)
	setMode("global")
	for _, domain := range []string{"cn", "foreign", "unmatched", "ads"} {
		checkPath(domain, true, true, logs)
	}
	stop()
	if info, err := os.Stat(cachePath); err != nil || info.Size() == 0 {
		t.Fatalf("default cache was not persisted: %v", err)
	}
	rules.Close()
	stop, logs = startRouterRulesCore(t, corePath, configPath)
	setMode("rule")
	checkPath("cn", false, true, logs)
	checkPath("foreign", true, true, logs)
	checkPath("unmatched", true, true, logs)
	stop()
	for tag, count := range downloads {
		if count.Load() != 1 {
			t.Fatalf("cached rule set %s was downloaded again: %d", tag, count.Load())
		}
	}
	if unexpectedProxyRequests.Load() != 0 {
		t.Fatalf("core attempted %d unexpected proxy destinations", unexpectedProxyRequests.Load())
	}
	t.Log("real QuickStart protobuf: all 6 rule sets fetched; Rule CN=direct/foreign+final=proxy; Global=proxy; offline rule-cache restart passed")
}

func defaultProfileNativeRequest[T any](message *T, secret string) *connect.Request[T] {
	request := connect.NewRequest(message)
	request.Header().Set("Authorization", "Bearer "+secret)
	return request
}

func defaultProfileUnusedAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func defaultProfileProxy(t *testing.T, allowed map[string]bool, target string, targetRequests, unexpected *atomic.Int64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || !allowed[r.Host] {
			unexpected.Add(1)
			http.Error(w, "only fixture CONNECT destinations are allowed", http.StatusBadRequest)
			return
		}
		if r.Host == target {
			targetRequests.Add(1)
		}
		upstream, err := net.DialTimeout("tcp", r.Host, 3*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		connection, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(15 * time.Second))
		_ = upstream.SetDeadline(time.Now().Add(15 * time.Second))
		if _, err := fmt.Fprint(buffered, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		if err := buffered.Flush(); err != nil {
			return
		}
		go func() {
			_, _ = io.Copy(upstream, buffered)
			_ = upstream.Close()
			_ = connection.Close()
		}()
		_, _ = io.Copy(connection, upstream)
	}))
	t.Cleanup(server.Close)
	return server
}

func requestDefaultProfileTarget(proxyAddress, target, host string) error {
	connection, err := net.DialTimeout("tcp", proxyAddress, 3*time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(connection, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); err != nil {
		return err
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return fmt.Errorf("CONNECT: %s", response.Status)
	}
	if _, err := fmt.Fprintf(connection, "GET /data-path HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", host); err != nil {
		return err
	}
	response, err = http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || string(body) != "default-profile:"+host {
		return fmt.Errorf("target response: %s %q", response.Status, body)
	}
	return nil
}
