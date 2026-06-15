package bridge

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

	"github.com/jellydator/ttlcache/v3"
)

const SECRET_PATH = "data/secret.key"

const (
	maxLoginFailures = 5
	loginLockout     = 30 * time.Second
)

var secretRWMutex = &sync.RWMutex{}

var loginFailureStore = struct {
	sync.Mutex
	items map[string]loginFailure
}{items: make(map[string]loginFailure)}

type loginFailure struct {
	count   int
	blocked time.Time
}

// sessions stores valid session tokens in memory.
var sessions = ttlcache.New(
	ttlcache.WithTTL[string, struct{}](8 * time.Hour),
)

// var sessions = struct {
// 	sync.RWMutex
// 	tokens map[string]bool
// }{tokens: make(map[string]bool)}

// HashSecret returns the SHA-256 hex digest of the given secret.
func HashSecret(plain string) string {
	h := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(h[:])
}

// VerifySecret checks whether the given plain-text secret matches the stored hash.
func VerifySecret(plain string) bool {
	hash := GetSecretKey()
	if hash == "" {
		return true // No secret set, allow all
	}
	return HashSecret(plain) == hash
}

func GetSecretKey() string {
	keyPath := Env.BasePath + "/" + SECRET_PATH

	secretRWMutex.RLock()
	defer secretRWMutex.RUnlock()
	if data, err := os.ReadFile(keyPath); err == nil {
		if key := strings.TrimSpace(string(data)); key != "" {
			return key
		}
	}
	if Config != nil && Config.AuthSecret != "" {
		return strings.TrimSpace(Config.AuthSecret)
	}
	return ""
}

func SetSecretKey(plain string) error {
	keyPath := Env.BasePath + "/" + SECRET_PATH

	secretRWMutex.Lock()
	defer secretRWMutex.Unlock()
	var content []byte
	if plain != "" {
		content = []byte(HashSecret(plain))
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, content, 0600); err != nil {
		return err
	}

	if Config != nil && Config.AuthSecret != "" {
		Config.AuthSecret = ""
		return SaveConfig()
	}
	return nil
}

// GenerateToken creates a cryptographically random session token.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// AddSession registers a session token.
func AddSession(token string) {
	sessions.Set(token, struct{}{}, ttlcache.DefaultTTL)
	sessions.DeleteExpired()
}

// ValidateSession checks whether the token is a valid session.
func ValidateSession(token string) bool {
	return sessions.Has(token)
}

// RemoveSession removes a single session token.
func RemoveSession(token string) {
	sessions.Delete(token)
}

// ClearSessions removes all session tokens (e.g. after password reset).
func ClearSessions() {
	sessions.DeleteAll()
}

func ClearSessionsWithExclude(token string) {
	removeItems := list.New()
	sessions.Range(func(item *ttlcache.Item[string, struct{}]) bool {
		if item.Key() != token {
			removeItems.PushBack(item.Key())
		}
		return true
	})
	for e := removeItems.Front(); e != nil; e = e.Next() {
		if key, ok := e.Value.(string); ok {
			sessions.Delete(key)
		}
	}
}

func IsLoginRateLimited(remoteAddr string) bool {
	key := loginFailureKey(remoteAddr)
	if key == "" {
		return false
	}

	loginFailureStore.Lock()
	defer loginFailureStore.Unlock()

	item, ok := loginFailureStore.items[key]
	if !ok {
		return false
	}
	if time.Now().Before(item.blocked) {
		return true
	}
	if !item.blocked.IsZero() {
		delete(loginFailureStore.items, key)
	}
	return false
}

func RecordLoginFailure(remoteAddr string) {
	key := loginFailureKey(remoteAddr)
	if key == "" {
		return
	}

	loginFailureStore.Lock()
	defer loginFailureStore.Unlock()

	item := loginFailureStore.items[key]
	if time.Now().Before(item.blocked) {
		return
	}

	item.count++
	if item.count >= maxLoginFailures {
		item.count = 0
		item.blocked = time.Now().Add(loginLockout)
	}
	loginFailureStore.items[key] = item
}

func ClearLoginFailures(remoteAddr string) {
	key := loginFailureKey(remoteAddr)
	if key == "" {
		return
	}

	loginFailureStore.Lock()
	defer loginFailureStore.Unlock()
	delete(loginFailureStore.items, key)
}

func loginFailureKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(remoteAddr)
}
