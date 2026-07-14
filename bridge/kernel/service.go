package kernel

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"guiforcores/bridge/config"
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
)

type kernelRuntimeConfig struct {
	Branch string
	Args   []string
	Env    map[string]string
}

type ProcessRunner interface {
	Exec(path string, args []string, options platform.ExecOptions) platform.Result
	ExecBackground(path string, args []string, outEvent string, options platform.ExecOptions) platform.Result
	ProcessInfo(pid int32) platform.Result
	ProcessMemory(pid int32) platform.Result
	KillProcess(pid int, timeout int) platform.Result
	ResolvePath(path string) string
	BaseDir() string
}

type ConfigGenerator interface {
	Generate(profile *profilev1.Profile, options *kernelv1.GenerateConfigOptions) (map[string]any, error)
	WriteGeneratedConfig(generatedConfig map[string]any) error
	ReadGeneratedSecret() string
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
	processes ProcessRunner
	config    ConfigGenerator
	appConfig AppConfigReader
	profiles  ProfileReader
	events    EventPublisher

	stateEventMu    sync.Mutex
	mu              sync.Mutex
	status          kernelv1.CoreStatus
	activeProfileID string
	corePID         int
	currentProfile  *profilev1.Profile
	downloads       map[string]context.CancelFunc
}

var waitKernelAPIReadyFunc = waitKernelAPIReady

func NewService(processes ProcessRunner, configService ConfigGenerator, appConfig AppConfigReader, profiles ProfileReader, events EventPublisher) *Service {
	return &Service{
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

func (s *Service) updateCoreState(update func()) {
	s.stateEventMu.Lock()
	defer s.stateEventMu.Unlock()

	s.mu.Lock()
	previousStatus := s.status
	previousPID := visibleCorePID(s.status, s.corePID)
	update()
	status := s.status
	pid := visibleCorePID(status, s.corePID)
	s.mu.Unlock()

	if status != previousStatus || pid != previousPID {
		s.publish(kernelStateChangedEvent, map[string]any{"status": status, "pid": pid})
	}
}

func (s *Service) setStarting(profileID string) error {
	s.stateEventMu.Lock()
	defer s.stateEventMu.Unlock()

	s.mu.Lock()
	if s.status == kernelv1.CoreStatus_CORE_STATUS_STARTING || s.status == kernelv1.CoreStatus_CORE_STATUS_RUNNING {
		s.mu.Unlock()
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("core is already starting or running"))
	}
	s.status = kernelv1.CoreStatus_CORE_STATUS_STARTING
	s.activeProfileID = profileID
	s.corePID = -1
	s.currentProfile = nil
	s.mu.Unlock()

	s.publish(kernelStateChangedEvent, map[string]any{"status": kernelv1.CoreStatus_CORE_STATUS_STARTING, "pid": -1})
	return nil
}

func (s *Service) setRunning(pid int, profileID string, profile *profilev1.Profile) {
	s.updateCoreState(func() {
		s.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
		s.activeProfileID = profileID
		s.corePID = pid
		s.currentProfile = cloneProfile(profile)
	})
}

func (s *Service) completeStart(pid int, profileID string, profile *profilev1.Profile) bool {
	s.stateEventMu.Lock()
	defer s.stateEventMu.Unlock()

	s.mu.Lock()
	if s.status != kernelv1.CoreStatus_CORE_STATUS_STARTING || s.corePID != pid {
		s.mu.Unlock()
		return false
	}
	s.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	s.activeProfileID = profileID
	s.currentProfile = cloneProfile(profile)
	s.mu.Unlock()

	s.publish(kernelStateChangedEvent, map[string]any{"status": kernelv1.CoreStatus_CORE_STATUS_RUNNING, "pid": pid})
	return true
}

func (s *Service) setStopped() {
	s.updateCoreState(func() {
		s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPED
		s.corePID = -1
		s.currentProfile = nil
	})
}

func (s *Service) setCrashed() {
	s.updateCoreState(func() {
		s.status = kernelv1.CoreStatus_CORE_STATUS_CRASHED
		s.corePID = -1
		s.currentProfile = nil
	})
}

func (s *Service) beginStopping() (int, error) {
	s.stateEventMu.Lock()
	defer s.stateEventMu.Unlock()

	s.mu.Lock()
	if s.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
		s.mu.Unlock()
		return -1, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("core is not running"))
	}
	pid := s.corePID
	s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPING
	s.mu.Unlock()

	s.publish(kernelStateChangedEvent, map[string]any{"status": kernelv1.CoreStatus_CORE_STATUS_STOPPING, "pid": -1})
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

	crashed := status != kernelv1.CoreStatus_CORE_STATUS_STOPPING
	crashPhase := "runtime"
	if status == kernelv1.CoreStatus_CORE_STATUS_STARTING {
		crashPhase = "startup"
	}
	if crashed {
		s.status = kernelv1.CoreStatus_CORE_STATUS_CRASHED
	} else {
		s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPED
	}
	s.corePID = -1
	s.currentProfile = nil
	status = s.status
	s.mu.Unlock()

	s.publish(kernelStateChangedEvent, map[string]any{"status": status, "pid": -1})
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

func (s *Service) Status() (kernelv1.CoreStatus, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.activeProfileID
}

func (s *Service) Restart(ctx context.Context, profileID string) error {
	_, err := s.RestartCore(ctx, connect.NewRequest(&kernelv1.RestartCoreRequest{ProfileId: profileID}))
	return err
}

func (s *Service) StartCore(
	ctx context.Context,
	req *connect.Request[kernelv1.StartCoreRequest],
) (*connect.Response[kernelv1.StartCoreResponse], error) {
	profileID := req.Msg.GetProfileId()
	if profileID == "" {
		return nil, rpcutil.AsConnectError(rpcutil.InvalidArgumentError{Message: "profile_id is required"})
	}

	profile, err := s.loadProfileByID(profileID)
	if err != nil {
		return nil, rpcutil.AsConnectError(err)
	}

	pid, err := s.startCoreWithProfile(ctx, profile)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&kernelv1.StartCoreResponse{Pid: int32(pid)}), nil
}

func (s *Service) StartCoreWithProfile(
	ctx context.Context,
	req *connect.Request[kernelv1.StartCoreWithProfileRequest],
) (*connect.Response[kernelv1.StartCoreWithProfileResponse], error) {
	profile := req.Msg.GetProfile()
	if profile == nil || profile.GetId() == "" {
		return nil, rpcutil.AsConnectError(rpcutil.InvalidArgumentError{Message: "profile is required"})
	}

	pid, err := s.startCoreWithProfile(ctx, profile)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&kernelv1.StartCoreWithProfileResponse{Pid: int32(pid)}), nil
}

func (s *Service) startCoreWithProfile(ctx context.Context, profile *profilev1.Profile) (int, error) {
	profileID := profile.GetId()

	if err := s.setStarting(profileID); err != nil {
		return -1, err
	}
	generatedConfig, err := s.config.Generate(profile, &kernelv1.GenerateConfigOptions{
		EnableMixinProcessing:  true,
		EnableScriptProcessing: true,
	})
	if err != nil {
		s.setStopped()
		return -1, rpcutil.AsConnectError(err)
	}

	config.FinalizeGeneratedConfig(generatedConfig)
	if err := s.config.WriteGeneratedConfig(generatedConfig); err != nil {
		s.setStopped()
		return -1, rpcutil.AsConnectError(err)
	}

	runtimeCfg, err := s.loadRuntimeConfig()
	if err != nil {
		s.setStopped()
		return -1, rpcutil.AsConnectError(err)
	}

	if err := validateKernelConfig(s.processes, runtimeCfg); err != nil {
		s.setStopped()
		return -1, connect.NewError(connect.CodeInvalidArgument, err)
	}

	execResult := s.processes.ExecBackground(
		coreWorkingDirectory+"/"+getKernelFileName(runtimeCfg.Branch == "alpha"),
		runtimeCfg.Args,
		"kernelLog",
		platform.ExecOptions{
			PIDFile:           corePidFilePath,
			StopOutputKeyword: coreStopOutputKeyword,
			Env:               runtimeCfg.Env,
			OnExit:            s.handleCoreProcessExit,
		},
	)
	if !execResult.Flag {
		s.setStopped()
		return -1, connect.NewError(connect.CodeInternal, fmt.Errorf("start core failed: %s", execResult.Data))
	}

	pid, convErr := parsePID(execResult.Data)
	if convErr != nil {
		s.setStopped()
		return -1, connect.NewError(connect.CodeInternal, convErr)
	}

	s.updateCoreState(func() {
		s.corePID = pid
	})

	if err := waitKernelAPIReadyFunc(ctx, config.CoreAPIController, s.config.ReadGeneratedSecret(), pid, 15*time.Second); err != nil {
		s.setCrashed()
		s.publishCoreCrash(pid, err.Error(), "startup")
		_ = s.processes.KillProcess(pid, 5)
		s.setStopped()
		return -1, connect.NewError(connect.CodeUnavailable, fmt.Errorf("kernel api is not ready: %w", err))
	}

	if !s.completeStart(pid, profileID, profile) {
		return -1, connect.NewError(connect.CodeUnavailable, fmt.Errorf("core process exited during startup"))
	}

	_ = ctx
	return pid, nil
}

func (s *Service) StopCore(
	_ context.Context,
	_ *connect.Request[kernelv1.StopCoreRequest],
) (*connect.Response[kernelv1.StopCoreResponse], error) {
	pid, err := s.beginStopping()
	if err != nil {
		return nil, err
	}

	result := s.processes.KillProcess(pid, 10)
	if !result.Flag {
		s.setCrashed()
		s.publishCoreCrash(pid, result.Data, "shutdown")
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("stop core failed: %s", result.Data))
	}

	s.setStopped()

	return connect.NewResponse(&kernelv1.StopCoreResponse{}), nil
}

func (s *Service) RestartCore(
	ctx context.Context,
	req *connect.Request[kernelv1.RestartCoreRequest],
) (*connect.Response[kernelv1.RestartCoreResponse], error) {
	if _, err := s.StopCore(ctx, connect.NewRequest(&kernelv1.StopCoreRequest{})); err != nil {
		return nil, err
	}

	profileID := req.Msg.GetProfileId()
	if profileID == "" {
		s.mu.Lock()
		profileID = s.activeProfileID
		s.mu.Unlock()
	}
	if profileID == "" {
		return nil, rpcutil.AsConnectError(rpcutil.InvalidArgumentError{Message: "profile_id is required for restart when no active profile exists"})
	}

	startResp, err := s.StartCore(ctx, connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: profileID}))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&kernelv1.RestartCoreResponse{Pid: startResp.Msg.GetPid()}), nil
}

func (s *Service) GetCoreStatus(
	_ context.Context,
	_ *connect.Request[kernelv1.GetCoreStatusRequest],
) (*connect.Response[kernelv1.GetCoreStatusResponse], error) {
	s.mu.Lock()
	status := s.status
	pid := s.corePID
	s.mu.Unlock()
	if status != kernelv1.CoreStatus_CORE_STATUS_RUNNING || pid <= 0 {
		pid = -1
	}
	return connect.NewResponse(&kernelv1.GetCoreStatusResponse{Status: status, Pid: int32(pid)}), nil
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
	_ context.Context,
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

	return connect.NewResponse(&kernelv1.GetCurrentCoreMemoryResponse{Rss: rss}), nil
}

func (s *Service) AutoStart(ctx context.Context) {
	appConfig := s.appConfig.Current()
	if !appConfig.AutoStartKernel {
		return
	}

	profileID := strings.TrimSpace(appConfig.Profile)
	if profileID == "" {
		log.Printf("AutoStartCore: skipped: no kernel profile selected")
		return
	}

	if s.attachExistingCoreFromPID(profileID) {
		return
	}

	log.Printf("AutoStartCore: starting core with profile %s", profileID)
	if _, err := s.StartCore(ctx, connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: profileID})); err != nil {
		log.Printf("AutoStartCore: failed: %v", err)
	}
}

func (s *Service) attachExistingCoreFromPID(profileID string) bool {
	bytes, err := os.ReadFile(s.processes.ResolvePath(corePidFilePath))
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(bytes)))
	if err != nil || pid <= 0 {
		return false
	}

	result := s.processes.ProcessInfo(int32(pid))
	if !result.Flag || !strings.HasPrefix(result.Data, "sing-box") {
		return false
	}

	s.setRunning(pid, profileID, nil)

	log.Printf("AutoStartCore: attached to existing core process %d", pid)
	return true
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

func validateKernelConfig(app ProcessRunner, runtimeCfg kernelRuntimeConfig) error {
	branchAlpha := runtimeCfg.Branch == "alpha"
	binary := coreWorkingDirectory + "/" + getKernelFileName(branchAlpha)

	result := app.Exec(binary, []string{
		"check",
		"--disable-color",
		"-c",
		app.ResolvePath(coreConfigRelativePath),
		"-D",
		app.ResolvePath(coreWorkingDirectory),
	}, platform.ExecOptions{
		WorkingDirectory: app.ResolvePath(coreWorkingDirectory),
		Env:              runtimeCfg.Env,
	})

	if result.Flag {
		return nil
	}

	msg := strings.TrimSpace(result.Data)
	if msg == "" {
		msg = "unknown error"
	}
	return fmt.Errorf("invalid core config: %s", msg)
}

func waitKernelAPIReady(ctx context.Context, controller, secret string, pid int, timeout time.Duration) error {
	target := normalizeKernelController(controller)
	endpoint := "http://" + target + "/configs"
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	var lastErr error
	proc, _ := os.FindProcess(pid)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("timeout waiting for %s: %w", endpoint, lastErr)
			}
			return fmt.Errorf("timeout waiting for %s", endpoint)
		}

		if proc != nil {
			alive, err := platform.IsProcessAlive(proc)
			if err == nil && !alive {
				if lastErr != nil {
					return fmt.Errorf("core process exited before API ready: %w", lastErr)
				}
				return fmt.Errorf("core process %s exited before API ready", strconv.Itoa(pid))
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		if secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}

		time.Sleep(250 * time.Millisecond)
	}
}

func normalizeKernelController(controller string) string {
	fallback := "127.0.0.1:20123"
	raw := strings.TrimSpace(controller)
	if raw == "" {
		return fallback
	}

	hostPort := raw
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return fallback
		}
		hostPort = u.Host
	}

	if strings.HasPrefix(hostPort, ":") {
		hostPort = "127.0.0.1" + hostPort
	}
	if !strings.Contains(hostPort, ":") {
		hostPort = hostPort + ":20123"
	}
	if strings.HasPrefix(hostPort, "0.0.0.0:") {
		hostPort = "127.0.0.1:" + strings.TrimPrefix(hostPort, "0.0.0.0:")
	}

	parts := strings.Split(hostPort, ":")
	port := parts[len(parts)-1]
	if port == "" {
		return fallback
	}
	for _, ch := range port {
		if ch < '0' || ch > '9' {
			return fallback
		}
	}

	return hostPort
}
