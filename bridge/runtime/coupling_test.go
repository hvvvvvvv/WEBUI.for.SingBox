package runtime

import (
	"context"
	"testing"
	"time"

	"guiforcores/bridge/config"
	kernelv1 "guiforcores/gen/kernel/v1"
)

type staticAppConfig struct {
	value config.AppConfig
}

func (s staticAppConfig) Current() config.AppConfig {
	return s.value
}

type recordingKernel struct {
	restarted chan string
}

func (k recordingKernel) Status() (kernelv1.CoreStatus, string) {
	return kernelv1.CoreStatus_CORE_STATUS_RUNNING, "active-profile"
}

func (k recordingKernel) Restart(_ context.Context, profileID string) error {
	k.restarted <- profileID
	return nil
}

func TestRuntimeChangeUsesKernelControllerInterface(t *testing.T) {
	kernelController := recordingKernel{restarted: make(chan string, 1)}
	service := &appRuntimeService{
		config: staticAppConfig{value: config.AppConfig{AutoRestartKernel: true}},
		kernel: kernelController,
	}
	service.markRuntimeChanged("ruleset", "changed")

	select {
	case profileID := <-kernelController.restarted:
		if profileID != "active-profile" {
			t.Fatalf("unexpected profile id %q", profileID)
		}
	case <-time.After(time.Second):
		t.Fatal("expected kernel restart")
	}
}
