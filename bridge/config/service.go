package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"guiforcores/bridge/rpcutil"
	"guiforcores/bridge/storage"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

const (
	CoreConfigFilePath         = "data/sing-box/config.json"
	coreGeneratedCacheFilePath = "cache.db"
	defaultGeneratedAppTitle   = "webui.for.singbox"
)

type Service struct {
	paths   *storage.Paths
	appName string
}

func NewService(paths *storage.Paths, appName string) *Service {
	return &Service{paths: paths, appName: appName}
}

func (s *Service) GenerateDnsServerUrl(
	_ context.Context,
	req *connect.Request[kernelv1.GenerateDnsServerUrlRequest],
) (*connect.Response[kernelv1.GenerateDnsServerUrlResponse], error) {
	if req.Msg == nil || req.Msg.GetDnsServer() == nil {
		return nil, asConnectError(invalidArgumentError{message: "dns_server is required"})
	}

	url, err := generateDNSServiceURL(req.Msg.GetDnsServer())
	if err != nil {
		return nil, asConnectError(err)
	}

	return connect.NewResponse(&kernelv1.GenerateDnsServerUrlResponse{Url: url}), nil
}

func (s *Service) GenerateConfig(
	_ context.Context,
	req *connect.Request[kernelv1.GenerateConfigRequest],
) (*connect.Response[kernelv1.GenerateConfigResponse], error) {
	config, err := s.Generate(req.Msg.GetProfile(), req.Msg.GetOptions())
	if err != nil {
		return nil, asConnectError(err)
	}

	configStruct, err := structpb.NewStruct(config)
	if err != nil {
		return nil, asConnectError(fmt.Errorf("build response struct: %w", err))
	}

	return connect.NewResponse(&kernelv1.GenerateConfigResponse{Config: configStruct}), nil
}

func (s *Service) GenerateConfigFile(
	_ context.Context,
	req *connect.Request[kernelv1.GenerateConfigFileRequest],
) (*connect.Response[kernelv1.GenerateConfigFileResponse], error) {
	config, err := s.Generate(req.Msg.GetProfile(), req.Msg.GetOptions())
	if err != nil {
		return nil, asConnectError(err)
	}

	FinalizeGeneratedConfig(config)
	if err := s.WriteGeneratedConfig(config); err != nil {
		return nil, asConnectError(err)
	}

	configStruct, err := structpb.NewStruct(config)
	if err != nil {
		return nil, asConnectError(fmt.Errorf("build response struct: %w", err))
	}

	return connect.NewResponse(&kernelv1.GenerateConfigFileResponse{
		Path:   CoreConfigFilePath,
		Config: configStruct,
	}), nil
}

func (s *Service) Generate(
	profile *profilev1.Profile,
	options *kernelv1.GenerateConfigOptions,
) (map[string]any, error) {
	if profile == nil {
		return nil, invalidArgumentError{message: "profile is required"}
	}

	normalized := normalizeGenerateOptions(options)

	generator, err := newConfigGenerator(s.paths)
	if err != nil {
		return nil, err
	}

	config, err := generator.GenerateConfig(profile)
	if err != nil {
		return nil, err
	}

	if normalized.enableStableConfigCompat {
		adaptToStableBranch(config)
	}

	if normalized.enableMixinProcessing {
		config, err = applyMixin(config, profile.GetMixin())
		if err != nil {
			return nil, err
		}
	}

	if normalized.enableScriptProcessing {
		config, err = applyScript(config, profile.GetScript())
		if err != nil {
			return nil, err
		}
	}

	return config, nil
}

type generateOptions struct {
	enableStableConfigCompat bool
	enableMixinProcessing    bool
	enableScriptProcessing   bool
}

func normalizeGenerateOptions(options *kernelv1.GenerateConfigOptions) generateOptions {
	if options == nil {
		return generateOptions{enableMixinProcessing: true}
	}

	return generateOptions{
		enableStableConfigCompat: options.GetEnableStableConfigCompat(),
		enableMixinProcessing:    options.GetEnableMixinProcessing(),
		enableScriptProcessing:   options.GetEnableScriptProcessing(),
	}
}

func adaptToStableBranch(_ map[string]any) {}

func FinalizeGeneratedConfig(config map[string]any) {
	logConfig := ensureChildMap(config, "log")
	logConfig["disabled"] = false
	logConfig["output"] = ""

	level, _ := logConfig["level"].(string)
	switch level {
	case logLevelTrace, logLevelDebug, logLevelInfo:
	default:
		logConfig["level"] = logLevelInfo
	}

	experimental := ensureChildMap(config, "experimental")
	cacheFile := ensureChildMap(experimental, "cache_file")
	cacheFile["path"] = coreGeneratedCacheFilePath
}

func (s *Service) ReadGeneratedSecret() string {
	data, err := os.ReadFile(s.paths.Resolve(CoreConfigFilePath))
	if err != nil || len(data) == 0 {
		return ""
	}
	var root map[string]any
	if json.Unmarshal(data, &root) != nil {
		return ""
	}
	experimental, _ := root["experimental"].(map[string]any)
	clashAPI, _ := experimental["clash_api"].(map[string]any)
	secret, _ := clashAPI["secret"].(string)
	return strings.TrimSpace(secret)
}

func ensureChildMap(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	child := map[string]any{}
	parent[key] = child
	return child
}

func (s *Service) WriteGeneratedConfig(config map[string]any) error {
	payload := map[string]any{"$schema": s.generatedSchemaHeader()}
	for key, value := range config {
		payload[key] = value
	}

	bytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal generated config: %w", err)
	}

	fullPath := s.paths.Resolve(CoreConfigFilePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), os.ModePerm); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(fullPath, bytes, 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

func (s *Service) generatedSchemaHeader() string {
	if s.appName != "" {
		return "DO NOT EDIT - Generated by " + s.appName
	}
	return "DO NOT EDIT - Generated by " + defaultGeneratedAppTitle
}

func asConnectError(err error) error {
	if invalid, ok := err.(invalidArgumentError); ok {
		return rpcutil.AsConnectError(rpcutil.InvalidArgumentError{Message: invalid.message})
	}
	return rpcutil.AsConnectError(err)
}
