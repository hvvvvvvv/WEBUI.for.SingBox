package kernel

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
)

type decompileProcesses struct {
	fakeProcesses
	result        platform.Result
	outputContent string
	writeOutput   bool
	binary        string
	args          []string
	options       platform.ExecOptions
	outputPath    string
}

func (p *decompileProcesses) Exec(
	path string,
	args []string,
	options platform.ExecOptions,
) platform.Result {
	p.binary = path
	p.args = append([]string{}, args...)
	p.options = options
	for index, arg := range args {
		if arg == "--output" && index+1 < len(args) {
			p.outputPath = args[index+1]
			break
		}
	}
	if p.result.Flag && p.writeOutput {
		if err := os.WriteFile(p.outputPath, []byte(p.outputContent), 0600); err != nil {
			return platform.Result{Flag: false, Data: err.Error()}
		}
	}
	return p.result
}

func TestDecompileRuleSetUsesSelectedCoreBranch(t *testing.T) {
	tests := []struct {
		name           string
		branch         string
		expectedBinary string
		expectedEnv    string
	}{
		{name: "main", branch: "main", expectedBinary: getKernelFileName(false), expectedEnv: "main"},
		{name: "alpha", branch: "alpha", expectedBinary: getKernelFileName(true), expectedEnv: "alpha"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseDir := t.TempDir()
			processes := &decompileProcesses{
				fakeProcesses: fakeProcesses{resolveBase: baseDir},
				result:        platform.Result{Flag: true},
				outputContent: `{"version":2,"rules":[]}`,
				writeOutput:   true,
			}
			appConfig := config.AppConfig{
				Branch: test.branch,
				Main: config.CoreRuntimeConfig{
					Env: map[string]string{"RULESET_TEST_BRANCH": "main"},
				},
				Alpha: config.CoreRuntimeConfig{
					Env: map[string]string{"RULESET_TEST_BRANCH": "alpha"},
				},
			}
			service := NewService(
				processes,
				&fakeGenerator{},
				fakeConfig{value: appConfig},
				&fakeProfiles{},
				fakeEvents{},
			)
			sourcePath := filepath.Join(baseDir, "data", "rulesets", "ruleset with spaces.srs")

			content, err := service.DecompileRuleSet(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			if content != processes.outputContent {
				t.Fatalf("content = %q, want %q", content, processes.outputContent)
			}
			expectedBinary := filepath.ToSlash(filepath.Join(coreWorkingDirectory, test.expectedBinary))
			if filepath.ToSlash(processes.binary) != expectedBinary {
				t.Fatalf("binary = %q, want %q", processes.binary, expectedBinary)
			}
			expectedArgs := []string{
				"rule-set",
				"decompile",
				"--output",
				processes.outputPath,
				sourcePath,
			}
			if !reflect.DeepEqual(processes.args, expectedArgs) {
				t.Fatalf("args = %#v, want %#v", processes.args, expectedArgs)
			}
			if processes.options.WorkingDirectory != filepath.Join(baseDir, coreWorkingDirectory) {
				t.Fatalf(
					"working directory = %q, want %q",
					processes.options.WorkingDirectory,
					filepath.Join(baseDir, coreWorkingDirectory),
				)
			}
			if processes.options.Env["RULESET_TEST_BRANCH"] != test.expectedEnv {
				t.Fatalf("environment = %#v, want selected branch %q", processes.options.Env, test.expectedEnv)
			}
			if _, err := os.Stat(filepath.Dir(processes.outputPath)); !os.IsNotExist(err) {
				t.Fatalf("temporary directory was not removed: %v", err)
			}
		})
	}
}

func TestDecompileRuleSetReturnsCoreError(t *testing.T) {
	processes := &decompileProcesses{
		result: platform.Result{Flag: false, Data: "invalid SRS payload"},
	}
	service := NewService(
		processes,
		&fakeGenerator{},
		fakeConfig{value: config.AppConfig{Branch: "main"}},
		&fakeProfiles{},
		fakeEvents{},
	)

	_, err := service.DecompileRuleSet("/tmp/ruleset.srs")
	if err == nil || !strings.Contains(err.Error(), "invalid SRS payload") {
		t.Fatalf("error = %v, want core output", err)
	}
}

func TestDecompileRuleSetRequiresCoreOutput(t *testing.T) {
	processes := &decompileProcesses{
		result: platform.Result{Flag: true},
	}
	service := NewService(
		processes,
		&fakeGenerator{},
		fakeConfig{value: config.AppConfig{Branch: "main"}},
		&fakeProfiles{},
		fakeEvents{},
	)

	_, err := service.DecompileRuleSet("/tmp/ruleset.srs")
	if err == nil || !strings.Contains(err.Error(), "read output") {
		t.Fatalf("error = %v, want missing output error", err)
	}
}

func TestDecompileRuleSetRejectsEmptySourcePath(t *testing.T) {
	service := NewService(
		fakeProcesses{},
		&fakeGenerator{},
		fakeConfig{},
		&fakeProfiles{},
		fakeEvents{},
	)

	_, err := service.DecompileRuleSet(" ")
	if err == nil || !strings.Contains(err.Error(), "source path is empty") {
		t.Fatalf("error = %v, want empty source path error", err)
	}
}
