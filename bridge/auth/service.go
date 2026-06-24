package auth

import (
	"container/list"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"guiforcores/bridge/storage"

	"github.com/jellydator/ttlcache/v3"
)

const (
	secretPath       = "data/secret.key"
	maxLoginFailures = 5
	loginLockout     = 30 * time.Second
	sessionTTL       = 8 * time.Hour
)

type loginFailure struct {
	count   int
	blocked time.Time
}

type Service struct {
	paths *storage.Paths

	secretMu sync.RWMutex
	failures struct {
		sync.Mutex
		items map[string]loginFailure
	}
	sessions *ttlcache.Cache[string, struct{}]
}

func NewService(paths *storage.Paths) *Service {
	service := &Service{
		paths: paths,
		sessions: ttlcache.New(
			ttlcache.WithTTL[string, struct{}](sessionTTL),
		),
	}
	service.failures.items = make(map[string]loginFailure)
	return service
}

func HashSecret(plain string) string {
	hash := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(hash[:])
}

func (s *Service) VerifySecret(plain string) bool {
	hash := s.SecretHash()
	return hash == "" || HashSecret(plain) == hash
}

func (s *Service) SecretHash() string {
	s.secretMu.RLock()
	defer s.secretMu.RUnlock()

	data, err := os.ReadFile(s.paths.Resolve(secretPath))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (s *Service) SetSecret(secret string) error {
	s.secretMu.Lock()
	defer s.secretMu.Unlock()

	var content []byte
	if secret != "" {
		content = []byte(HashSecret(secret))
	}
	path := s.paths.Resolve(secretPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

func (s *Service) GenerateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (s *Service) AddSession(token string) {
	s.sessions.Set(token, struct{}{}, ttlcache.DefaultTTL)
	s.sessions.DeleteExpired()
}

func (s *Service) ValidateSessionWithoutTouch(token string) bool {
	return s.sessions.Has(token)
}

func (s *Service) ValidateSession(token string) bool {
	if !s.sessions.Has(token) {
		return false
	}
	s.sessions.Touch(token)
	return true
}

func (s *Service) RemoveSession(token string) {
	s.sessions.Delete(token)
}

func (s *Service) ClearSessions() {
	s.sessions.DeleteAll()
}

func (s *Service) ClearSessionsExcept(token string) {
	removeItems := list.New()
	s.sessions.Range(func(item *ttlcache.Item[string, struct{}]) bool {
		if item.Key() != token {
			removeItems.PushBack(item.Key())
		}
		return true
	})
	for item := removeItems.Front(); item != nil; item = item.Next() {
		if key, ok := item.Value.(string); ok {
			s.sessions.Delete(key)
		}
	}
}

func (s *Service) IsLoginRateLimited(remoteAddr string) bool {
	key := loginFailureKey(remoteAddr)
	if key == "" {
		return false
	}

	s.failures.Lock()
	defer s.failures.Unlock()

	item, ok := s.failures.items[key]
	if !ok {
		return false
	}
	if time.Now().Before(item.blocked) {
		return true
	}
	if !item.blocked.IsZero() {
		delete(s.failures.items, key)
	}
	return false
}

func (s *Service) RecordLoginFailure(remoteAddr string) {
	key := loginFailureKey(remoteAddr)
	if key == "" {
		return
	}

	s.failures.Lock()
	defer s.failures.Unlock()

	item := s.failures.items[key]
	if time.Now().Before(item.blocked) {
		return
	}
	item.count++
	if item.count >= maxLoginFailures {
		item.count = 0
		item.blocked = time.Now().Add(loginLockout)
	}
	s.failures.items[key] = item
}

func (s *Service) ClearLoginFailures(remoteAddr string) {
	key := loginFailureKey(remoteAddr)
	if key == "" {
		return
	}
	s.failures.Lock()
	defer s.failures.Unlock()
	delete(s.failures.items, key)
}

func loginFailureKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(remoteAddr)
}
