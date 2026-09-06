package kernel

import (
	"context"
	"log/slog"
	"time"

	"guiforcores/bridge/config"
	"guiforcores/bridge/syncstate"
	kernelv1 "guiforcores/gen/kernel/v1"
	profilev1 "guiforcores/gen/profile/v1"
)

const autoRestartDebounce = 100 * time.Millisecond

// ProfilesChanged is called after profile data has been persisted. Only a
// material change to the profile used by the running core affects its config.
func (s *Service) ProfilesChanged(ids []string) {
	if len(ids) == 0 {
		return
	}

	s.mu.Lock()
	activeProfileID := s.activeProfileID
	status := s.status
	restarting := s.restarting
	s.mu.Unlock()
	if status != kernelv1.CoreStatus_CORE_STATUS_RUNNING && !restarting {
		return
	}

	for _, id := range ids {
		if id == activeProfileID {
			s.requestConfigRestart(activeProfileID, false)
			return
		}
	}
}

// ReferencedResourcesChanged is called after subscription or rule-set data has
// been persisted. It filters changes against the profile actually used by the
// core and does not participate in WebSocket broadcasting.
func (s *Service) ReferencedResourcesChanged(domain syncstate.Domain, ids []string) {
	profile, profileID := s.runningProfileSnapshot()
	if profile == nil || profileID == "" {
		return
	}

	affected := false
	switch domain {
	case syncstate.DomainSubscriptions:
		for _, id := range ids {
			if profileUsesSubscription(profile, id) {
				affected = true
				break
			}
		}
	case syncstate.DomainRuleSets:
		for _, id := range ids {
			if profileUsesRuleSet(profile, id) {
				affected = true
				break
			}
		}
	}
	if affected {
		s.requestConfigRestart(profileID, false)
	}
}

// AppConfigChanged handles persisted app-config changes that affect a running
// core. Selecting a profile is an explicit switch and therefore always
// restarts. Branch changes follow the auto-restart setting.
func (s *Service) AppConfigChanged(previous config.AppConfig, current config.AppConfig) {
	if previous.Profile != current.Profile {
		s.requestConfigRestart(current.Profile, true)
		return
	}
	if previous.Branch != current.Branch {
		s.requestConfigRestart(current.Profile, false)
	}
}

func (s *Service) runningProfileSnapshot() (*profilev1.Profile, string) {
	s.mu.Lock()
	status := s.status
	restarting := s.restarting
	profileID := s.activeProfileID
	profile := cloneProfile(s.currentProfile)
	s.mu.Unlock()

	if status != kernelv1.CoreStatus_CORE_STATUS_RUNNING && !restarting {
		return nil, ""
	}
	if profile == nil && profileID != "" && s.profiles != nil {
		loaded, err := s.profiles.FindByID(profileID)
		if err == nil {
			profile = loaded
		}
	}
	return profile, profileID
}

func profileUsesSubscription(profile *profilev1.Profile, id string) bool {
	for _, outbound := range profile.GetOutbounds() {
		for _, proxy := range outbound.GetOutbounds() {
			if proxy == nil || proxy.GetType() == "Built-in" || proxy.GetType() == "" {
				continue
			}
			if id == "" || (proxy.GetType() == "Subscription" && proxy.GetId() == id) || proxy.GetType() == id {
				return true
			}
		}
	}
	return false
}

func profileUsesRuleSet(profile *profilev1.Profile, id string) bool {
	if profile.GetRoute() == nil {
		return false
	}
	for _, ruleSet := range profile.GetRoute().GetRuleSet() {
		if ruleSet.GetType() != profilev1.RulesetType_RULESET_TYPE_LOCAL {
			continue
		}
		if id == "" || ruleSet.GetPath() == id {
			return true
		}
	}
	return false
}

func (s *Service) requestConfigRestart(profileID string, force bool) {
	shouldRestart := force
	if !shouldRestart && s.appConfig != nil {
		shouldRestart = s.appConfig.Current().AutoRestartKernel
	}
	queued := false

	s.updateCoreState(func() {
		if s.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING && !s.restarting {
			return
		}
		if profileID == "" {
			profileID = s.activeProfileID
		}
		if profileID == "" {
			return
		}
		s.restartRequired = true
		if shouldRestart {
			s.restarting = true
			queued = true
		}
	})
	if queued {
		slog.Info("automatic core restart queued", "component", "kernel", "operation", "auto_restart", "profile_id", profileID, "force", force)
		s.enqueueAutomaticRestart(profileID, force)
	} else if !shouldRestart {
		slog.Info("core restart required", "component", "kernel", "operation", "auto_restart", "profile_id", profileID, "result", "skipped")
	}
}

func (s *Service) enqueueAutomaticRestart(profileID string, force bool) {
	s.restartQueueMu.Lock()
	s.restartPending = true
	s.restartRequestedAt = time.Now()
	if !force && s.restartExecutingID != "" {
		profileID = s.restartExecutingID
	}
	if force || !s.restartTargetForce {
		s.restartTargetID = profileID
		s.restartTargetForce = force
	}
	startWorker := !s.restartWorker
	if startWorker {
		s.restartWorker = true
	}
	s.updateCoreState(func() {
		s.restarting = true
	})
	s.restartQueueMu.Unlock()

	if startWorker {
		go s.runAutomaticRestartQueue()
	}
}

func (s *Service) runAutomaticRestartQueue() {
	for {
		s.restartQueueMu.Lock()
		if !s.restartPending {
			s.restartWorker = false
			s.updateCoreState(func() {
				s.restarting = false
				if s.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
					s.restartRequired = false
				}
			})
			s.restartQueueMu.Unlock()
			return
		}
		wait := time.Until(s.restartRequestedAt.Add(autoRestartDebounce))
		s.restartQueueMu.Unlock()
		if wait > 0 {
			time.Sleep(wait)
			continue
		}

		s.restartQueueMu.Lock()
		if time.Since(s.restartRequestedAt) < autoRestartDebounce {
			s.restartQueueMu.Unlock()
			continue
		}
		profileID := s.restartTargetID
		s.restartPending = false
		s.restartTargetID = ""
		s.restartTargetForce = false
		s.restartExecutingID = profileID
		s.restartQueueMu.Unlock()

		s.restartOperationMu.Lock()
		started := time.Now()
		_, err := s.restartCoreOnce(context.Background(), profileID)
		s.restartOperationMu.Unlock()
		s.restartQueueMu.Lock()
		s.restartExecutingID = ""
		s.restartQueueMu.Unlock()
		if err != nil {
			slog.Error("automatic core restart failed", "component", "kernel", "operation", "auto_restart", "profile_id", profileID, "duration", time.Since(started), "result", "failure", "error", err)
			s.publish("kernelAutoRestartFailed", map[string]any{"reason": sanitizeCoreCrashReason(err.Error())})
			s.updateCoreState(func() {
				if s.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
					s.restartRequired = false
				}
			})
		} else {
			slog.Info("automatic core restart completed", "component", "kernel", "operation", "auto_restart", "profile_id", profileID, "duration", time.Since(started), "result", "success")
		}
	}
}

func (s *Service) finishManualRestart() {
	s.restartQueueMu.Lock()
	if !s.restartWorker {
		s.updateCoreState(func() {
			s.restarting = false
			if s.status != kernelv1.CoreStatus_CORE_STATUS_RUNNING {
				s.restartRequired = false
			}
		})
	}
	s.restartQueueMu.Unlock()
}
