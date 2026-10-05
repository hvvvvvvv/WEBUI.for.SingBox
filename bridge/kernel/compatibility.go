package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/rpcutil"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"

	"connectrpc.com/connect"
)

const MinimumCoreVersion = "1.14.2"

var semanticCoreVersionRE = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func requireCompatibleCoreVersion(version string) error {
	matches := semanticCoreVersionRE.FindStringSubmatch(strings.TrimSpace(version))
	if matches == nil {
		return fmt.Errorf("cannot identify sing-box version %q; install sing-box %s or newer", version, MinimumCoreVersion)
	}
	for _, identifier := range strings.Split(matches[4], ".") {
		if len(identifier) > 1 && identifier[0] == '0' {
			if _, err := strconv.ParseUint(identifier, 10, 64); err == nil {
				return fmt.Errorf("invalid sing-box semantic version %q", version)
			}
		}
	}
	values := [3]uint64{}
	for index := range values {
		value, err := strconv.ParseUint(matches[index+1], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid sing-box version %q", version)
		}
		values[index] = value
	}
	minimum := [3]uint64{1, 14, 2}
	for index, value := range values {
		if value > minimum[index] {
			return nil
		}
		if value < minimum[index] {
			return fmt.Errorf("sing-box %s is unsupported; install sing-box %s or newer", version, MinimumCoreVersion)
		}
	}
	if matches[4] != "" {
		return fmt.Errorf("sing-box %s is unsupported; install sing-box %s or newer", version, MinimumCoreVersion)
	}
	return nil
}

func (s *Service) checkCoreVersion(ctx context.Context, corePath string, env map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := s.processes.Exec(corePath, []string{"version"}, platform.ExecOptions{Context: ctx, Env: env})
	if !result.Flag {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cannot read sing-box version: %s", result.Data))
	}
	matches := coreVersionRE.FindStringSubmatch(result.Data)
	version := ""
	if len(matches) > 1 {
		version = matches[1]
	}
	if err := requireCompatibleCoreVersion(version); err != nil {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return nil
}

type preparedCoreStart struct {
	runtime kernelRuntimeConfig
	path    string
	config  map[string]any
}

// The temporary check never replaces the configuration of a running core.
func (s *Service) prepareCoreStart(ctx context.Context, profile *profilev1.Profile, runtimeCfg kernelRuntimeConfig, path string) (*preparedCoreStart, error) {
	if err := s.checkCoreVersion(ctx, path, runtimeCfg.Env); err != nil {
		return nil, err
	}
	generated, err := s.config.Generate(profile, &kernelv1.GenerateConfigOptions{
		EnableMixinProcessing: true, EnableScriptProcessing: true,
	})
	if err != nil {
		return nil, rpcutil.AsConnectError(err)
	}
	if err := config.EnforceNativeAPIConfig(generated); err != nil {
		return nil, rpcutil.AsConnectError(err)
	}
	config.FinalizeGeneratedConfig(generated)
	if err := s.checkPreparedConfig(ctx, path, runtimeCfg.Env, generated); err != nil {
		return nil, err
	}
	return &preparedCoreStart{runtime: runtimeCfg, path: path, config: generated}, nil
}

func (s *Service) checkPreparedConfig(ctx context.Context, path string, env map[string]string, generated map[string]any) error {
	data, err := json.Marshal(generated)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("marshal core configuration: %w", err))
	}
	file, err := os.CreateTemp("", "webui-native-config-*.json")
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	temporaryPath := file.Name()
	defer os.Remove(temporaryPath)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return connect.NewError(connect.CodeInternal, err)
	}
	if err := file.Close(); err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	workingDirectory := s.processes.ResolvePath(coreWorkingDirectory)
	if err := os.MkdirAll(workingDirectory, 0700); err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result := s.processes.Exec(path, []string{"check", "--disable-color", "-c", temporaryPath, "-D", workingDirectory}, platform.ExecOptions{Context: ctx, WorkingDirectory: workingDirectory, Env: env})
	if !result.Flag {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid core config: %s", result.Data))
	}
	return nil
}
