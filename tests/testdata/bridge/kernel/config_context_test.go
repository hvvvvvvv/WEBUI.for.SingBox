package kernel

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

type contextConfigGenerator struct {
	fakeGenerator
	called bool
}

func (g *contextConfigGenerator) GenerateContext(ctx context.Context, _ *profilev1.Profile, options *kernelv1.GenerateConfigOptions) (map[string]any, error) {
	g.called = true
	if !options.EnableMixinProcessing || !options.EnableScriptProcessing {
		panic("runtime processors disabled")
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestCorePreparationPassesCancellationToConfigScripts(t *testing.T) {
	generator := &contextConfigGenerator{}
	service := NewService(fakeProcesses{}, generator, fakeConfig{}, &fakeProfiles{}, fakeEvents{})
	t.Cleanup(service.lifecycleCancel)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := service.prepareCoreStart(ctx, &profilev1.Profile{}, kernelRuntimeConfig{}, "fixture-core")
	if !generator.called || connect.CodeOf(err) != connect.CodeDeadlineExceeded {
		t.Fatalf("configuration lost startup context: called=%v error=%v", generator.called, err)
	}
}
