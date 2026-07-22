package syncstate

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	commonv1 "guiforcores/gen/common/v1"

	connect "connectrpc.com/connect"
)

type Domain string

const (
	DomainProfiles       Domain = "profiles"
	DomainSubscriptions  Domain = "subscriptions"
	DomainRuleSets       Domain = "rulesets"
	DomainScheduledTasks Domain = "scheduledTasks"
)

type Operation string

const (
	OperationUpsert  Operation = "upsert"
	OperationDelete  Operation = "delete"
	OperationReorder Operation = "reorder"
	OperationRuntime Operation = "runtime"
)

type ChangeEvent struct {
	Domain        Domain    `json:"domain"`
	Operation     Operation `json:"operation"`
	IDs           []string  `json:"ids"`
	InstanceID    string    `json:"instanceId"`
	StateRevision uint64    `json:"stateRevision"`
}

type domainState struct {
	initialized   bool
	stateRevision uint64
	orderRevision uint64
	itemRevisions map[string]uint64
}

type Coordinator struct {
	mu         sync.Mutex
	instanceID string
	domains    map[Domain]*domainState
}

func NewCoordinator() *Coordinator {
	return &Coordinator{
		instanceID: newInstanceID(),
		domains:    make(map[Domain]*domainState),
	}
}

func newInstanceID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("instance-%p", &value)
}

func (c *Coordinator) domain(domain Domain, ids []string) *domainState {
	state := c.domains[domain]
	if state == nil {
		state = &domainState{itemRevisions: make(map[string]uint64)}
		c.domains[domain] = state
	}
	if !state.initialized {
		state.initialized = true
		state.stateRevision = 1
		state.orderRevision = 1
		for _, id := range ids {
			if id != "" {
				state.itemRevisions[id] = 1
			}
		}
	}
	return state
}

func (c *Coordinator) Snapshot(domain Domain, ids []string) *commonv1.ResourceState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.domain(domain, ids)
	return resourceState(c.instanceID, state)
}

func (c *Coordinator) Mutation(domain Domain, ids []string, id string) *commonv1.MutationState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.domain(domain, ids)
	return mutationState(c.instanceID, state, id)
}

func (c *Coordinator) ExpectedItem(domain Domain, ids []string, id string) *commonv1.ExpectedRevision {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.domain(domain, ids)
	return &commonv1.ExpectedRevision{InstanceId: c.instanceID, Revision: state.itemRevisions[id]}
}

func (c *Coordinator) CheckItem(domain Domain, ids []string, id string, expected *commonv1.ExpectedRevision, required bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.domain(domain, ids)
	return c.checkRevision(expected, state.itemRevisions[id], required, "item")
}

func (c *Coordinator) CheckOrder(domain Domain, ids []string, expected *commonv1.ExpectedRevision, required bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.domain(domain, ids)
	return c.checkRevision(expected, state.orderRevision, required, "order")
}

func (c *Coordinator) checkRevision(expected *commonv1.ExpectedRevision, current uint64, required bool, scope string) error {
	if expected == nil || expected.GetInstanceId() == "" {
		if required {
			return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("%s revision is required", scope))
		}
		return nil
	}
	if expected.GetInstanceId() != c.instanceID {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("server instance changed"))
	}
	if expected.GetRevision() != current {
		return connect.NewError(connect.CodeAborted, fmt.Errorf("%s revision conflict", scope))
	}
	return nil
}

func (c *Coordinator) Advance(
	domain Domain,
	ids []string,
	changedIDs []string,
	removedIDs []string,
	orderChanged bool,
	resultID string,
) *commonv1.MutationState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.domain(domain, ids)
	state.stateRevision++
	for _, id := range changedIDs {
		if id != "" {
			state.itemRevisions[id] = state.stateRevision
		}
	}
	for _, id := range removedIDs {
		delete(state.itemRevisions, id)
	}
	if orderChanged {
		state.orderRevision = state.stateRevision
	}
	return mutationState(c.instanceID, state, resultID)
}

func (c *Coordinator) AdvanceRuntime(domain Domain, ids []string, resultIDs ...string) *commonv1.MutationState {
	resultID := ""
	if len(resultIDs) > 0 {
		resultID = resultIDs[0]
	}
	return c.Advance(domain, ids, nil, nil, false, resultID)
}

func resourceState(instanceID string, state *domainState) *commonv1.ResourceState {
	items := make(map[string]uint64, len(state.itemRevisions))
	for id, revision := range state.itemRevisions {
		items[id] = revision
	}
	return &commonv1.ResourceState{
		InstanceId:    instanceID,
		StateRevision: state.stateRevision,
		OrderRevision: state.orderRevision,
		ItemRevisions: items,
	}
}

func mutationState(instanceID string, state *domainState, id string) *commonv1.MutationState {
	return &commonv1.MutationState{
		InstanceId:    instanceID,
		StateRevision: state.stateRevision,
		ItemRevision:  state.itemRevisions[id],
		OrderRevision: state.orderRevision,
	}
}

func Event(domain Domain, operation Operation, ids []string, state *commonv1.MutationState) ChangeEvent {
	if ids == nil {
		ids = []string{}
	}
	return ChangeEvent{
		Domain:        domain,
		Operation:     operation,
		IDs:           append([]string{}, ids...),
		InstanceID:    state.GetInstanceId(),
		StateRevision: state.GetStateRevision(),
	}
}
