package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	hash, err := s.SecretHash()
	return err == nil && (hash == "" || HashSecret(plain) == hash)
}

func (s *Service) SecretHash() (string, error) {
	s.secretMu.RLock()
	defer s.secretMu.RUnlock()

	path := s.paths.Resolve(secretPath)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// A missing key is the original unconfigured state. A broken key
			// symlink is a read failure, not a request to disable authentication.
			if _, linkErr := os.Lstat(path); os.IsNotExist(linkErr) {
				parent, parentErr := os.Stat(filepath.Dir(path))
				if os.IsNotExist(parentErr) || (parentErr == nil && parent.IsDir()) {
					return "", nil
				}
			}
		}
		return "", fmt.Errorf("inspect authentication secret: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("authentication secret must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read authentication secret: %w", err)
	}
	// SetSecret("") deliberately writes an empty file to disable login.
	if len(data) == 0 {
		return "", nil
	}
	hash := strings.TrimSpace(string(data))
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("authentication secret contains an invalid hash")
	}
	return hash, nil
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
	return storage.AtomicWriteFile(path, content, 0o600)
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
