package auth

import (
	"os"
	"testing"
	"time"
)

func TestUnknownAndExpiredSessionsDoNotAccessStorage(t *testing.T) {
	// No Paths at all: any attempted filesystem lookup would panic.
	service := NewService(nil)
	if service.ValidateSession("random-bearer") {
		t.Fatal("unknown session accepted")
	}
	service.sessions["expired"] = session{generation: "anything", expires: time.Now().Add(-time.Hour)}
	if service.ValidateSession("expired") {
		t.Fatal("expired session accepted")
	}
}

func TestSessionCacheDetectsAtomicReplacementWithSameSizeAndTimestamp(t *testing.T) {
	s, token := configuredAuth(t)
	if !s.ValidateSession(token) {
		t.Fatal("initial session rejected")
	}
	original, err := os.Stat(s.paths.Resolve(secretPath))
	if err != nil {
		t.Fatal(err)
	}
	other := NewService(s.paths)
	if err := other.SetSecret("different"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(s.paths.Resolve(secretPath), original.ModTime(), original.ModTime()); err != nil {
		t.Fatal(err)
	}
	replacement, _ := os.Stat(s.paths.Resolve(secretPath))
	if original.Size() != replacement.Size() {
		t.Fatal("fixture must preserve file size")
	}
	if s.ValidateSession(token) {
		t.Fatal("atomic replacement escaped cached session invalidation")
	}
}

func TestSessionCredentialCacheReturnsDeepCopies(t *testing.T) {
	s, token := configuredAuth(t)
	_, result := enrollAuth(t, s, token)
	if !s.ValidateSession(result.Token) {
		t.Fatal("initial session rejected")
	}
	first, err := s.readStateWithCache(true)
	if err != nil {
		t.Fatal(err)
	}
	originalSalt, originalSecret, originalHash := first.Password.Salt, first.TOTP.Secret, first.TOTP.RecoveryHashes[0]
	first.Password.Salt = "mutated"
	first.TOTP.Secret = "mutated"
	first.TOTP.RecoveryHashes[0] = "mutated"
	second, err := s.readStateWithCache(true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Password.Salt != originalSalt || second.TOTP.Secret != originalSecret || second.TOTP.RecoveryHashes[0] != originalHash {
		t.Fatal("cache aliases transaction state")
	}
}
