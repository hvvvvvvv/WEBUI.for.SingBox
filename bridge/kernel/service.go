package kernel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/logging"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/rpcutil"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

const (
	coreWorkingDirectory    = "data/sing-box"
	corePidFilePath         = coreWorkingDirectory + "/pid.txt"
	coreStopOutputKeyword   = "sing-box started"
	coreConfigRelativePath  = coreWorkingDirectory + "/config.json"
	kernelStateChangedEvent = "kernelStateChanged"
	coreErrorReasonHeader   = "X-Core-Error-Reason"
	coreAPIUnavailable      = "core-api-not-ready"
)

type kernelRuntimeConfig struct {
	Branch string
	Args   []string
	Env    map[string]string
}

type ProcessRunner interface {
	Exec(path string, args []string, options platform.ExecOptions) platform.Result
	ExecBackground(path string, args []string, outEvent string, options platform.ExecOptions) platform.Result
	ProcessMemory(pid int32) platform.Result
	KillProcess(pid int, timeout int) platform.Result
	ResolvePath(path string) string
	BaseDir() string
}

type ConfigGenerator interface {
	Generate(profile *profilev1.Profile, options *kernelv1.GenerateConfigOptions) (map[string]any, error)
	WriteGeneratedConfig(generatedConfig map[string]any) error
}

type AppConfigReader interface {
	Current() config.AppConfig
}

type ProfileReader interface {
	FindByID(id string) (*profilev1.Profile, error)
}

type EventPublisher interface {
	Publish(eventName string, data ...any)
}

type Service struct {
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	operations      sync.WaitGroup
	closing         bool
	coreLogs        *logging.CoreWriter
	processes       ProcessRunner
	config          ConfigGenerator
	appConfig       AppConfigReader
	profiles        ProfileReader
	events          EventPublisher

	stateEventMu       sync.Mutex
	restartOperationMu sync.Mutex
	coreTransitionMu   sync.Mutex
	restartQueueMu     sync.Mutex
	mu                 sync.Mutex
	status             kernelv1.CoreStatus
	activeProfileID    string
	corePID            int
	currentProfile     *profilev1.Profile
	restartRequired    bool
	restarting         bool
	restartWorker      bool
	restartPending     bool
	restartRequestedAt time.Time
	restartTargetID    string
	restartTargetForce bool
	restartExecutingID string
	downloads          map[string]context.CancelFunc
	nativeAPI          *nativeAPIInstance
	requestProxyURL    string
	currentStart       *coreStartSnapshot
	stopGeneration     uint64
	stopRequested      bool
	transitionCancel   context.CancelFunc
}

var waitKernelAPIReadyFunc = waitKernelAPIReady

func NewService(processes ProcessRunner, configService ConfigGenerator, appConfig AppConfigReader, profiles ProfileReader, events EventPublisher) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		lifecycleCtx: ctx, lifecycleCancel: cancel,
		processes: processes,
		config:    configService,
		appConfig: appConfig,
		profiles:  profiles,
		events:    events,
		status:    kernelv1.CoreStatus_CORE_STATUS_STOPPED,
		corePID:   -1,
		downloads: map[string]context.CancelFunc{},
	}
}

func (s *Service) publish(eventName string, data ...any) {
	if s.events != nil {
		s.events.Publish(eventName, data...)
	}
}

func visibleCorePID(status kernelv1.CoreStatus, pid int) int {
	if status == kernelv1.CoreStatus_CORE_STATUS_RUNNING && pid > 0 {
		return pid
	}
	return -1
}

func (s *Service) publishCoreState(status kernelv1.CoreStatus, pid int, restartRequired bool, restarting bool) {
	s.publish(kernelStateChangedEvent, map[string]any{
		"status":          status,
		"pid":             pid,
		"restartRequired": restartRequired,
		"restarting":      restarting,
	})
}

func (s *Service) updateCoreState(update func()) {
	s.stateEventMu.Lock()
	defer s.stateEventMu.Unlock()

	s.mu.Lock()
	previousStatus := s.status
	previousPID := visibleCorePID(s.status, s.corePID)
	previousRestartRequired := s.restartRequired
	previousRestarting := s.restarting
	update()
	status := s.status
	pid := visibleCorePID(status, s.corePID)
	restartRequired := s.restartRequired
	restarting := s.restarting
	s.mu.Unlock()

	if status != previousStatus || pid != previousPID || restartRequired != previousRestartRequired || restarting != previousRestarting {
		s.publishCoreState(status, pid, restartRequired, restarting)
	}
}

func (s *Service) setStarting(profileID string, generation ...uint64) error {
	s.stateEventMu.Lock()
	defer s.stateEventMu.Unlock()

	s.mu.Lock()
	if len(generation) > 0 && s.stopGeneration != generation[0] {
		s.mu.Unlock()
		return connect.NewError(connect.CodeCanceled, fmt.Errorf("core start superseded by stop"))
	}
	if s.closing || s.corePID > 0 || s.status == kernelv1.CoreStatus_CORE_STATUS_STARTING || s.status == kernelv1.CoreStatus_CORE_STATUS_RUNNING || s.status == kernelv1.CoreStatus_CORE_STATUS_STOPPING {
		s.mu.Unlock()
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("core is already starting or running"))
	}
	s.status = kernelv1.CoreStatus_CORE_STATUS_STARTING
	s.activeProfileID = profileID
	s.corePID = -1
	s.currentProfile = nil
	s.currentStart = nil
	restartRequired := s.restartRequired
	restarting := s.restarting
	s.mu.Unlock()

	s.publishCoreState(kernelv1.CoreStatus_CORE_STATUS_STARTING, -1, restartRequired, restarting)
	return nil
}

func (s *Service) completeStart(pid int, profileID string, profile *profilev1.Profile, secret string, proxyConfig ...map[string]any) bool {
	return s.completeStartWithSnapshot(pid, profileID, profile, secret, nil, proxyConfig...)
}

func (s *Service) completeStartWithSnapshot(pid int, profileID string, profile *profilev1.Profile, secret string, snapshot *coreStartSnapshot, proxyConfig ...map[string]any) bool {
	s.stateEventMu.Lock()
	defer s.stateEventMu.Unlock()

	s.mu.Lock()
	if s.closing || s.status != kernelv1.CoreStatus_CORE_STATUS_STARTING || s.corePID != pid {
		s.mu.Unlock()
		return false
	}
	s.installNativeAPILocked(pid, secret)
	if len(proxyConfig) > 0 {
		s.requestProxyURL = requestProxyFromConfig(proxyConfig[0])
	}
	s.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	s.activeProfileID = profileID
	s.currentProfile = cloneProfile(profile)
	s.currentStart = snapshot
	s.restartRequired = false
	restartRequired := s.restartRequired
	restarting := s.restarting
	s.mu.Unlock()

	s.publishCoreState(kernelv1.CoreStatus_CORE_STATUS_RUNNING, pid, restartRequired, restarting)
	return true
}

func (s *Service) setStopped() {
	s.updateCoreState(func() {
		s.revokeNativeAPILocked()
		s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPED
		s.corePID = -1
		s.currentProfile = nil
		s.currentStart = nil
		if !s.restarting {
			s.restartRequired = false
		}
	})
}

func (s *Service) beginStopping() (int, error) {
	s.stateEventMu.Lock()
	defer s.stateEventMu.Unlock()

	s.mu.Lock()
	if s.status == kernelv1.CoreStatus_CORE_STATUS_STOPPED && s.corePID <= 0 {
		s.mu.Unlock()
		return 0, nil
	}
	// Failed startup/termination keeps the registered PID until exit is
	// confirmed. StopCore serializes retries with starts and other stops, so
	// these states may safely retry the same runner's process cleanup.
	retryCleanup := s.status == kernelv1.CoreStatus_CORE_STATUS_CRASHED || s.status == kernelv1.CoreStatus_CORE_STATUS_STOPPING
	if s.corePID <= 0 || (s.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING && !retryCleanup) {
		s.mu.Unlock()
		return -1, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("core is not running"))
	}
	pid := s.corePID
	s.revokeNativeAPILocked()
	s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPING
	restartRequired := s.restartRequired
	restarting := s.restarting
	s.mu.Unlock()

	s.publishCoreState(kernelv1.CoreStatus_CORE_STATUS_STOPPING, -1, restartRequired, restarting)
	return pid, nil
}

func (s *Service) handleCoreProcessExit(pid int, waitErr error) {
	s.stateEventMu.Lock()
	defer s.stateEventMu.Unlock()

	s.mu.Lock()
	if s.corePID != pid {
		s.mu.Unlock()
		return
	}
	status := s.status
	if status != kernelv1.CoreStatus_CORE_STATUS_STOPPING &&
		status != kernelv1.CoreStatus_CORE_STATUS_STARTING &&
		status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
		s.mu.Unlock()
		return
	}

	s.revokeNativeAPILocked()
	unconfirmed := errors.Is(waitErr, platform.ErrCoreExitUnconfirmed)
	crashed := (!s.closing && status != kernelv1.CoreStatus_CORE_STATUS_STOPPING) || unconfirmed
	crashPhase := "runtime"
	if status == kernelv1.CoreStatus_CORE_STATUS_STARTING {
		crashPhase = "startup"
	}
	if crashed {
		s.status = kernelv1.CoreStatus_CORE_STATUS_CRASHED
		s.restartRequired = false
	} else {
		s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPED
		if !s.restarting {
			s.restartRequired = false
		}
	}
	if !unconfirmed {
		s.corePID = -1
	}
	s.currentProfile = nil
	s.currentStart = nil
	status = s.status
	restartRequired := s.restartRequired
	restarting := s.restarting
	s.mu.Unlock()

	s.publishCoreState(status, -1, restartRequired, restarting)
	if !crashed {
		return
	}

	reason := "core process exited"
	if waitErr != nil {
		reason = waitErr.Error()
	}
	s.publishCoreCrash(pid, reason, crashPhase)
}

const maxCoreCrashReasonRunes = 500

func sanitizeCoreCrashReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "core process exited"
	}

	fields := strings.Fields(reason)
	for index, field := range fields {
		if !strings.Contains(field, "://") {
			continue
		}
		trimmed := strings.TrimRight(field, ".,;:)]}")
		suffix := strings.TrimPrefix(field, trimmed)
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Host == "" {
			continue
		}
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		fields[index] = parsed.String() + suffix
	}
	reason = strings.Join(fields, " ")
	runes := []rune(reason)
	if len(runes) > maxCoreCrashReasonRunes {
		reason = string(runes[:maxCoreCrashReasonRunes-1]) + "…"
	}
	return reason
}

func (s *Service) publishCoreCrash(pid int, reason string, phase string) {
	s.publish("kernelCrashed", map[string]any{
		"pid":    pid,
		"reason": sanitizeCoreCrashReason(reason),
		"phase":  phase,
	})
}

func coreAPIUnavailableError(cause error) error {
	err := connect.NewError(connect.CodeUnavailable, fmt.Errorf("kernel api is not ready: %w", cause))
	err.Meta().Set(coreErrorReasonHeader, coreAPIUnavailable)
	return err
}

func (s *Service) StartCore(
	ctx context.Context,
	req *connect.Request[kernelv1.StartCoreRequest],
) (response *connect.Response[kernelv1.StartCoreResponse], responseErr error) {
	started := time.Now()
	profileID := req.Msg.GetProfileId()
	defer func() {
		pid := -1
		if response != nil {
			pid = int(response.Msg.GetPid())
		}
		logging.Complete(ctx, "kernel", "start", "core started", started, responseErr, "profile_id", profileID, "pid", pid)
	}()
	if profileID == "" {
		return nil, rpcutil.AsConnectError(rpcutil.InvalidArgumentError{Message: "profile_id is required"})
	}
	generation, err := s.beginExplicitCoreOperation()
	if err != nil {
		return nil, err
	}

	profile, err := s.loadProfileByID(profileID)
	if err != nil {
		return nil, rpcutil.AsConnectError(err)
	}

	pid, err := s.startCoreWithProfile(ctx, profile, generation)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&kernelv1.StartCoreResponse{Pid: int32(pid)}), nil
}

func (s *Service) StartCoreWithProfile(
	ctx context.Context,
	req *connect.Request[kernelv1.StartCoreWithProfileRequest],
) (response *connect.Response[kernelv1.StartCoreWithProfileResponse], responseErr error) {
	started := time.Now()
	profile := req.Msg.GetProfile()
	profileID := profile.GetId()
	defer func() {
		pid := -1
		if response != nil {
			pid = int(response.Msg.GetPid())
		}
		logging.Complete(ctx, "kernel", "start", "core started", started, responseErr, "profile_id", profileID, "pid", pid)
	}()
	if profile == nil || profile.GetId() == "" {
		return nil, rpcutil.AsConnectError(rpcutil.InvalidArgumentError{Message: "profile is required"})
	}
	generation, err := s.beginExplicitCoreOperation()
	if err != nil {
		return nil, err
	}

	pid, err := s.startCoreWithProfile(ctx, profile, generation)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&kernelv1.StartCoreWithProfileResponse{Pid: int32(pid)}), nil
}

func (s *Service) startCoreWithProfile(ctx context.Context, profile *profilev1.Profile, generation uint64) (int, error) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return -1, connect.NewError(connect.CodeUnavailable, fmt.Errorf("application is shutting down"))
	}
	s.operations.Add(1)
	s.mu.Unlock()
	defer s.operations.Done()
	ctx, _, finish, err := s.beginCoreTransition(ctx, generation)
	if err != nil {
		return -1, err
	}
	defer finish()
	profileID := profile.GetId()

	if err := s.setStarting(profileID, generation); err != nil {
		return -1, err
	}
	runtimeCfg, err := s.loadRuntimeConfig()
	if err != nil {
		s.setStopped()
		return -1, rpcutil.AsConnectError(err)
	}
	prepared, err := s.prepareCoreStart(ctx, profile, runtimeCfg, coreWorkingDirectory+"/"+getKernelFileName(runtimeCfg.Branch == "alpha"))
	if err != nil {
		s.setStopped()
		return -1, err
	}
	return s.launchPreparedCore(ctx, profile, prepared)
}

func (s *Service) launchPreparedCore(ctx context.Context, profile *profilev1.Profile, prepared *preparedCoreStart) (int, error) {
	profileID := profile.GetId()
	runtimeCfg, generatedConfig := prepared.runtime, prepared.config
	if err := ctx.Err(); err != nil {
		s.setStopped()
		return -1, rpcutil.AsConnectError(err)
	}
	snapshot, err := newCoreStartSnapshot(profile, prepared)
	if err != nil {
		s.setStopped()
		return -1, rpcutil.AsConnectError(err)
	}
	if err := s.config.WriteGeneratedConfig(generatedConfig); err != nil {
		s.setStopped()
		return -1, rpcutil.AsConnectError(err)
	}

	execResult := s.processes.ExecBackground(
		prepared.path,
		runtimeCfg.Args,
		"kernelLog",
		platform.ExecOptions{
			Guard:            true,
			Context:          ctx,
			CheckArgs:        []string{"check", "--disable-color", "-c", s.processes.ResolvePath(coreConfigRelativePath), "-D", s.processes.ResolvePath(coreWorkingDirectory)},
			WorkingDirectory: s.processes.ResolvePath(coreWorkingDirectory),
			OnOutput: func(output logging.CoreOutput) {
				if s.coreLogs != nil {
					s.coreLogs.Write(output, profileID)
				}
			},
			OnStarted:         func(pid int) { s.updateCoreState(func() { s.corePID = pid }) },
			PIDFile:           corePidFilePath,
			StopOutputKeyword: coreStopOutputKeyword,
			Env:               runtimeCfg.Env,
			OnExit:            s.handleCoreProcessExit,
		},
	)
	if !execResult.Flag {
		if strings.Contains(execResult.Data, platform.ErrCoreExitUnconfirmed.Error()) {
			s.updateCoreState(func() { s.status = kernelv1.CoreStatus_CORE_STATUS_CRASHED })
			return -1, connect.NewError(connect.CodeInternal, fmt.Errorf("start core failed: %w: %s", platform.ErrCoreExitUnconfirmed, execResult.Data))
		}
		if err := s.cleanFailedCoreStart(); err != nil {
			return -1, connect.NewError(connect.CodeInternal, err)
		}
		if ctx.Err() != nil {
			return -1, rpcutil.AsConnectError(ctx.Err())
		}
		if strings.HasPrefix(execResult.Data, "invalid core config:") {
			return -1, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s", execResult.Data))
		}
		return -1, connect.NewError(connect.CodeInternal, fmt.Errorf("start core failed: %s", execResult.Data))
	}

	pid, convErr := parsePID(execResult.Data)
	if convErr != nil {
		if err := s.cleanFailedCoreStart(); err != nil {
			return -1, connect.NewError(connect.CodeInternal, errors.Join(convErr, err))
		}
		return -1, connect.NewError(connect.CodeInternal, convErr)
	}

	s.updateCoreState(func() {
		if s.status == kernelv1.CoreStatus_CORE_STATUS_STARTING {
			s.corePID = pid
		}
	})

	readyErr := waitKernelAPIReadyFunc(ctx, config.CoreAPIController, config.NativeAPISecret(generatedConfig), pid, coreStartupTimeout)
	if readyErr == nil {
		readyErr = ctx.Err()
	}
	if err := readyErr; err != nil {
		s.updateCoreState(func() { s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPING })
		if ctx.Err() == nil {
			s.publishCoreCrash(pid, err.Error(), "startup")
		}
		if result := s.processes.KillProcess(pid, 5); !result.Flag {
			return -1, connect.NewError(connect.CodeInternal, fmt.Errorf("core startup cleanup failed: %s", result.Data))
		}
		s.setStopped()
		if ctx.Err() != nil {
			return -1, rpcutil.AsConnectError(ctx.Err())
		}
		return -1, coreAPIUnavailableError(err)
	}

	var proxyConfig map[string]any
	if usesManagedProxyConfig(runtimeCfg.Args, s.processes.ResolvePath(coreConfigRelativePath)) {
		proxyConfig = generatedConfig
	}
	if !s.completeStartWithSnapshot(pid, profileID, profile, config.NativeAPISecret(generatedConfig), snapshot, proxyConfig) {
		return -1, connect.NewError(connect.CodeUnavailable, fmt.Errorf("core process exited during startup"))
	}

	return pid, nil
}

func (s *Service) StopCore(
	ctx context.Context,
	_ *connect.Request[kernelv1.StopCoreRequest],
) (response *connect.Response[kernelv1.StopCoreResponse], responseErr error) {
	// Declare stop before waiting for an in-flight start/restart. Its rollback
	// must never resurrect a core after an explicit stop request.
	s.mu.Lock()
	s.stopGeneration++
	s.stopRequested = true
	cancel := s.transitionCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.restartQueueMu.Lock()
	s.restartPending = false
	s.restartQueueMu.Unlock()
	s.coreTransitionMu.Lock()
	defer s.coreTransitionMu.Unlock()
	response, responseErr = s.stopCore(ctx)
	if responseErr == nil {
		s.updateCoreState(func() { s.restartRequired, s.restarting = false, false })
	}
	return response, responseErr
}

// Internal stop does not invalidate the transaction that deliberately replaces
// the current process. Public StopCore additionally cancels that transaction.
func (s *Service) stopCore(ctx context.Context) (response *connect.Response[kernelv1.StopCoreResponse], responseErr error) {
	started := time.Now()
	pid := -1
	defer func() {
		logging.Complete(ctx, "kernel", "stop", "core stopped", started, responseErr, "pid", pid)
	}()
	pid, err := s.beginStopping()
	if err != nil {
		return nil, err
	}
	if pid == 0 {
		return connect.NewResponse(&kernelv1.StopCoreResponse{}), nil
	}

	result := s.processes.KillProcess(pid, 10)
	if !result.Flag {
		s.updateCoreState(func() { s.status = kernelv1.CoreStatus_CORE_STATUS_CRASHED; s.restartRequired = false })
		s.publishCoreCrash(pid, result.Data, "shutdown")
		err := connect.NewError(connect.CodeInternal, fmt.Errorf("stop core failed: %s", result.Data))
		return nil, err
	}

	s.setStopped()

	return connect.NewResponse(&kernelv1.StopCoreResponse{}), nil
}

func (s *Service) RestartCore(
	ctx context.Context,
	req *connect.Request[kernelv1.RestartCoreRequest],
) (response *connect.Response[kernelv1.RestartCoreResponse], responseErr error) {
	if s.lifecycleCtx.Err() != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("application is shutting down"))
	}
	started := time.Now()
	profileID := req.Msg.GetProfileId()
	if req.Msg.GetProfile() != nil {
		profileID = req.Msg.GetProfile().GetId()
	}
	if profileID == "" {
		s.mu.Lock()
		profileID = s.activeProfileID
		s.mu.Unlock()
	}
	defer func() {
		pid := -1
		if response != nil {
			pid = int(response.Msg.GetPid())
		}
		logging.Complete(ctx, "kernel", "restart", "core restarted", started, responseErr, "profile_id", profileID, "pid", pid)
	}()
	if profileID == "" {
		return nil, rpcutil.AsConnectError(rpcutil.InvalidArgumentError{Message: "profile_id is required for restart when no active profile exists"})
	}
	generation, err := s.beginExplicitCoreOperation()
	if err != nil {
		return nil, err
	}

	s.updateCoreState(func() {
		s.restarting = true
	})
	defer s.finishManualRestart()

	s.restartOperationMu.Lock()
	defer s.restartOperationMu.Unlock()
	if profile := req.Msg.GetProfile(); profile != nil {
		if profile.GetId() == "" || (req.Msg.GetProfileId() != "" && profile.GetId() != req.Msg.GetProfileId()) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("profile.id is required and must match profile_id when both are supplied"))
		}
		response, err = s.restartCoreWithProfileOnce(ctx, cloneProfile(profile), generation)
	} else {
		response, err = s.restartCoreAtGeneration(ctx, profileID, generation)
	}
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (s *Service) restartCoreOnce(
	ctx context.Context,
	profileID string,
) (*connect.Response[kernelv1.RestartCoreResponse], error) {
	s.mu.Lock()
	generation, stopped := s.stopGeneration, s.stopRequested
	s.mu.Unlock()
	if stopped {
		return nil, connect.NewError(connect.CodeCanceled, fmt.Errorf("automatic restart superseded by stop"))
	}
	return s.restartCoreAtGeneration(ctx, profileID, generation)
}

func (s *Service) restartCoreAtGeneration(ctx context.Context, profileID string, generation uint64) (*connect.Response[kernelv1.RestartCoreResponse], error) {
	profile, err := s.loadProfileByID(profileID)
	if err != nil {
		return nil, rpcutil.AsConnectError(err)
	}

	return s.restartCoreWithProfileOnce(ctx, profile, generation)
}

func (s *Service) restartCoreWithProfileOnce(ctx context.Context, profile *profilev1.Profile, generation uint64) (*connect.Response[kernelv1.RestartCoreResponse], error) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("application is shutting down"))
	}
	s.operations.Add(1)
	s.mu.Unlock()
	defer s.operations.Done()
	ctx, cancel := context.WithCancel(ctx)
	stopCancellation := context.AfterFunc(s.lifecycleCtx, cancel)
	defer func() { stopCancellation(); cancel() }()
	runtimeCfg, err := s.loadRuntimeConfig()
	if err != nil {
		return nil, rpcutil.AsConnectError(err)
	}
	prepared, err := s.prepareCoreStart(ctx, profile, runtimeCfg, coreWorkingDirectory+"/"+getKernelFileName(runtimeCfg.Branch == "alpha"))
	if err != nil {
		return nil, err
	}
	return s.restartPreparedCore(ctx, profile, prepared, generation)
}

func (s *Service) restartPreparedCore(ctx context.Context, profile *profilev1.Profile, prepared *preparedCoreStart, generation uint64) (*connect.Response[kernelv1.RestartCoreResponse], error) {
	ctx, recoveryCtx, finish, err := s.beginCoreTransition(ctx, generation)
	if err != nil {
		return nil, err
	}
	defer finish()
	s.mu.Lock()
	previous := s.currentStart
	if s.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
		previous = nil
	}
	s.mu.Unlock()
	if _, err := s.stopCore(ctx); err != nil {
		return nil, err
	}
	if err := s.setStarting(profile.GetId(), generation); err != nil {
		return nil, err
	}
	pid, err := s.launchPreparedCore(ctx, profile, prepared)
	if err != nil {
		return nil, s.restorePreviousCore(recoveryCtx, generation, previous, err)
	}
	return connect.NewResponse(&kernelv1.RestartCoreResponse{Pid: int32(pid)}), nil
}

func (s *Service) GetCoreStatus(
	ctx context.Context,
	_ *connect.Request[kernelv1.GetCoreStatusRequest],
) (*connect.Response[kernelv1.GetCoreStatusResponse], error) {
	s.mu.Lock()
	status := s.status
	pid := s.corePID
	restartRequired := s.restartRequired
	restarting := s.restarting
	s.mu.Unlock()
	logging.FromContext(ctx).DebugContext(ctx, "core status read", "component", "kernel", "operation", "status", "status", status.String(), "pid", pid)
	if status != kernelv1.CoreStatus_CORE_STATUS_RUNNING || pid <= 0 {
		pid = -1
	}
	return connect.NewResponse(&kernelv1.GetCoreStatusResponse{
		Status:          status,
		Pid:             int32(pid),
		RestartRequired: restartRequired,
		Restarting:      restarting,
	}), nil
}

func (s *Service) GetCurrentProfile(
	_ context.Context,
	_ *connect.Request[kernelv1.GetCurrentProfileRequest],
) (*connect.Response[kernelv1.GetCurrentProfileResponse], error) {
	s.mu.Lock()
	status := s.status
	currentProfile := cloneProfile(s.currentProfile)
	activeProfileID := s.activeProfileID
	s.mu.Unlock()

	if status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("core is not running"))
	}

	if currentProfile == nil && activeProfileID != "" {
		profile, err := s.loadProfileByID(activeProfileID)
		if err != nil {
			return nil, rpcutil.AsConnectError(err)
		}
		currentProfile = cloneProfile(profile)
	}

	if currentProfile == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("current profile is not available"))
	}

	return connect.NewResponse(&kernelv1.GetCurrentProfileResponse{
		Profile: currentProfile,
		Status:  status,
	}), nil
}

func cloneProfile(profile *profilev1.Profile) *profilev1.Profile {
	if profile == nil {
		return nil
	}
	cloned, ok := proto.Clone(profile).(*profilev1.Profile)
	if !ok {
		return nil
	}
	return cloned
}

func (s *Service) GetCurrentCoreMemory(
	ctx context.Context,
	_ *connect.Request[kernelv1.GetCurrentCoreMemoryRequest],
) (*connect.Response[kernelv1.GetCurrentCoreMemoryResponse], error) {
	s.mu.Lock()
	status := s.status
	pid := s.corePID
	s.mu.Unlock()

	if status != kernelv1.CoreStatus_CORE_STATUS_RUNNING || pid <= 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("core is not running"))
	}

	result := s.processes.ProcessMemory(int32(pid))
	if !result.Flag {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get current core memory failed: %s", result.Data))
	}

	rss, err := strconv.ParseUint(result.Data, 10, 64)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("invalid memory usage from core process: %q", result.Data))
	}
	logging.FromContext(ctx).DebugContext(ctx, "core memory read", "component", "kernel", "operation", "memory", "pid", pid, "bytes", rss, "result", "success")

	return connect.NewResponse(&kernelv1.GetCurrentCoreMemoryResponse{Rss: rss}), nil
}

func (s *Service) AutoStart(ctx context.Context) {
	appConfig := s.appConfig.Current()
	if !appConfig.AutoStartKernel {
		slog.Debug("core auto-start disabled", "component", "kernel", "operation", "auto_start", "result", "skipped")
		return
	}

	profileID := strings.TrimSpace(appConfig.Profile)
	if profileID == "" {
		slog.Warn("core auto-start skipped", "component", "kernel", "operation", "auto_start", "result", "skipped", "error", "no profile selected")
		return
	}

	slog.Info("core auto-start requested", "component", "kernel", "operation", "auto_start", "profile_id", profileID)
	if _, err := s.StartCore(ctx, connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: profileID})); err != nil {
		return
	}
}

func (s *Service) loadProfileByID(id string) (*profilev1.Profile, error) {
	return s.profiles.FindByID(id)
}

func (s *Service) loadRuntimeConfig() (kernelRuntimeConfig, error) {
	appCfg := s.appConfig.Current()
	coreCfg := appCfg.Main
	if appCfg.Branch == "alpha" {
		coreCfg = appCfg.Alpha
	}
	cfg := kernelRuntimeConfig{
		Branch: appCfg.Branch,
		Args:   append([]string{}, coreCfg.Args...),
		Env:    map[string]string{},
	}
	for key, value := range coreCfg.Env {
		cfg.Env[key] = value
	}
	return normalizeKernelRuntime(cfg, s.processes.BaseDir()), nil
}

func normalizeKernelRuntime(cfg kernelRuntimeConfig, basePath string) kernelRuntimeConfig {
	replacer := strings.NewReplacer(
		"$APP_BASE_PATH", strings.TrimSuffix(basePath, "/"),
		"$CORE_BASE_PATH", coreWorkingDirectory,
	)

	for i, arg := range cfg.Args {
		cfg.Args[i] = replacer.Replace(arg)
	}
	for key, value := range cfg.Env {
		cfg.Env[key] = replacer.Replace(value)
	}
	return cfg
}

func getKernelFileName(isAlpha bool) string {
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	latest := ""
	if isAlpha {
		latest = "-latest"
	}
	return "sing-box" + latest + suffix
}

func parsePID(raw string) (int, error) {
	pid := 0
	_, err := fmt.Sscanf(raw, "%d", &pid)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid from core start result: %q", raw)
	}
	return pid, nil
}
