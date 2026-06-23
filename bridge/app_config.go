package bridge

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"

	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
	"gopkg.in/yaml.v3"
)

const appConfigPath = "data/config.yaml"

func defaultCoreRuntimeConfig() AppCoreRuntimeConfig {
	return AppCoreRuntimeConfig{
		Env: map[string]string{},
		Args: []string{
			"run",
			"--disable-color",
			"-c",
			"$APP_BASE_PATH/$CORE_BASE_PATH/config.json",
			"-D",
			"$APP_BASE_PATH/$CORE_BASE_PATH",
		},
	}
}

func defaultAppConfig() AppConfig {
	return AppConfig{
		RollingRelease: true,
		Branch:         "main",
		Main:           defaultCoreRuntimeConfig(),
		Alpha:          defaultCoreRuntimeConfig(),
	}
}

func normalizeAppConfig(cfg AppConfig) AppConfig {
	cfg.Branch = strings.TrimSpace(cfg.Branch)
	if cfg.Branch != "alpha" {
		cfg.Branch = "main"
	}
	cfg.Profile = strings.TrimSpace(cfg.Profile)
	cfg.Main = normalizeCoreRuntimeConfig(cfg.Main)
	cfg.Alpha = normalizeCoreRuntimeConfig(cfg.Alpha)
	return cfg
}

func normalizeCoreRuntimeConfig(cfg AppCoreRuntimeConfig) AppCoreRuntimeConfig {
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	if len(cfg.Args) == 0 {
		cfg.Args = append([]string{}, defaultCoreRuntimeConfig().Args...)
	}
	return cfg
}

func loadConfig() {
	cfg, err := loadAppConfigFromDisk()
	if err != nil {
		log.Printf("load config: %v", err)
		cfg = defaultAppConfig()
	}
	cfg = normalizeAppConfig(cfg)
	*Config = cfg
}

func loadAppConfigFromDisk() (AppConfig, error) {
	cfg := defaultAppConfig()
	data, err := os.ReadFile(GetPath(appConfigPath))
	if err == nil {
		if len(strings.TrimSpace(string(data))) > 0 {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return cfg, err
			}
		}
		cfg = normalizeAppConfig(cfg)
		if err := writeAppConfigFile(cfg); err != nil {
			return cfg, err
		}
		return cfg, nil
	}
	if !os.IsNotExist(err) {
		return cfg, err
	}
	cfg = normalizeAppConfig(cfg)
	if err := writeAppConfigFile(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func SaveConfig() error {
	cfg := normalizeAppConfig(*Config)
	*Config = cfg
	return writeAppConfigFile(cfg)
}

func writeAppConfigFile(cfg AppConfig) error {
	fullPath := GetPath(appConfigPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(normalizeAppConfig(cfg))
	if err != nil {
		return err
	}
	return os.WriteFile(fullPath, data, 0644)
}

func appConfigToProto(cfg AppConfig) *appv1.AppConfig {
	cfg = normalizeAppConfig(cfg)
	return &appv1.AppConfig{
		AutoStartKernel:   cfg.AutoStartKernel,
		AutoRestartKernel: cfg.AutoRestartKernel,
		UserAgent:         cfg.UserAgent,
		GithubApiToken:    cfg.GitHubApiToken,
		RollingRelease:    cfg.RollingRelease,
		Branch:            branchToProto(cfg.Branch),
		Profile:           cfg.Profile,
		Main:              coreRuntimeToProto(cfg.Main),
		Alpha:             coreRuntimeToProto(cfg.Alpha),
	}
}

func appConfigFromProto(config *appv1.AppConfig) AppConfig {
	defaults := defaultAppConfig()
	if config == nil {
		return defaults
	}
	return normalizeAppConfig(AppConfig{
		AutoStartKernel:   config.GetAutoStartKernel(),
		AutoRestartKernel: config.GetAutoRestartKernel(),
		UserAgent:         config.GetUserAgent(),
		GitHubApiToken:    config.GetGithubApiToken(),
		RollingRelease:    config.GetRollingRelease(),
		Branch:            branchFromProto(config.GetBranch()),
		Profile:           config.GetProfile(),
		Main:              coreRuntimeFromProto(config.GetMain(), defaults.Main),
		Alpha:             coreRuntimeFromProto(config.GetAlpha(), defaults.Alpha),
	})
}

func branchToProto(branch string) appv1.KernelBranch {
	if branch == "alpha" {
		return appv1.KernelBranch_KERNEL_BRANCH_ALPHA
	}
	return appv1.KernelBranch_KERNEL_BRANCH_MAIN
}

func branchFromProto(branch appv1.KernelBranch) string {
	if branch == appv1.KernelBranch_KERNEL_BRANCH_ALPHA {
		return "alpha"
	}
	return "main"
}

func coreRuntimeToProto(config AppCoreRuntimeConfig) *appv1.CoreRuntimeConfig {
	config = normalizeCoreRuntimeConfig(config)
	env := make(map[string]string, len(config.Env))
	for key, value := range config.Env {
		env[key] = value
	}
	return &appv1.CoreRuntimeConfig{
		Env:  env,
		Args: append([]string{}, config.Args...),
	}
}

func coreRuntimeFromProto(config *appv1.CoreRuntimeConfig, defaults AppCoreRuntimeConfig) AppCoreRuntimeConfig {
	if config == nil {
		return defaults
	}
	env := make(map[string]string, len(config.GetEnv()))
	for key, value := range config.GetEnv() {
		env[key] = value
	}
	out := AppCoreRuntimeConfig{
		Env:  env,
		Args: append([]string{}, config.GetArgs()...),
	}
	return normalizeCoreRuntimeConfig(out)
}

func (s *appRuntimeService) GetAppConfig(ctx context.Context, req *connect.Request[appv1.GetAppConfigRequest]) (*connect.Response[appv1.GetAppConfigResponse], error) {
	return connect.NewResponse(&appv1.GetAppConfigResponse{Config: appConfigToProto(*Config)}), nil
}

func (s *appRuntimeService) SaveAppConfig(ctx context.Context, req *connect.Request[appv1.SaveAppConfigRequest]) (*connect.Response[appv1.SaveAppConfigResponse], error) {
	cfg := appConfigFromProto(req.Msg.GetConfig())
	*Config = cfg
	if err := writeAppConfigFile(cfg); err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&appv1.SaveAppConfigResponse{Config: appConfigToProto(*Config)}), nil
}
