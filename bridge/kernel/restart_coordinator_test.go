package kernel

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/platform"
	"guiforcores/bridge/syncstate"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
)

func referencedProfile() *profilev1.Profile {
	return &profilev1.Profile{
		Id: "active",
		Outbounds: []*profilev1.Outbound{{
			Outbounds: []*profilev1.ProxyRef{
				{Type: "Subscription", Id: "subscription-a"},
				{Type: "subscription-b", Id: "node-a"},
				{Type: "Built-in", Id: "direct"},
			},
		}},
		Route: &profilev1.Route{RuleSet: []*profilev1.RuleSet{
			{Type: profilev1.RulesetType_RULESET_TYPE_LOCAL, Path: "ruleset-a"},
			{Type: profilev1.RulesetType_RULESET_TYPE_REMOTE, Path: "remote-ruleset"},
		}},
	}
}

func newRunningCoordinator(autoRestart bool) (*Service, *recordingEvents) {
	profile := referencedProfile()
	events := &recordingEvents{}
	service := NewService(
		fakeProcesses{},
		&fakeGenerator{},
		fakeConfig{value: config.AppConfig{AutoRestartKernel: autoRestart}},
		&fakeProfiles{profile: profile},
		events,
	)
	service.mu.Lock()
	service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	service.corePID = 9
	service.activeProfileID = profile.GetId()
	service.currentProfile = cloneProfile(profile)
	service.mu.Unlock()
	return service, events
}

func coreRestartState(t *testing.T, service *Service) (bool, bool) {
	t.Helper()
	response, err := service.GetCoreStatus(context.Background(), connect.NewRequest(&kernelv1.GetCoreStatusRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	return response.Msg.GetRestartRequired(), response.Msg.GetRestarting()
}

func TestChangeImpactSetsBackendRestartRequired(t *testing.T) {
	tests := []struct {
		name   string
		notify func(*Service)
		want   bool
	}{
		{name: "active profile", notify: func(service *Service) { service.ProfilesChanged([]string{"active"}) }, want: true},
		{name: "unrelated profile", notify: func(service *Service) { service.ProfilesChanged([]string{"other"}) }, want: false},
		{name: "whole subscription", notify: func(service *Service) {
			service.ReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{"subscription-a"})
		}, want: true},
		{name: "subscription node", notify: func(service *Service) {
			service.ReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{"subscription-b"})
		}, want: true},
		{name: "unrelated subscription", notify: func(service *Service) {
			service.ReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{"other"})
		}, want: false},
		{name: "local ruleset", notify: func(service *Service) {
			service.ReferencedResourcesChanged(syncstate.DomainRuleSets, []string{"ruleset-a"})
		}, want: true},
		{name: "remote ruleset", notify: func(service *Service) {
			service.ReferencedResourcesChanged(syncstate.DomainRuleSets, []string{"remote-ruleset"})
		}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _ := newRunningCoordinator(false)
			test.notify(service)
			required, restarting := coreRestartState(t, service)
			if required != test.want || restarting {
				t.Fatalf("restart state = required:%v restarting:%v, want required:%v", required, restarting, test.want)
			}
		})
	}
}

func TestBranchChangeFollowsAutoRestartAndEnablingDoesNotConsumePendingState(t *testing.T) {
	service, _ := newRunningCoordinator(false)
	before := config.AppConfig{Branch: "main", Profile: "active", AutoRestartKernel: false}
	after := config.AppConfig{Branch: "alpha", Profile: "active", AutoRestartKernel: false}
	service.AppConfigChanged(before, after)

	required, restarting := coreRestartState(t, service)
	if !required || restarting {
		t.Fatalf("branch change state = required:%v restarting:%v", required, restarting)
	}

	enabled := after
	enabled.AutoRestartKernel = true
	service.AppConfigChanged(after, enabled)
	required, restarting = coreRestartState(t, service)
	if !required || restarting {
		t.Fatalf("enabling auto restart consumed pending state: required:%v restarting:%v", required, restarting)
	}
}

type countingProcesses struct {
	fakeProcesses
	mu     sync.Mutex
	starts int
	kills  int
}

type blockingKillProcesses struct {
	countingProcesses
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (p *blockingKillProcesses) KillProcess(pid int, timeout int) platform.Result {
	p.once.Do(func() {
		close(p.entered)
		<-p.release
	})
	return p.countingProcesses.KillProcess(pid, timeout)
}

type generatedProfileReader struct {
	base *profilev1.Profile
}

func (r generatedProfileReader) FindByID(id string) (*profilev1.Profile, error) {
	profile := cloneProfile(r.base)
	profile.Id = id
	return profile, nil
}

func (p *countingProcesses) ExecBackground(string, []string, string, platform.ExecOptions) platform.Result {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.starts++
	return platform.Result{Flag: true, Data: strconv.Itoa(100 + p.starts)}
}

func (p *countingProcesses) KillProcess(int, int) platform.Result {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kills++
	return platform.Result{Flag: true}
}

func (p *countingProcesses) counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.starts, p.kills
}

func newCoordinatorWithAppConfig(processes ProcessRunner, profiles ProfileReader, events EventPublisher, appConfig config.AppConfig) *Service {
	profile := referencedProfile()
	service := NewService(
		processes,
		&fakeGenerator{},
		fakeConfig{value: appConfig},
		profiles,
		events,
	)
	service.mu.Lock()
	service.status = kernelv1.CoreStatus_CORE_STATUS_RUNNING
	service.corePID = 9
	service.activeProfileID = profile.GetId()
	service.currentProfile = cloneProfile(profile)
	service.mu.Unlock()
	return service
}

func newAutoRestartCoordinator(processes ProcessRunner, profiles ProfileReader, events EventPublisher) *Service {
	return newCoordinatorWithAppConfig(processes, profiles, events, config.AppConfig{
		AutoRestartKernel: true,
		Branch:            "main",
		Profile:           "active",
		Main:              config.CoreRuntimeConfig{Args: []string{"run"}},
	})
}

func waitForRestartQueue(t *testing.T, service *Service) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, restarting := coreRestartState(t, service)
		if !restarting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("restart queue did not become idle")
}

func TestAutomaticRestartCoalescesBurstAndAddsOneTrailingRestart(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	var readyCalls atomic.Int32
	firstReady := make(chan struct{})
	releaseFirst := make(chan struct{})
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error {
		if readyCalls.Add(1) == 1 {
			close(firstReady)
			<-releaseFirst
		}
		return nil
	}
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })

	processes := &countingProcesses{}
	profile := referencedProfile()
	service := newAutoRestartCoordinator(processes, &fakeProfiles{profile: profile}, &recordingEvents{})
	service.ReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{"subscription-a"})
	service.ReferencedResourcesChanged(syncstate.DomainRuleSets, []string{"ruleset-a"})

	select {
	case <-firstReady:
	case <-time.After(2 * time.Second):
		t.Fatal("first automatic restart did not start")
	}

	service.ReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{"subscription-a"})
	service.ReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{"subscription-a"})
	close(releaseFirst)
	waitForRestartQueue(t, service)

	starts, kills := processes.counts()
	if starts != 2 || kills != 2 {
		t.Fatalf("restart calls = starts:%d kills:%d, want two coalesced restarts", starts, kills)
	}
	required, restarting := coreRestartState(t, service)
	if required || restarting {
		t.Fatalf("successful restart left stale state: required:%v restarting:%v", required, restarting)
	}
}

func TestProfileAndBranchAppConfigRestartPolicies(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error { return nil }
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })

	t.Run("profile switch always restarts", func(t *testing.T) {
		processes := &countingProcesses{}
		profile := referencedProfile()
		service := newCoordinatorWithAppConfig(
			processes,
			generatedProfileReader{base: profile},
			&recordingEvents{},
			config.AppConfig{
				AutoRestartKernel: false,
				Branch:            "main",
				Profile:           "selected",
				Main:              config.CoreRuntimeConfig{Args: []string{"run"}},
			},
		)
		service.AppConfigChanged(
			config.AppConfig{Profile: "active", Branch: "main"},
			config.AppConfig{Profile: "selected", Branch: "main"},
		)
		waitForRestartQueue(t, service)
		_, profileID := service.Status()
		starts, kills := processes.counts()
		if profileID != "selected" || starts != 1 || kills != 1 {
			t.Fatalf("profile switch = profile:%q starts:%d kills:%d", profileID, starts, kills)
		}
	})

	t.Run("branch switch follows enabled auto restart", func(t *testing.T) {
		processes := &countingProcesses{}
		profile := referencedProfile()
		service := newCoordinatorWithAppConfig(
			processes,
			&fakeProfiles{profile: profile},
			&recordingEvents{},
			config.AppConfig{
				AutoRestartKernel: true,
				Branch:            "alpha",
				Profile:           "active",
				Alpha:             config.CoreRuntimeConfig{Args: []string{"run"}},
			},
		)
		service.AppConfigChanged(
			config.AppConfig{Profile: "active", Branch: "main"},
			config.AppConfig{Profile: "active", Branch: "alpha"},
		)
		waitForRestartQueue(t, service)
		starts, kills := processes.counts()
		if starts != 1 || kills != 1 {
			t.Fatalf("branch switch restart calls = starts:%d kills:%d", starts, kills)
		}
	})
}

func TestTrailingChangeKeepsForcedProfileSwitchTarget(t *testing.T) {
	previousWait := waitKernelAPIReadyFunc
	waitKernelAPIReadyFunc = func(context.Context, string, string, int, time.Duration) error { return nil }
	t.Cleanup(func() { waitKernelAPIReadyFunc = previousWait })

	processes := &blockingKillProcesses{entered: make(chan struct{}), release: make(chan struct{})}
	profile := referencedProfile()
	service := newAutoRestartCoordinator(processes, generatedProfileReader{base: profile}, &recordingEvents{})
	service.AppConfigChanged(
		config.AppConfig{Profile: "active", Branch: "main"},
		config.AppConfig{Profile: "selected", Branch: "main"},
	)

	select {
	case <-processes.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("forced profile restart did not begin stopping")
	}
	service.ReferencedResourcesChanged(syncstate.DomainSubscriptions, []string{"subscription-a"})
	close(processes.release)
	waitForRestartQueue(t, service)

	status, profileID := service.Status()
	if status != kernelv1.CoreStatus_CORE_STATUS_RUNNING || profileID != "selected" {
		t.Fatalf("trailing restart selected status %v profile %q", status, profileID)
	}
	starts, kills := processes.counts()
	if starts != 2 || kills != 2 {
		t.Fatalf("restart calls = starts:%d kills:%d, want forced plus one trailing restart", starts, kills)
	}
}

func TestAutomaticRestartFailureKeepsRunningCoreAndPublishesFailure(t *testing.T) {
	processes := &countingProcesses{}
	events := &recordingEvents{}
	service := newAutoRestartCoordinator(
		processes,
		&fakeProfiles{err: errors.New("profile missing")},
		events,
	)
	before := config.AppConfig{Profile: "active", Branch: "main"}
	after := config.AppConfig{Profile: "missing", Branch: "main"}
	service.AppConfigChanged(before, after)
	waitForRestartQueue(t, service)

	response, err := service.GetCoreStatus(context.Background(), connect.NewRequest(&kernelv1.GetCoreStatusRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetStatus() != kernelv1.CoreStatus_CORE_STATUS_RUNNING || !response.Msg.GetRestartRequired() {
		t.Fatalf("failure state = %#v", response.Msg)
	}
	starts, kills := processes.counts()
	if starts != 0 || kills != 0 {
		t.Fatalf("missing profile changed the running core: starts:%d kills:%d", starts, kills)
	}
	if len(events.named("kernelAutoRestartFailed")) != 1 {
		t.Fatalf("automatic restart failure events = %d", len(events.named("kernelAutoRestartFailed")))
	}
}
