package syncstate

import (
	"sync"
	"testing"

	commonv1 "guiforcores/gen/common/v1"

	connect "connectrpc.com/connect"
)

func TestCoordinatorRejectsStaleAndPreviousInstanceRevisions(t *testing.T) {
	coordinator := NewCoordinator()
	snapshot := coordinator.Snapshot(DomainRuleSets, []string{"ruleset"})
	coordinator.Advance(DomainRuleSets, []string{"ruleset"}, []string{"ruleset"}, nil, false, "ruleset")

	stale := &commonv1.ExpectedRevision{InstanceId: snapshot.GetInstanceId(), Revision: snapshot.GetItemRevisions()["ruleset"]}
	if code := connect.CodeOf(coordinator.CheckItem(DomainRuleSets, []string{"ruleset"}, "ruleset", stale, true)); code != connect.CodeAborted {
		t.Fatalf("stale revision code = %v", code)
	}

	restarted := NewCoordinator()
	if code := connect.CodeOf(restarted.CheckItem(DomainRuleSets, []string{"ruleset"}, "ruleset", stale, true)); code != connect.CodeFailedPrecondition {
		t.Fatalf("previous instance code = %v", code)
	}
	if code := connect.CodeOf(restarted.CheckItem(DomainRuleSets, []string{"ruleset"}, "ruleset", nil, true)); code != connect.CodeFailedPrecondition {
		t.Fatalf("missing revision code = %v", code)
	}
}

func TestCoordinatorConcurrentAdvancesAreSerialized(t *testing.T) {
	coordinator := NewCoordinator()
	coordinator.Snapshot(DomainScheduledTasks, []string{"task"})

	const mutations = 100
	var wait sync.WaitGroup
	wait.Add(mutations)
	for range mutations {
		go func() {
			defer wait.Done()
			coordinator.AdvanceRuntime(DomainScheduledTasks, []string{"task"})
		}()
	}
	wait.Wait()

	state := coordinator.Snapshot(DomainScheduledTasks, []string{"task"})
	if state.GetStateRevision() != mutations+1 {
		t.Fatalf("state revision = %d, want %d", state.GetStateRevision(), mutations+1)
	}
}
