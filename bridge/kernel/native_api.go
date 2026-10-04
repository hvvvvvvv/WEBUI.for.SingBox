package kernel

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	kernelv1 "guiforcores/gen/kernel/v1"
	"guiforcores/gen/native/daemon/daemonconnect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

type nativeAPIInstance struct {
	pid        int
	generation uint64
	secret     string
	ctx        context.Context
	cancel     context.CancelFunc
}

// NativeAPIContext obtains the credential and cancellation scope of exactly one
// running instance. Callers must invoke cancel when their HTTP request finishes.
// A stale request is canceled before another core can use the same listen port.
func (s *Service) NativeAPIContext(parent context.Context) (context.Context, string, string, context.CancelFunc, error) {
	s.mu.Lock()
	instance := s.nativeAPI
	if s.closing || s.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING || instance == nil || instance.pid != s.corePID || instance.ctx.Err() != nil {
		s.mu.Unlock()
		return nil, "", "", nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("native kernel API is not running"))
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(instance.ctx, cancel)
	secret := instance.secret
	s.mu.Unlock()
	return ctx, "http://" + config.CoreAPIController, secret, func() { stop(); cancel() }, nil
}

// Must be called while s.mu is held.
func (s *Service) revokeNativeAPILocked() {
	if s.nativeAPI != nil {
		s.nativeAPI.cancel()
		s.nativeAPI = nil
	}
}

func (s *Service) installNativeAPILocked(pid int, secret string) {
	s.revokeNativeAPILocked()
	s.nativeGeneration++
	ctx, cancel := context.WithCancel(s.lifecycleCtx)
	s.nativeAPI = &nativeAPIInstance{pid: pid, generation: s.nativeGeneration, secret: secret, ctx: ctx, cancel: cancel}
}

func waitKernelAPIReady(ctx context.Context, controller, secret string, pid int, timeout time.Duration) error {
	return waitKernelAPIReadyWithClient(ctx, controller, secret, pid, timeout, &http.Client{Timeout: 1200 * time.Millisecond})
}

func waitKernelAPIReadyWithClient(ctx context.Context, controller, secret string, pid int, timeout time.Duration, httpClient connect.HTTPClient) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := daemonconnect.NewStartedServiceClient(httpClient, "http://"+controller, connect.WithGRPCWeb())
	proc, _ := os.FindProcess(pid)
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("waiting for native kernel API: %w (last error: %v)", err, lastErr)
		}
		if proc != nil {
			alive, err := platform.IsProcessAlive(proc)
			if err == nil && !alive {
				return fmt.Errorf("core process %s exited before native API ready: %v", strconv.Itoa(pid), lastErr)
			}
		}
		versionReq := connect.NewRequest(&emptypb.Empty{})
		versionReq.Header().Set("Authorization", "Bearer "+secret)
		version, err := client.GetVersion(ctx, versionReq)
		if err == nil {
			if version.Msg.GetApiVersion() < 4 {
				return fmt.Errorf("native API version %d is unsupported; require API version 4 or newer", version.Msg.GetApiVersion())
			}
			modeReq := connect.NewRequest(&emptypb.Empty{})
			modeReq.Header().Set("Authorization", "Bearer "+secret)
			_, err = client.GetClashModeStatus(ctx, modeReq)
			if err == nil {
				return nil
			}
		}
		lastErr = err
		if connect.CodeOf(err) == connect.CodeUnauthenticated || connect.CodeOf(err) == connect.CodePermissionDenied || connect.CodeOf(err) == connect.CodeUnimplemented {
			return fmt.Errorf("native API compatibility or authentication check failed: %w", err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(250 * time.Millisecond):
		}
	}
}
