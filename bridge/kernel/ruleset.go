package kernel

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"guiforcores/bridge/platform"
)

const maxRuleSetPreviewBytes = 32 << 20

func (s *Service) DecompileRuleSet(sourcePath string) (string, error) {
	return s.DecompileRuleSetContext(context.Background(), sourcePath)
}

func (s *Service) DecompileRuleSetContext(parent context.Context, sourcePath string) (string, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return "", fmt.Errorf("decompile rule-set: source path is empty")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.lifecycleCtx, cancel)
	defer stop()
	if err := ctx.Err(); err != nil {
		return "", err
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
			Context:          ctx,
			WorkingDirectory: s.processes.ResolvePath(coreWorkingDirectory),
			Env:              runtimeCfg.Env,
		},
	)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !result.Flag {
		message := strings.TrimSpace(result.Data)
		if message == "" {
			message = "unknown error"
		}
		return "", fmt.Errorf("decompile rule-set: %s", message)
	}

	file, err := os.Open(outputPath)
	if err != nil {
		return "", fmt.Errorf("decompile rule-set: read output: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("decompile rule-set: stat output: %w", err)
	}
	if info.Size() > maxRuleSetPreviewBytes {
		return "", fmt.Errorf("decompile rule-set: preview exceeds %d bytes", maxRuleSetPreviewBytes)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxRuleSetPreviewBytes+1))
	if err != nil {
		return "", fmt.Errorf("decompile rule-set: read output: %w", err)
	}
	if len(content) > maxRuleSetPreviewBytes {
		return "", fmt.Errorf("decompile rule-set: preview exceeds %d bytes", maxRuleSetPreviewBytes)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(content), nil
}
