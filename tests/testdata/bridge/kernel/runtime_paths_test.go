package kernel

import (
	"context"
	"strings"
	"testing"

	connect "connectrpc.com/connect"
	kernelv1 "guiforcores/gen/kernel/v1"
)

type runtimePathsProcesses struct {
	fakeProcesses
	base string
}

func (p runtimePathsProcesses) BaseDir() string { return p.base }

func TestRuntimePathsUseTheSameSubstitutionsAsCoreLaunch(t *testing.T) {
	for _, base := range []string{"/opt/webui", "/opt/router app/", "/", `D:\Router App\`, "/srv/$CORE_BASE_PATH"} {
		t.Run(base, func(t *testing.T) {
			s := &Service{processes: runtimePathsProcesses{base: base}}
			response, err := s.GetRuntimePaths(context.Background(), connect.NewRequest(&kernelv1.GetRuntimePathsRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			paths := response.Msg
			if paths.AppBasePath != strings.TrimSuffix(base, "/") || paths.CoreBasePath != coreWorkingDirectory {
				t.Fatalf("unexpected server substitutions: %+v", paths)
			}
			input := "$APP_BASE_PATH/$CORE_BASE_PATH/config.json"
			preview := strings.NewReplacer("$APP_BASE_PATH", paths.AppBasePath, "$CORE_BASE_PATH", paths.CoreBasePath).Replace(input)
			runtime := normalizeKernelRuntime(kernelRuntimeConfig{Args: []string{input}, Env: map[string]string{"CONFIG": input}}, base)
			if preview != runtime.Args[0] || preview != runtime.Env["CONFIG"] {
				t.Fatalf("preview differs from launch: %q %+v", preview, runtime)
			}
		})
	}
}
