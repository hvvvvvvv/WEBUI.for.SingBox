package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"guiforcores/bridge/platform"
)

func (s *Service) DecompileRuleSet(sourcePath string) (string, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return "", fmt.Errorf("decompile rule-set: source path is empty")
	}

	runtimeCfg, err := s.loadRuntimeConfig()
	if err != nil {
		return "", fmt.Errorf("decompile rule-set: load core configuration: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "webui-ruleset-preview-*")
	if err != nil {
		return "", fmt.Errorf("decompile rule-set: create temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	outputPath := filepath.Join(tempDir, "ruleset.json")
	binary := coreWorkingDirectory + "/" + getKernelFileName(runtimeCfg.Branch == "alpha")
	result := s.processes.Exec(
		binary,
		[]string{"rule-set", "decompile", "--output", outputPath, sourcePath},
		platform.ExecOptions{
			WorkingDirectory: s.processes.ResolvePath(coreWorkingDirectory),
			Env:              runtimeCfg.Env,
		},
	)
	if !result.Flag {
		message := strings.TrimSpace(result.Data)
		if message == "" {
			message = "unknown error"
		}
		return "", fmt.Errorf("decompile rule-set: %s", message)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("decompile rule-set: read output: %w", err)
	}
	return string(content), nil
}
