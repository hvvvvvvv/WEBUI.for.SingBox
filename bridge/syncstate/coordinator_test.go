package syncstate

import (
	"sync"
	"testing"

	commonv1 "guiforcores/gen/common/v1"

	connect "connectrpc.com/connect"
)

func TestCoordinatorTracksIndependentItemOrderAndRuntimeRevisions(t *testing.T) {
	coordinator := NewCoordinator()
	initial := coordinator.Snapshot(DomainProfiles, []string{"first", "second"})
	if initial.GetInstanceId() == "" || initial.GetStateRevision() != 1 || initial.GetOrderRevision() != 1 {
		t.Fatalf("initial state = %#v", initial)
	}
	if initial.GetItemRevisions()["first"] != 1 || initial.GetItemRevisions()["second"] != 1 {
		t.Fatalf("initial item revisions = %#v", initial.GetItemRevisions())
	}

	itemChange := coordinator.Advance(DomainProfiles, []string{"first", "second"}, []string{"first"}, nil, false, "first")
	if itemChange.GetStateRevision() != 2 || itemChange.GetItemRevision() != 2 || itemChange.GetOrderRevision() != 1 {
		t.Fatalf("item mutation = %#v", itemChange)
	}
	if err := coordinator.CheckItem(DomainProfiles, []string{"first", "second"}, "second", &commonv1.ExpectedRevision{
		InstanceId: initial.GetInstanceId(), Revision: 1,
	}, true); err != nil {
		t.Fatalf("unrelated item revision changed: %v", err)
	}

	runtimeMutation := coordinator.AdvanceRuntime(DomainProfiles, []string{"first", "second"})
	if runtimeMutation.GetStateRevision() != 3 || runtimeMutation.GetOrderRevision() != 1 {
		t.Fatalf("runtime mutation = %#v", runtimeMutation)
	}
	if got := coordinator.Snapshot(DomainProfiles, []string{"first", "second"}).GetItemRevisions()["first"]; got != 2 {
		t.Fatalf("runtime mutation changed item revision to %d", got)
	}

	orderChange := coordinator.Advance(DomainProfiles, []string{"second", "first"}, nil, nil, true, "")
	if orderChange.GetStateRevision() != 4 || orderChange.GetOrderRevision() != 4 {
		t.Fatalf("order mutation = %#v", orderChange)
	}
}

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
