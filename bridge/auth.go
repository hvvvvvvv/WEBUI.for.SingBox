package bridge

import (
	"container/list"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

const SECRET_PATH = "data/secret.key"

var secretRWMutex = &sync.RWMutex{}	

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
		return string(data)
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
	return os.WriteFile(keyPath, content, 0600)
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
