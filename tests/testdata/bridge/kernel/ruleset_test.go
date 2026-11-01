package kernel

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"guiforcores/bridge/platform"
)

type ruleSetProcesses struct {
	fakeProcesses
	execute func([]string, platform.ExecOptions) platform.Result
}

func (p *ruleSetProcesses) Exec(_ string, args []string, options platform.ExecOptions) platform.Result {
	return p.execute(args, options)
}

func TestRuleSetPreviewHonorsCancellationAndOutputLimit(t *testing.T) {
	for _, mode := range []string{"success", "oversize", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			var outputPath string
			processes := &ruleSetProcesses{fakeProcesses: fakeProcesses{resolveBase: t.TempDir()}}
			processes.execute = func(args []string, options platform.ExecOptions) platform.Result {
				if options.Context == nil {
					t.Fatal("decompiler has no context")
				}
				if deadline, ok := options.Context.Deadline(); !ok || time.Until(deadline) > 15*time.Second {
					t.Fatal("decompiler has no bounded deadline")
				}
				outputPath = args[3]
				if mode == "canceled" {
					<-options.Context.Done()
					return platform.Result{Data: "killed"}
				}
				file, err := os.Create(outputPath)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "oversize" {
					err = file.Truncate(maxRuleSetPreviewBytes + 1)
				} else {
					_, err = file.WriteString(`{"version":3,"rules":[]}`)
				}
				if closeErr := file.Close(); err != nil || closeErr != nil {
					t.Fatalf("fixture write: %v %v", err, closeErr)
				}
				return platform.Result{Flag: true}
			}
			service := NewService(processes, &fakeGenerator{}, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
			t.Cleanup(service.lifecycleCancel)
			timeout := 5 * time.Second
			if mode == "canceled" {
				timeout = 50 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			result, err := service.DecompileRuleSetContext(ctx, "fixture.srs")
			switch mode {
			case "success":
				if err != nil || result != `{"version":3,"rules":[]}` {
					t.Fatalf("preview failed: %q %v", result, err)
				}
			case "oversize":
				if err == nil || !strings.Contains(err.Error(), "preview exceeds") || result != "" {
					t.Fatalf("oversize preview accepted: %v", err)
				}
			case "canceled":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("deadline lost: %v", err)
				}
			}
			if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
				t.Fatalf("temporary output was retained: %v", err)
			}
		})
	}
}
