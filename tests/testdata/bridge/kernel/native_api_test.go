package kernel

import (
	"archive/zip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	appv1 "guiforcores/gen/app/v1"
	kernelv1 "guiforcores/gen/kernel/v1"
	daemon "guiforcores/gen/native/daemon"
	"guiforcores/gen/native/daemon/daemonconnect"
	profilev1 "guiforcores/gen/profile/v1"
)

type nativeHandlerTransport struct{ handler http.Handler }

func (t nativeHandlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	return recorder.Result(), nil
}

func TestNativeAPIReadinessUsesAuthenticatedGRPCWeb(t *testing.T) {
	for _, apiVersion := range []int32{3, 4} {
		t.Run(fmt.Sprint(apiVersion), func(t *testing.T) {
			mux := http.NewServeMux()
			modeCalls := 0
			mux.Handle(daemonconnect.StartedServiceGetVersionProcedure, connect.NewUnaryHandler(daemonconnect.StartedServiceGetVersionProcedure, func(_ context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[daemon.Version], error) {
				if req.Header().Get("Authorization") != "Bearer instance-secret" {
					t.Errorf("missing instance credential")
				}
				if req.Peer().Protocol != connect.ProtocolGRPCWeb {
					t.Errorf("protocol=%q", req.Peer().Protocol)
				}
				return connect.NewResponse(&daemon.Version{Version: "1.14.2", ApiVersion: apiVersion}), nil
			}))
			mux.Handle(daemonconnect.StartedServiceGetClashModeStatusProcedure, connect.NewUnaryHandler(daemonconnect.StartedServiceGetClashModeStatusProcedure, func(_ context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[daemon.ClashModeStatus], error) {
				modeCalls++
				if req.Header().Get("Authorization") != "Bearer instance-secret" {
					t.Errorf("missing mode credential")
				}
				return connect.NewResponse(&daemon.ClashModeStatus{}), nil
			}))
			client := &http.Client{Transport: nativeHandlerTransport{handler: mux}}
			err := waitKernelAPIReadyWithClient(context.Background(), "127.0.0.1:20123", "instance-secret", os.Getpid(), time.Second, client)
			if apiVersion < 4 {
				if err == nil || modeCalls != 0 {
					t.Fatalf("accepted incompatible API: err=%v, modeCalls=%d", err, modeCalls)
				}
			} else if err != nil || modeCalls != 1 {
				t.Fatalf("ready API: err=%v, modeCalls=%d", err, modeCalls)
			}
		})
	}
}

func startNativeTestInstance(t *testing.T, s *Service, pid int, secret string) {
	t.Helper()
	if err := s.setStarting("profile"); err != nil {
		t.Fatal(err)
	}
	s.updateCoreState(func() { s.corePID = pid })
	if !s.completeStart(pid, "profile", &profilev1.Profile{Id: "profile"}, secret) {
		t.Fatal("instance did not start")
	}
}

func TestNativeAPIInstanceCancellationAndRotation(t *testing.T) {
	for _, action := range []string{"stop", "crash", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			s := NewService(fakeProcesses{}, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
			defer s.lifecycleCancel()
			startNativeTestInstance(t, s, 11, "first-secret")
			ctx, target, secret, cancel, err := s.NativeAPIContext(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer cancel()
			if target != "http://"+config.CoreAPIController || secret != "first-secret" {
				t.Fatalf("target=%q credential=%q", target, secret)
			}
			switch action {
			case "stop":
				if _, err := s.StopCore(context.Background(), connect.NewRequest(&kernelv1.StopCoreRequest{})); err != nil {
					t.Fatal(err)
				}
			case "crash":
				s.handleCoreProcessExit(11, fmt.Errorf("exit"))
			case "shutdown":
				s.BeginShutdown()
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("old instance request survived termination")
			}
			if _, _, _, _, err := s.NativeAPIContext(context.Background()); err == nil {
				t.Fatal("obtained stopped instance")
			}
			if action == "shutdown" {
				return
			}
			startNativeTestInstance(t, s, 12, "second-secret")
			_, _, secret, newCancel, err := s.NativeAPIContext(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer newCancel()
			if secret != "second-secret" || s.nativeGeneration != 2 {
				t.Fatal("new instance retained old credential/generation")
			}
		})
	}
}

type preflightProcesses struct {
	fakeProcesses
	version               string
	checkError            string
	checks, starts, kills int
}

func (p *preflightProcesses) Exec(_ string, args []string, _ platform.ExecOptions) platform.Result {
	if args[0] == "version" {
		return platform.Result{Flag: true, Data: "sing-box version " + p.version}
	}
	p.checks++
	return platform.Result{Flag: p.checkError == "", Data: p.checkError}
}
func (p *preflightProcesses) ExecBackground(string, []string, string, platform.ExecOptions) platform.Result {
	p.starts++
	return platform.Result{Flag: true, Data: "12"}
}
func (p *preflightProcesses) KillProcess(int, int) platform.Result {
	p.kills++
	return platform.Result{Flag: true}
}

func TestRestartPreflightDoesNotStopRunningCore(t *testing.T) {
	for _, test := range []struct{ name, version, checkError string }{{"old version", "1.14.1", ""}, {"unknown version", "custom", ""}, {"invalid config", "1.14.2", "invalid configuration"}} {
		t.Run(test.name, func(t *testing.T) {
			processes := &preflightProcesses{version: test.version, checkError: test.checkError}
			s := NewService(processes, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
			defer s.lifecycleCancel()
			startNativeTestInstance(t, s, 11, "running-secret")
			if _, err := s.RestartCore(context.Background(), connect.NewRequest(&kernelv1.RestartCoreRequest{ProfileId: "profile"})); err == nil {
				t.Fatal("invalid restart accepted")
			}
			if processes.kills != 0 || processes.starts != 0 {
				t.Fatalf("preflight failure killed/started process: %#v", processes)
			}
			_, _, secret, cancel, err := s.NativeAPIContext(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer cancel()
			if secret != "running-secret" {
				t.Fatal("preflight changed running credential")
			}
		})
	}
}

func TestRollbackRejectsIncompatibleCandidateBeforeMutation(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, coreFilePathForBranch(appv1.KernelBranch_KERNEL_BRANCH_MAIN))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", []byte("backup"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &preflightProcesses{fakeProcesses: fakeProcesses{resolveBase: base}, version: "1.14.1"}
	s := NewService(p, &fakeGenerator{}, fakeConfig{value: config.AppConfig{Branch: "main"}}, &fakeProfiles{}, fakeEvents{})
	defer s.lifecycleCancel()
	startNativeTestInstance(t, s, 11, "secret")
	if _, err := s.RollbackCore(context.Background(), connect.NewRequest(&kernelv1.RollbackCoreRequest{})); err == nil {
		t.Fatal("old backup accepted")
	}
	if p.kills != 0 {
		t.Fatal("stopped current process")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "current" {
		t.Fatal("replaced current binary")
	}
}

func TestInstallRejectsIncompatibleCandidateBeforeReplacingFiles(t *testing.T) {
	base := t.TempDir()
	cache := t.TempDir()
	archive := filepath.Join(cache, "candidate.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("candidate/" + getKernelFileName(false))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("downloaded binary")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, coreFilePathForBranch(appv1.KernelBranch_KERNEL_BRANCH_MAIN))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", []byte("backup"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &preflightProcesses{fakeProcesses: fakeProcesses{resolveBase: base}, version: "1.14.1"}
	s := NewService(p, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	defer s.lifecycleCancel()
	if err := s.installCoreArchive(context.Background(), archive, cache, appv1.KernelBranch_KERNEL_BRANCH_MAIN); err == nil {
		t.Fatal("incompatible candidate accepted")
	}
	data, _ := os.ReadFile(path)
	backup, _ := os.ReadFile(path + ".bak")
	if string(data) != "current" || string(backup) != "backup" {
		t.Fatal("version check changed existing binaries")
	}
}
