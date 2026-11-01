package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"connectrpc.com/connect"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/rpcutil"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

// Immutable snapshot of what successfully started, not of the subsequently
// editable profile/config.json. JSON includes the instance's native API secret.
// Rollback must not rerun plugins, reload subscriptions, or rotate that secret.
type coreStartSnapshot struct {
	profile *profilev1.Profile
	runtime kernelRuntimeConfig
	path    string
	config  []byte
}

func cloneKernelRuntime(cfg kernelRuntimeConfig) kernelRuntimeConfig {
	cfg.Args = slices.Clone(cfg.Args)
	cfg.Env = maps.Clone(cfg.Env)
	return cfg
}

func newCoreStartSnapshot(profile *profilev1.Profile, prepared *preparedCoreStart) (*coreStartSnapshot, error) {
	data, err := json.Marshal(prepared.config)
	if err != nil {
		return nil, fmt.Errorf("snapshot running core configuration: %w", err)
	}
	return &coreStartSnapshot{profile: cloneProfile(profile), runtime: cloneKernelRuntime(prepared.runtime), path: prepared.path, config: data}, nil
}

func (s *coreStartSnapshot) prepared() (*preparedCoreStart, error) {
	var generated map[string]any
	decoder := json.NewDecoder(bytes.NewReader(s.config))
	decoder.UseNumber()
	if err := decoder.Decode(&generated); err != nil {
		return nil, err
	}
	return &preparedCoreStart{runtime: cloneKernelRuntime(s.runtime), path: s.path, config: generated}, nil
}

func (s *Service) beginExplicitCoreOperation() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return 0, connect.NewError(connect.CodeUnavailable, fmt.Errorf("application is shutting down"))
	}
	s.stopRequested = false
	return s.stopGeneration, nil
}

// The transition lock is separate from restartOperationMu so an explicit stop
// can cancel an in-flight restart or core upgrade. stopGeneration invalidates work
// prepared before a stop, including work waiting for this lock.
func (s *Service) beginCoreTransition(requestCtx context.Context, generation uint64) (context.Context, context.Context, func(), error) {
	s.coreTransitionMu.Lock()
	s.mu.Lock()
	if s.closing || s.stopGeneration != generation || requestCtx.Err() != nil {
		err := requestCtx.Err()
		if err == nil {
			err = context.Canceled
		}
		s.mu.Unlock()
		s.coreTransitionMu.Unlock()
		return nil, nil, nil, rpcutil.AsConnectError(err)
	}
	controlCtx, cancelControl := context.WithCancel(s.lifecycleCtx)
	s.transitionCancel = cancelControl
	s.mu.Unlock()
	launchCtx, cancelLaunch := context.WithCancel(requestCtx)
	stopLink := context.AfterFunc(controlCtx, cancelLaunch)
	finish := func() {
		stopLink()
		cancelLaunch()
		cancelControl()
		s.mu.Lock()
		s.transitionCancel = nil
		s.mu.Unlock()
		s.coreTransitionMu.Unlock()
	}
	return launchCtx, controlCtx, finish, nil
}

func restartRecoveryError(cause error, outcome string, recoveryErr error) error {
	message := fmt.Errorf("core restart failed; %s: %w", outcome, cause)
	if recoveryErr != nil {
		message = fmt.Errorf("core restart failed; %s: %w", outcome, errors.Join(cause, recoveryErr))
	}
	result := connect.NewError(connect.CodeOf(cause), message)
	var original *connect.Error
	if errors.As(cause, &original) {
		for key, values := range original.Meta() {
			result.Meta()[key] = slices.Clone(values)
		}
	}
	return result
}

func (s *Service) restorePreviousCore(controlCtx context.Context, generation uint64, previous *coreStartSnapshot, cause error) error {
	if previous == nil {
		return restartRecoveryError(cause, "previous running configuration is unavailable; core was not restored", nil)
	}
	s.mu.Lock()
	stopped := s.closing || s.stopGeneration != generation || controlCtx.Err() != nil
	unsafe := s.corePID > 0 || s.status == kernelv1.CoreStatus_CORE_STATUS_STARTING || s.status == kernelv1.CoreStatus_CORE_STATUS_STOPPING || s.status == kernelv1.CoreStatus_CORE_STATUS_RUNNING
	s.mu.Unlock()
	if stopped {
		return restartRecoveryError(cause, "restore canceled by stop or shutdown", nil)
	}
	if unsafe || errors.Is(cause, platform.ErrCoreExitUnconfirmed) {
		return restartRecoveryError(cause, "new core exit was not confirmed; previous core was not restored", nil)
	}
	prepared, err := previous.prepared()
	if err != nil {
		return restartRecoveryError(cause, "previous core restore failed", err)
	}
	// A lost HTTP/RPC client must not leave a router offline. Explicit StopCore
	// and application shutdown still cancel this independent, bounded recovery.
	ctx, cancel := context.WithTimeout(controlCtx, 30*time.Second)
	defer cancel()
	if err := s.setStarting(previous.profile.GetId(), generation); err != nil {
		return restartRecoveryError(cause, "previous core restore canceled", err)
	}
	if _, err := s.launchPreparedCore(ctx, previous.profile, prepared); err != nil {
		return restartRecoveryError(cause, "previous core restore failed", err)
	}
	s.updateCoreState(func() { s.restartRequired = true })
	return restartRecoveryError(cause, "previous running configuration was restored", nil)
}

// A runner can report a failure after OnStarted (for example cancellation).
// Do not discard that PID or restore an old instance until cleanup succeeds.
func (s *Service) cleanFailedCoreStart() error {
	s.mu.Lock()
	pid := s.corePID
	s.mu.Unlock()
	if pid > 0 {
		s.updateCoreState(func() { s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPING })
		if result := s.processes.KillProcess(pid, 5); !result.Flag {
			return fmt.Errorf("core startup cleanup failed: %w: %s", platform.ErrCoreExitUnconfirmed, result.Data)
		}
	}
	s.setStopped()
	return nil
}
