package httptransport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"guiforcores/bridge/auth"
	"guiforcores/bridge/config"
	"guiforcores/bridge/event"
	"guiforcores/bridge/kernel"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	"guiforcores/gen/native/daemon"
	"guiforcores/gen/native/daemon/daemonconnect"
	profilev1 "guiforcores/gen/profile/v1"

	"connectrpc.com/connect"
)

// A real platform.Service launches the current executable as its core guard.
// Serve that command in this test executable too, so the acceptance test uses
// the production process ownership and shutdown path.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == platform.CoreGuardCommand {
		if err := platform.RunCoreGuard(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type nativeLifecycleProfileReader struct{ profile *profilev1.Profile }

func (r nativeLifecycleProfileReader) FindByID(string) (*profilev1.Profile, error) {
	return r.profile, nil
}

func nativeWaitForStreamCancellation[T any](t *testing.T, stream *connect.ServerStreamForClient[T]) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		for stream.Receive() {
		}
		done <- stream.Err()
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		stream.Close()
		t.Fatal("production native stream survived lifecycle cancellation")
	}
}

func TestNativeAPIProductionLifecycle(t *testing.T) {
	binary := os.Getenv("SINGBOX_NATIVE_TEST_BINARY")
	if binary == "" {
		t.Skip("set SINGBOX_NATIVE_TEST_BINARY to the official sing-box 1.14.2 executable")
	}
	paths := storage.NewPaths(t.TempDir())
	filename := "sing-box"
	if runtime.GOOS == "windows" {
		filename += ".exe"
	}
	corePath := paths.Resolve(filepath.Join("data", "sing-box", filename))
	if err := os.MkdirAll(filepath.Dir(corePath), 0700); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	destination, err := os.OpenFile(corePath, os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		t.Fatal(err)
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
	appConfig, err := config.NewStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	configService := config.NewService(paths, "test")
	authService := auth.NewService(paths)
	if err := authService.SetSecret("application-secret"); err != nil {
		t.Fatal(err)
	}
	authService.AddSession("browser-session")
	events := event.NewHub(authService)
	processes := platform.NewService(paths, events, platform.Environment{})
	profile := &profilev1.Profile{Id: "native-lifecycle", Name: "Lifecycle"}
	core := kernel.NewService(processes, configService, appConfig, nativeLifecycleProfileReader{profile}, events)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := core.Close(ctx); err != nil {
			t.Errorf("cleanup production core: %v", err)
		}
	})
	lifecycleContext, lifecycleCancel := context.WithCancel(context.Background())
	server := &Server{options: Options{Auth: authService, Kernel: core}, lifecycleContext: lifecycleContext, lifecycleCancel: lifecycleCancel}
	protected := http.NewServeMux()
	protected.HandleFunc("/api/kernel/", server.handleKernelProxy)
	proxy := httptest.NewServer(server.buildRootHandler(http.NotFoundHandler(), protected))
	server.server = proxy.Config
	defer proxy.Close()
	client := daemonconnect.NewStartedServiceClient(proxy.Client(), proxy.URL+"/api/kernel", connect.WithGRPCWeb())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := func() {
		if _, err := core.StartCore(ctx, connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: profile.Id})); err != nil {
			t.Fatal(err)
		}
	}
	subscribe := func() *connect.ServerStreamForClient[daemon.Status] {
		stream, err := client.SubscribeStatus(ctx, nativeRequest(&daemon.SubscribeStatusRequest{Interval: int64(time.Second)}))
		nativeFirst(t, stream, err)
		return stream
	}
	instanceSecret := func() string {
		_, _, secret, cancel, err := core.NativeAPIContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		return secret
	}
	start()
	oldSecret := instanceSecret()
	first := subscribe()
	if _, err := core.StopCore(ctx, connect.NewRequest(&kernelv1.StopCoreRequest{})); err != nil {
		t.Fatal(err)
	}
	nativeWaitForStreamCancellation(t, first)
	first.Close()
	start()
	if instanceSecret() == oldSecret {
		t.Fatal("production restart reused old credential")
	}
	second := subscribe()
	if _, err := core.RestartCore(ctx, connect.NewRequest(&kernelv1.RestartCoreRequest{ProfileId: profile.Id})); err != nil {
		t.Fatal(err)
	}
	nativeWaitForStreamCancellation(t, second)
	second.Close()
	third := subscribe()
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	if err := server.Close(shutdownContext); err != nil {
		t.Fatal(err)
	}
	nativeWaitForStreamCancellation(t, third)
	third.Close()
	if status, _ := core.Status(); status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
		t.Fatal("HTTP Close unexpectedly stopped kernel")
	}
}
