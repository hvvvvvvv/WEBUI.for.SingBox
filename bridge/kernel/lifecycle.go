package kernel

import (
	"context"
	"errors"
	"fmt"

	"guiforcores/bridge/logging"
	kernelv1 "guiforcores/gen/kernel/v1"
)

func (s *Service) SetCoreLogWriter(writer *logging.CoreWriter) { s.coreLogs = writer }

func (s *Service) BeginShutdown() {
	s.mu.Lock()
	s.closing = true
	s.revokeNativeAPILocked()
	if s.corePID > 0 {
		s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPING
	}
	s.mu.Unlock()
	s.lifecycleCancel()
	s.restartQueueMu.Lock()
	s.restartPending = false
	s.restartQueueMu.Unlock()
}

func (s *Service) Close(ctx context.Context) error {
	s.BeginShutdown()
	var err error
	if closer, ok := s.processes.(interface{ CloseCoreProcesses(context.Context) error }); ok {
		err = closer.CloseCoreProcesses(ctx)
	} else {
		s.mu.Lock()
		pid := s.corePID
		s.mu.Unlock()
		if pid > 0 {
			if result := s.processes.KillProcess(pid, 10); !result.Flag {
				err = fmt.Errorf("stop core: %s", result.Data)
			}
		}
	}
	done := make(chan struct{})
	go func() { s.operations.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		err = errors.Join(err, ctx.Err())
	}
	if err == nil {
		s.setStopped()
	}
	if s.coreLogs != nil {
		err = errors.Join(err, s.coreLogs.Close())
	}
	return err
}
