package kernel

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"guiforcores/bridge/rpcutil"
	kernelv1 "guiforcores/gen/kernel/v1"
)

// Both core branches use the same managed cache.db. Its current owner, rather
// than the branch selected in the settings card, must be stopped before removal.
func (s *Service) clearSharedCoreCache(requestCtx context.Context, remove func() error) error {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("application is shutting down"))
	}
	s.operations.Add(1)
	generation := s.stopGeneration
	s.mu.Unlock()
	defer s.operations.Done()
	s.restartOperationMu.Lock()
	defer s.restartOperationMu.Unlock()
	ctx, recoveryCtx, finish, err := s.beginCoreTransition(requestCtx, generation)
	if err != nil {
		return err
	}
	defer finish()
	s.mu.Lock()
	status, pid := s.status, s.corePID
	previous, pending := s.currentStart, s.restartRequired
	s.mu.Unlock()
	if status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
		if pid > 0 {
			return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("core exit must be confirmed before clearing its cache"))
		}
		return remove()
	}
	if previous == nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("running core snapshot is unavailable; stop the core before clearing its cache"))
	}
	if !usesManagedProxyConfig(previous.runtime.Args, s.processes.ResolvePath(coreConfigRelativePath)) {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cache clearing is unavailable for custom core configuration arguments; no cache file was removed"))
	}
	prepared, err := previous.prepared()
	if err != nil {
		return err
	}
	if _, err := s.stopCore(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return s.restorePreviousCore(recoveryCtx, generation, previous, rpcutil.AsConnectError(err))
	}
	if err := remove(); err != nil {
		return s.restorePreviousCore(recoveryCtx, generation, previous, err)
	}
	if err := s.setStarting(previous.profile.GetId(), generation); err != nil {
		return err
	}
	if _, err := s.launchPreparedCore(ctx, previous.profile, prepared); err != nil {
		return s.restorePreviousCore(recoveryCtx, generation, previous, err)
	}
	if pending {
		s.updateCoreState(func() { s.restartRequired = true })
	}
	return nil
}
