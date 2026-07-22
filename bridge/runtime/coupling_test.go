package runtime

import (
	"testing"

	"guiforcores/bridge/config"
	"guiforcores/bridge/syncstate"
)

type staticAppConfig struct {
	value config.AppConfig
}

func (s staticAppConfig) Current() config.AppConfig {
	return s.value
}

type referencedResourceChange struct {
	domain syncstate.Domain
	ids    []string
}

type recordingKernel struct {
	changes chan referencedResourceChange
}

func (k recordingKernel) ReferencedResourcesChanged(domain syncstate.Domain, ids []string) {
	k.changes <- referencedResourceChange{domain: domain, ids: ids}
}

func TestReferencedResourceChangeUsesKernelControllerInterface(t *testing.T) {
	kernelController := recordingKernel{changes: make(chan referencedResourceChange, 1)}
	service := &appRuntimeService{
		kernel: kernelController,
	}
	service.notifyReferencedResourcesChanged(syncstate.DomainRuleSets, []string{"changed"})

	change := <-kernelController.changes
	if change.domain != syncstate.DomainRuleSets || len(change.ids) != 1 || change.ids[0] != "changed" {
		t.Fatalf("unexpected referenced resource change: %#v", change)
	}
}
