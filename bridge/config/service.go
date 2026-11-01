package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"guiforcores/bridge/logging"
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
	plugins ConfigPlugins
}

type ConfigPlugins interface {
	ApplyConfig(context.Context, map[string]any) (map[string]any, error)
}

// SetPlugins is configured once, before the application starts serving requests.
func (s *Service) SetPlugins(value ConfigPlugins) { s.plugins = value }

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
	ctx context.Context,
	req *connect.Request[kernelv1.GenerateConfigRequest],
) (response *connect.Response[kernelv1.GenerateConfigResponse], responseErr error) {
	started := time.Now()
	profileID := req.Msg.GetProfile().GetId()
	defer func() {
		logging.Complete(ctx, "config", "generate", "core configuration generated", started, responseErr, "profile_id", profileID)
	}()
	config, err := s.GenerateContext(ctx, req.Msg.GetProfile(), req.Msg.GetOptions())
	if err != nil {
		return nil, asGenerateError(err)
	}

	configStruct, err := structpb.NewStruct(config)
	if err != nil {
		return nil, asConnectError(fmt.Errorf("build response struct: %w", err))
	}

	return connect.NewResponse(&kernelv1.GenerateConfigResponse{Config: configStruct}), nil
}

func (s *Service) GenerateConfigFile(
	ctx context.Context,
	req *connect.Request[kernelv1.GenerateConfigFileRequest],
) (response *connect.Response[kernelv1.GenerateConfigFileResponse], responseErr error) {
	started := time.Now()
	profileID := req.Msg.GetProfile().GetId()
	defer func() {
		logging.Complete(ctx, "config", "generate_file", "core configuration file generated", started, responseErr, "profile_id", profileID, "path", CoreConfigFilePath)
	}()
	config, err := s.GenerateContext(ctx, req.Msg.GetProfile(), req.Msg.GetOptions())
	if err != nil {
		return nil, asGenerateError(err)
	}

	FinalizeGeneratedConfig(config)
	if err := s.WriteGeneratedConfig(config); err != nil {
		return nil, asConnectError(err)
	}

	configStruct, err := structpb.NewStruct(RedactGeneratedConfig(config))
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
	return s.GenerateContext(context.Background(), profile, options)
}

func asGenerateError(err error) error {
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	return asConnectError(err)
}

func (s *Service) GenerateContext(ctx context.Context, profile *profilev1.Profile, options *kernelv1.GenerateConfigOptions) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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

	if normalized.enableMixinProcessing {
		config, err = applyMixin(config, profile.GetMixin())
		if err != nil {
			return nil, err
		}
	}

	if normalized.enableScriptProcessing {
		config, err = applyScript(ctx, config, profile.GetScript())
		if err != nil {
			return nil, err
		}
	}

	if s.plugins != nil {
		config, err = s.plugins.ApplyConfig(ctx, config)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, invalidArgumentError{message: "configuration plugin: " + err.Error()}
		}
		if config == nil {
			return nil, invalidArgumentError{message: "configuration plugin returned a null configuration"}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := normalizeRuleSetHTTPClients(config); err != nil {
		return nil, err
	}

	if err := EnforceNativeAPIConfig(config); err != nil {
		return nil, err
	}
	return config, nil
}

type generateOptions struct {
	enableMixinProcessing  bool
	enableScriptProcessing bool
}

func normalizeGenerateOptions(options *kernelv1.GenerateConfigOptions) generateOptions {
	if options == nil {
		return generateOptions{enableMixinProcessing: true}
	}

	return generateOptions{
		enableMixinProcessing:  options.GetEnableMixinProcessing(),
		enableScriptProcessing: options.GetEnableScriptProcessing(),
	}
}

func FinalizeGeneratedConfig(config map[string]any) {
	// Native control/readiness no longer depends on console logs. Honor the
	// selected logging settings; only the cache location belongs to the app.
	logConfig := ensureChildMap(config, "log")
	if level, exists := logConfig["level"]; !exists || level == "" {
		logConfig["level"] = logLevelInfo
	}
	experimental := ensureChildMap(config, "experimental")
	cacheFile := ensureChildMap(experimental, "cache_file")
	cacheFile["path"] = coreGeneratedCacheFilePath
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
	if err := os.WriteFile(fullPath, bytes, 0600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	if err := os.Chmod(fullPath, 0600); err != nil {
		return fmt.Errorf("restrict config file permissions: %w", err)
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
