package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"guiforcores/gen/app/v1/appv1connect"
	"guiforcores/gen/kernel/v1/kernelv1connect"
	configv1 "guiforcores/gen/profile/v1"
	"guiforcores/gen/profile/v1/profilev1connect"

	connect "connectrpc.com/connect"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

const (
	coreConfigFilePath         = "data/sing-box/config.json"
	coreGeneratedCacheFilePath = "cache.db"
	defaultGeneratedAppTitle   = "webui.for.singbox"
)

type invalidArgumentError struct {
	message string
}

func (e invalidArgumentError) Error() string {
	return e.message
}

type configService struct {
	app *App
}

func registerConfigRPCRoutes(mux *http.ServeMux, app *App) *kernelService {
	configSvc := &configService{app: app}
	profileMgmtSvc := &profileManagementService{app: app}
	kernelSvc := newKernelService(app, configSvc, profileMgmtSvc)
	appRuntimeSvc := newAppRuntimeService(app, kernelSvc)

	path, handler := kernelv1connect.NewKernelConfigServiceHandler(configSvc)
	mux.Handle("/api/rpc"+path, http.StripPrefix("/api/rpc", handler))

	path, handler = profilev1connect.NewProfileManagementServiceHandler(profileMgmtSvc)
	mux.Handle("/api/rpc"+path, http.StripPrefix("/api/rpc", handler))

	path, handler = kernelv1connect.NewKernelServiceHandler(kernelSvc)
	mux.Handle("/api/rpc"+path, http.StripPrefix("/api/rpc", handler))

	path, handler = appv1connect.NewAppSettingsServiceHandler(appRuntimeSvc)
	mux.Handle("/api/rpc"+path, http.StripPrefix("/api/rpc", handler))

	path, handler = appv1connect.NewAppConfigServiceHandler(appRuntimeSvc)
	mux.Handle("/api/rpc"+path, http.StripPrefix("/api/rpc", handler))

	path, handler = appv1connect.NewSubscriptionServiceHandler(appRuntimeSvc)
	mux.Handle("/api/rpc"+path, http.StripPrefix("/api/rpc", handler))

	path, handler = appv1connect.NewRuleSetServiceHandler(appRuntimeSvc)
	mux.Handle("/api/rpc"+path, http.StripPrefix("/api/rpc", handler))

	path, handler = appv1connect.NewScheduledTaskServiceHandler(appRuntimeSvc)
	mux.Handle("/api/rpc"+path, http.StripPrefix("/api/rpc", handler))

	appRuntimeSvc.StartScheduler()

	return kernelSvc
}

func (s *configService) GenerateDnsServerUrl(
	_ context.Context,
	req *connect.Request[configv1.GenerateDnsServerUrlRequest],
) (*connect.Response[configv1.GenerateDnsServerUrlResponse], error) {
	if req.Msg == nil || req.Msg.GetDnsServer() == nil {
		return nil, asConnectError(invalidArgumentError{message: "dns_server is required"})
	}

	url, err := generateDNSServiceURL(req.Msg.GetDnsServer())
	if err != nil {
		return nil, asConnectError(err)
	}

	return connect.NewResponse(&configv1.GenerateDnsServerUrlResponse{Url: url}), nil
}

func (s *configService) GenerateConfig(
	_ context.Context,
	req *connect.Request[configv1.GenerateConfigRequest],
) (*connect.Response[configv1.GenerateConfigResponse], error) {
	config, err := s.generateConfig(req.Msg.GetProfile(), req.Msg.GetOptions())
	if err != nil {
		return nil, asConnectError(err)
	}

	configStruct, err := structpb.NewStruct(config)
	if err != nil {
		return nil, asConnectError(fmt.Errorf("build response struct: %w", err))
	}

	return connect.NewResponse(&configv1.GenerateConfigResponse{Config: configStruct}), nil
}

func (s *configService) GenerateConfigFile(
	_ context.Context,
	req *connect.Request[configv1.GenerateConfigFileRequest],
) (*connect.Response[configv1.GenerateConfigFileResponse], error) {
	config, err := s.generateConfig(req.Msg.GetProfile(), req.Msg.GetOptions())
	if err != nil {
		return nil, asConnectError(err)
	}

	finalizeGeneratedConfig(config)
	if err := writeGeneratedConfigFile(config); err != nil {
		return nil, asConnectError(err)
	}

	configStruct, err := structpb.NewStruct(config)
	if err != nil {
		return nil, asConnectError(fmt.Errorf("build response struct: %w", err))
	}

	return connect.NewResponse(&configv1.GenerateConfigFileResponse{
		Path:   coreConfigFilePath,
		Config: configStruct,
	}), nil
}

func (s *configService) generateConfig(
	profile *configv1.Profile,
	options *configv1.GenerateConfigOptions,
) (map[string]any, error) {
	if profile == nil {
		return nil, invalidArgumentError{message: "profile is required"}
	}

	normalized := normalizeGenerateOptions(options)

	generator, err := newConfigGenerator(s.app)
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

func normalizeGenerateOptions(options *configv1.GenerateConfigOptions) generateOptions {
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

func finalizeGeneratedConfig(config map[string]any) {
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

func ensureChildMap(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	child := map[string]any{}
	parent[key] = child
	return child
}

func writeGeneratedConfigFile(config map[string]any) error {
	payload := map[string]any{"$schema": generatedSchemaHeader()}
	for key, value := range config {
		payload[key] = value
	}

	bytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal generated config: %w", err)
	}

	fullPath := GetPath(coreConfigFilePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), os.ModePerm); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(fullPath, bytes, 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

func generatedSchemaHeader() string {
	if Env.AppName != "" {
		return "DO NOT EDIT - Generated by " + Env.AppName
	}
	return "DO NOT EDIT - Generated by " + defaultGeneratedAppTitle
}

func asConnectError(err error) error {
	if err == nil {
		return nil
	}
	var invalid invalidArgumentError
	if errors.As(err, &invalid) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return err
	}
	return connect.NewError(connect.CodeInternal, err)
}
