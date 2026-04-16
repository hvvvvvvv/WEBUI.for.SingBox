package bridge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// sessions stores valid session tokens in memory.
var sessions = ttlcache.New(
	ttlcache.WithTTL[string, struct{}](8 * time.Hour),
)
// var sessions = struct {
// 	sync.RWMutex
// 	tokens map[string]bool
// }{tokens: make(map[string]bool)}

// HashSecret returns the SHA-256 hex digest of the given secret.
func HashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

// VerifySecret checks whether the given plain-text secret matches the stored hash.
func VerifySecret(plain, hash string) bool {
	if hash == "" {
		return true // No secret set, allow all
	}
	return HashSecret(plain) == hash
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
