package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"guiforcores/bridge/rpcutil"
	"guiforcores/bridge/storage"
	appv1 "guiforcores/gen/app/v1"

	connect "connectrpc.com/connect"
	"gopkg.in/yaml.v3"
)

const appConfigPath = "data/config.yaml"

type AppConfig struct {
	AutoStartKernel   bool              `yaml:"autoStartKernel"`
	AutoRestartKernel bool              `yaml:"autoRestartKernel"`
	UserAgent         string            `yaml:"userAgent"`
	GitHubAPIToken    string            `yaml:"githubApiToken"`
	RollingRelease    bool              `yaml:"rollingRelease"`
	Branch            string            `yaml:"branch"`
	Profile           string            `yaml:"profile"`
	Main              CoreRuntimeConfig `yaml:"main"`
	Alpha             CoreRuntimeConfig `yaml:"alpha"`
}

type CoreRuntimeConfig struct {
	Env  map[string]string `yaml:"env"`
	Args []string          `yaml:"args"`
}

type Store struct {
	paths *storage.Paths
	mu    sync.RWMutex
	value AppConfig
}

type AppService struct {
	store         *Store
	changeHandler AppConfigChangeHandler
}

type AppConfigChangeHandler interface {
	AppConfigChanged(previous AppConfig, current AppConfig)
}

func NewStore(paths *storage.Paths) (*Store, error) {
	store := &Store{paths: paths}
	cfg, err := store.load()
	if err != nil {
		return nil, err
	}
	store.value = cfg
	return store, nil
}

func NewAppService(store *Store) *AppService {
	return &AppService{store: store}
}

func (s *AppService) SetChangeHandler(handler AppConfigChangeHandler) {
	s.changeHandler = handler
}

func defaultCoreRuntimeConfig() CoreRuntimeConfig {
	return CoreRuntimeConfig{
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

func normalizeCoreRuntimeConfig(cfg CoreRuntimeConfig) CoreRuntimeConfig {
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	if len(cfg.Args) == 0 {
		cfg.Args = append([]string{}, defaultCoreRuntimeConfig().Args...)
	}
	return cfg
}

func (s *Store) load() (AppConfig, error) {
	cfg := defaultAppConfig()
	data, err := os.ReadFile(s.paths.Resolve(appConfigPath))
	if err == nil {
		if len(strings.TrimSpace(string(data))) > 0 {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return cfg, err
			}
		}
		cfg = normalizeAppConfig(cfg)
		if err := s.write(cfg); err != nil {
			return cfg, err
		}
		return cfg, nil
	}
	if !os.IsNotExist(err) {
		return cfg, err
	}
	cfg = normalizeAppConfig(cfg)
	if err := s.write(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (s *Store) Current() AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneAppConfig(s.value)
}

func (s *Store) Save(cfg AppConfig) error {
	cfg = normalizeAppConfig(cfg)
	if err := s.write(cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.value = cloneAppConfig(cfg)
	s.mu.Unlock()
	return nil
}

func (s *Store) write(cfg AppConfig) error {
	fullPath := s.paths.Resolve(appConfigPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(normalizeAppConfig(cfg))
	if err != nil {
		return err
	}
	return os.WriteFile(fullPath, data, 0644)
}

func cloneAppConfig(cfg AppConfig) AppConfig {
	cfg.Main = CoreRuntimeConfig{
		Env:  cloneStringMap(cfg.Main.Env),
		Args: append([]string(nil), cfg.Main.Args...),
	}
	cfg.Alpha = CoreRuntimeConfig{
		Env:  cloneStringMap(cfg.Alpha.Env),
		Args: append([]string(nil), cfg.Alpha.Args...),
	}
	return cfg
}

func cloneStringMap(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func appConfigToProto(cfg AppConfig) *appv1.AppConfig {
	cfg = normalizeAppConfig(cfg)
	return &appv1.AppConfig{
		AutoStartKernel:   cfg.AutoStartKernel,
		AutoRestartKernel: cfg.AutoRestartKernel,
		UserAgent:         cfg.UserAgent,
		GithubApiToken:    cfg.GitHubAPIToken,
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
		GitHubAPIToken:    config.GetGithubApiToken(),
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

func coreRuntimeToProto(config CoreRuntimeConfig) *appv1.CoreRuntimeConfig {
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

func coreRuntimeFromProto(config *appv1.CoreRuntimeConfig, defaults CoreRuntimeConfig) CoreRuntimeConfig {
	if config == nil {
		return defaults
	}
	env := make(map[string]string, len(config.GetEnv()))
	for key, value := range config.GetEnv() {
		env[key] = value
	}
	out := CoreRuntimeConfig{
		Env:  env,
		Args: append([]string{}, config.GetArgs()...),
	}
	return normalizeCoreRuntimeConfig(out)
}

func (s *AppService) GetAppConfig(_ context.Context, _ *connect.Request[appv1.GetAppConfigRequest]) (*connect.Response[appv1.GetAppConfigResponse], error) {
	return connect.NewResponse(&appv1.GetAppConfigResponse{Config: appConfigToProto(s.store.Current())}), nil
}

func (s *AppService) SaveAppConfig(_ context.Context, req *connect.Request[appv1.SaveAppConfigRequest]) (*connect.Response[appv1.SaveAppConfigResponse], error) {
	previous := s.store.Current()
	cfg := appConfigFromProto(req.Msg.GetConfig())
	if err := s.store.Save(cfg); err != nil {
		return nil, rpcutil.AsConnectError(err)
	}
	current := s.store.Current()
	if s.changeHandler != nil {
		s.changeHandler.AppConfigChanged(previous, current)
	}
	return connect.NewResponse(&appv1.SaveAppConfigResponse{Config: appConfigToProto(current)}), nil
}
