package kernel

import (
	"context"

	connect "connectrpc.com/connect"
	kernelv1 "guiforcores/gen/kernel/v1"
)

// GetRuntimePaths exposes the exact server-side substitution values used to
// launch the core. The core base path is deliberately relative to the app path.
func (s *Service) GetRuntimePaths(_ context.Context, _ *connect.Request[kernelv1.GetRuntimePathsRequest]) (*connect.Response[kernelv1.GetRuntimePathsResponse], error) {
	values := normalizeKernelRuntime(kernelRuntimeConfig{Args: []string{"$APP_BASE_PATH", "$CORE_BASE_PATH"}}, s.processes.BaseDir())
	return connect.NewResponse(&kernelv1.GetRuntimePathsResponse{
		AppBasePath:  values.Args[0],
		CoreBasePath: values.Args[1],
	}), nil
}
