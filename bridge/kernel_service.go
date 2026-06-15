package bridge

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

	kernelv1 "guiforcores/gen/kernel/v1"
	configv1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
	"gopkg.in/yaml.v3"
)

const (
	coreWorkingDirectory   = "data/sing-box"
	corePidFilePath        = coreWorkingDirectory + "/pid.txt"
	coreStopOutputKeyword  = "sing-box started"
	coreConfigRelativePath = coreWorkingDirectory + "/config.json"
)

type kernelRuntimeConfig struct {
	Branch string
	Args   []string
	Env    map[string]string
}

type kernelService struct {
	app        *App
	configSvc  *configService
	profileSvc *profileManagementService

	mu              sync.Mutex
	status          kernelv1.CoreStatus
	activeProfileID string
	corePID         int
}

func newKernelService(app *App, configSvc *configService, profileSvc *profileManagementService) *kernelService {
	return &kernelService{
		app:        app,
		configSvc:  configSvc,
		profileSvc: profileSvc,
		status:     kernelv1.CoreStatus_CORE_STATUS_STOPPED,
		corePID:    -1,
	}
}

func (s *kernelService) StartCore(
	ctx context.Context,
	req *connect.Request[kernelv1.StartCoreRequest],
) (*connect.Response[kernelv1.StartCoreResponse], error) {
	s.mu.Lock()
	if s.status == kernelv1.CoreStatus_CORE_STATUS_STARTING || s.status == kernelv1.CoreStatus_CORE_STATUS_RUNNING {
		s.mu.Unlock()
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("core is already starting or running"))
	}

	profileID := req.Msg.GetProfileId()
	if profileID == "" {
		s.mu.Unlock()
		return nil, asConnectError(invalidArgumentError{message: "profile_id is required"})
	}

	s.status = kernelv1.CoreStatus_CORE_STATUS_STARTING
	s.activeProfileID = profileID
	s.mu.Unlock()
	Hub.Emit("kernelStarting", map[string]any{"profileId": profileID})

	profile, err := s.loadProfileByID(profileID)
	if err != nil {
		s.setStatus(kernelv1.CoreStatus_CORE_STATUS_STOPPED)
		return nil, asConnectError(err)
	}

	config, err := s.configSvc.generateConfig(profile, &configv1.GenerateConfigOptions{
		EnableMixinProcessing:  true,
		EnableScriptProcessing: true,
	})
	if err != nil {
		s.setStatus(kernelv1.CoreStatus_CORE_STATUS_STOPPED)
		return nil, asConnectError(err)
	}

	finalizeGeneratedConfig(config)
	if err := writeGeneratedConfigFile(config); err != nil {
		s.setStatus(kernelv1.CoreStatus_CORE_STATUS_STOPPED)
		return nil, asConnectError(err)
	}

	runtimeCfg, err := loadKernelRuntimeConfig()
	if err != nil {
		s.setStatus(kernelv1.CoreStatus_CORE_STATUS_STOPPED)
		return nil, asConnectError(err)
	}

	if err := validateKernelConfig(s.app, runtimeCfg); err != nil {
		s.setStatus(kernelv1.CoreStatus_CORE_STATUS_STOPPED)
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	execResult := s.app.ExecBackground(
		coreWorkingDirectory+"/"+getKernelFileName(runtimeCfg.Branch == "alpha"),
		runtimeCfg.Args,
		"kernelLog",
		"kernelStopped",
		ExecOptions{
			PidFile:           corePidFilePath,
			StopOutputKeyword: coreStopOutputKeyword,
			Env:               runtimeCfg.Env,
		},
	)
	if !execResult.Flag {
		s.setStatus(kernelv1.CoreStatus_CORE_STATUS_STOPPED)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("start core failed: %s", execResult.Data))
	}

	pid, convErr := parsePID(execResult.Data)
	if convErr != nil {
		s.setStatus(kernelv1.CoreStatus_CORE_STATUS_STOPPED)
		return nil, connect.NewError(connect.CodeInternal, convErr)
	}

	s.mu.Lock()
	s.corePID = pid
	s.mu.Unlock()

	controller := "127.0.0.1:20123"
	secret := ""
	if exp := profile.GetExperimental(); exp != nil {
		if clash := exp.GetClashApi(); clash != nil {
			if v := strings.TrimSpace(clash.GetExternalController()); v != "" {
				controller = v
			}
			secret = strings.TrimSpace(clash.GetSecret())
		}
	}
	if configSecret := readKernelBearerFromGeneratedConfig(); configSecret != "" {
		secret = configSecret
	}

	if err := waitKernelAPIReady(ctx, controller, secret, pid, 15*time.Second); err != nil {
		s.setStatus(kernelv1.CoreStatus_CORE_STATUS_CRASHED)
		Hub.Emit("kernelCrashed", map[string]any{"pid": pid, "reason": err.Error()})
		_ = s.app.KillProcess(pid, 5)
		s.mu.Lock()
		s.corePID = -1
		s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPED
		s.mu.Unlock()
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("kernel api is not ready: %w", err))
	}

	s.mu.Lock()
	s.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	s.mu.Unlock()

	Hub.Emit("kernelStarted", map[string]any{"pid": pid, "profileId": profileID})
	_ = ctx
	return connect.NewResponse(&kernelv1.StartCoreResponse{}), nil
}

func (s *kernelService) StopCore(
	_ context.Context,
	_ *connect.Request[kernelv1.StopCoreRequest],
) (*connect.Response[kernelv1.StopCoreResponse], error) {
	s.mu.Lock()
	if s.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
		s.mu.Unlock()
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("core is not running"))
	}
	pid := s.corePID
	s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPING
	s.mu.Unlock()

	result := s.app.KillProcess(pid, 10)
	if !result.Flag {
		s.setStatus(kernelv1.CoreStatus_CORE_STATUS_CRASHED)
		Hub.Emit("kernelCrashed", map[string]any{"pid": pid, "reason": result.Data})
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("stop core failed: %s", result.Data))
	}

	s.mu.Lock()
	s.corePID = -1
	s.status = kernelv1.CoreStatus_CORE_STATUS_STOPPED
	s.mu.Unlock()

	Hub.Emit("kernelStopped", map[string]any{"pid": pid})
	return connect.NewResponse(&kernelv1.StopCoreResponse{}), nil
}

func (s *kernelService) RestartCore(
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
		return nil, asConnectError(invalidArgumentError{message: "profile_id is required for restart when no active profile exists"})
	}

	if _, err := s.StartCore(ctx, connect.NewRequest(&kernelv1.StartCoreRequest{ProfileId: profileID})); err != nil {
		return nil, err
	}
	return connect.NewResponse(&kernelv1.RestartCoreResponse{}), nil
}

func (s *kernelService) GetCoreStatus(
	_ context.Context,
	_ *connect.Request[kernelv1.GetCoreStatusRequest],
) (*connect.Response[kernelv1.GetCoreStatusResponse], error) {
	s.mu.Lock()
	status := s.status
	s.mu.Unlock()
	return connect.NewResponse(&kernelv1.GetCoreStatusResponse{Status: status}), nil
}

func (s *kernelService) autoStartCoreOnLaunch(ctx context.Context) {
	if !Config.AutoStartKernel {
		return
	}

	profileID, err := loadSelectedKernelProfileID()
	if err != nil {
		log.Printf("AutoStartCore: skipped: %v", err)
		return
	}
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

func loadSelectedKernelProfileID() (string, error) {
	bytes, err := os.ReadFile(GetPath("data/user.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read user.yaml: %w", err)
	}

	var root map[string]any
	if err := yaml.Unmarshal(bytes, &root); err != nil {
		return "", fmt.Errorf("parse user.yaml: %w", err)
	}

	kernel, _ := root["kernel"].(map[string]any)
	profileID, _ := kernel["profile"].(string)
	return strings.TrimSpace(profileID), nil
}

func (s *kernelService) attachExistingCoreFromPID(profileID string) bool {
	bytes, err := os.ReadFile(GetPath(corePidFilePath))
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(bytes)))
	if err != nil || pid <= 0 {
		return false
	}

	result := s.app.ProcessInfo(int32(pid))
	if !result.Flag || !strings.HasPrefix(result.Data, "sing-box") {
		return false
	}

	s.mu.Lock()
	s.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	s.activeProfileID = profileID
	s.corePID = pid
	s.mu.Unlock()

	log.Printf("AutoStartCore: attached to existing core process %d", pid)
	return true
}

func (s *kernelService) loadProfileByID(id string) (*configv1.Profile, error) {
	s.profileSvc.mu.Lock()
	defer s.profileSvc.mu.Unlock()

	profiles, err := s.profileSvc.loadProfiles()
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if profile != nil && profile.GetId() == id {
			return cloneProfile(profile), nil
		}
	}
	return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("profile %q not found", id))
}

func (s *kernelService) setStatus(status kernelv1.CoreStatus) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

func loadKernelRuntimeConfig() (kernelRuntimeConfig, error) {
	cfg := kernelRuntimeConfig{
		Branch: "main",
		Args: []string{
			"run",
			"--disable-color",
			"-c",
			"$APP_BASE_PATH/$CORE_BASE_PATH/config.json",
			"-D",
			"$APP_BASE_PATH/$CORE_BASE_PATH",
		},
		Env: map[string]string{},
	}

	bytes, err := os.ReadFile(GetPath("data/user.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return normalizeKernelRuntime(cfg), nil
		}
		return cfg, fmt.Errorf("read user.yaml: %w", err)
	}

	var root map[string]any
	if err := yaml.Unmarshal(bytes, &root); err != nil {
		return cfg, fmt.Errorf("parse user.yaml: %w", err)
	}

	kernel, _ := root["kernel"].(map[string]any)
	if branch, ok := kernel["branch"].(string); ok && branch != "" {
		cfg.Branch = branch
	}

	target := "main"
	if cfg.Branch == "alpha" {
		target = "alpha"
	}
	runtimeNode, _ := kernel[target].(map[string]any)

	if rawArgs, ok := runtimeNode["args"].([]any); ok {
		args := make([]string, 0, len(rawArgs))
		for _, item := range rawArgs {
			if str, ok := item.(string); ok {
				args = append(args, str)
			}
		}
		if len(args) > 0 {
			cfg.Args = args
		}
	}

	if rawEnv, ok := runtimeNode["env"].(map[string]any); ok {
		env := make(map[string]string, len(rawEnv))
		for k, v := range rawEnv {
			env[k] = fmt.Sprint(v)
		}
		cfg.Env = env
	}

	return normalizeKernelRuntime(cfg), nil
}

func normalizeKernelRuntime(cfg kernelRuntimeConfig) kernelRuntimeConfig {
	replacer := strings.NewReplacer(
		"$APP_BASE_PATH", Env.BasePath,
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

func validateKernelConfig(app *App, runtimeCfg kernelRuntimeConfig) error {
	branchAlpha := runtimeCfg.Branch == "alpha"
	binary := coreWorkingDirectory + "/" + getKernelFileName(branchAlpha)

	result := app.Exec(binary, []string{
		"check",
		"--disable-color",
		"-c",
		GetPath(coreConfigRelativePath),
		"-D",
		GetPath(coreWorkingDirectory),
	}, ExecOptions{
		WorkingDirectory: GetPath(coreWorkingDirectory),
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
			alive, err := IsProcessAlive(proc)
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

		if configSecret := readKernelBearerFromGeneratedConfig(); configSecret != "" {
			secret = configSecret
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
