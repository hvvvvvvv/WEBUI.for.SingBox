package auth

import (
	"net"
	"strings"
	"time"
)

func (s *Service) AddSession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readState()
	if err == nil && token != "" {
		s.addSession(token, state.Generation)
	}
}
func (s *Service) addSession(token, generation string) {
	now := s.now()
	for key, item := range s.sessions {
		if !now.Before(item.expires) {
			delete(s.sessions, key)
			delete(s.pending, key)
		}
	}
	if len(s.sessions) >= maxSessions {
		var oldest string
		var expiry time.Time
		for key, item := range s.sessions {
			if oldest == "" || item.expires.Before(expiry) {
				oldest, expiry = key, item.expires
			}
		}
		delete(s.sessions, oldest)
		delete(s.pending, oldest)
	}
	s.sessions[token] = session{generation: generation, expires: now.Add(sessionTTL)}
}
func (s *Service) sessionValid(token string, state credentials, touch bool) bool {
	item, ok := s.sessions[token]
	if !ok || item.generation != state.Generation || !s.now().Before(item.expires) {
		delete(s.sessions, token)
		delete(s.pending, token)
		return false
	}
	if touch {
		item.expires = s.now().Add(sessionTTL)
		s.sessions[token] = item
	}
	return true
}
func (s *Service) validateSession(token string, touch bool) bool {
	if token == "" || len(token) > 256 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, exists := s.sessions[token]
	if !exists {
		return false
	}
	if !s.now().Before(item.expires) {
		delete(s.sessions, token)
		delete(s.pending, token)
		return false
	}
	state, err := s.readStateWithCache(true)
	return err == nil && s.sessionValid(token, state, touch)
}
func (s *Service) ValidateSessionWithoutTouch(token string) bool {
	return s.validateSession(token, false)
}
func (s *Service) ValidateSession(token string) bool { return s.validateSession(token, true) }
func (s *Service) RemoveSession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
	delete(s.pending, token)
}
func (s *Service) ClearSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.sessions)
	clear(s.pending)
}
func (s *Service) rateLimited(remote string) bool {
	now := s.now()
	item := s.failures[loginFailureKey(remote)]
	return now.Before(item.blocked) || now.Before(s.accountFailure.blocked)
}
func nextFailure(item loginFailure, now time.Time, limit int) loginFailure {
	if now.Before(item.blocked) {
		return item
	}
	if !item.blocked.IsZero() || now.Sub(item.updated) >= 10*time.Minute {
		item = loginFailure{}
	}
	item.count++
	item.updated = now
	if item.count >= limit {
		item.count = 0
		item.blocked = now.Add(loginLockout)
	}
	return item
}
func (s *Service) recordFailure(remote string) {
	now := s.now()
	key := loginFailureKey(remote)
	for k, item := range s.failures {
		if now.Sub(item.updated) >= 10*time.Minute {
			delete(s.failures, k)
		}
	}
	if _, exists := s.failures[key]; !exists && len(s.failures) >= maxFailureKeys {
		var oldest string
		var updated time.Time
		for k, item := range s.failures {
			if oldest == "" || item.updated.Before(updated) {
				oldest, updated = k, item.updated
			}
		}
		delete(s.failures, oldest)
	}
	s.failures[key] = nextFailure(s.failures[key], now, maxLoginFailures)
	s.accountFailure = nextFailure(s.accountFailure, now, 20)
}
func (s *Service) IsLoginRateLimited(remote string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rateLimited(remote)
}
func (s *Service) RecordLoginFailure(remote string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordFailure(remote)
}
func (s *Service) ClearLoginFailures(remote string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, loginFailureKey(remote))
}
func loginFailureKey(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err == nil {
		return host
	}
	if len(remote) > 256 {
		return "unknown"
	}
	return strings.TrimSpace(remote)
}
