package auth

import (
	"encoding/base32"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"guiforcores/bridge/storage"
)

func TestRFC4226AndRFC6238Vectors(t *testing.T) {
	key := []byte("12345678901234567890")
	for step, expected := range []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"} {
		if actual := hotp(key, int64(step), 6); actual != expected {
			t.Fatalf("HOTP step %d: %s != %s", step, actual, expected)
		}
	}
	for _, vector := range []struct {
		at   int64
		code string
	}{{59, "94287082"}, {1111111109, "07081804"}, {1111111111, "14050471"}, {1234567890, "89005924"}, {2000000000, "69279037"}, {20000000000, "65353130"}} {
		if actual := hotp(key, vector.at/30, 8); actual != vector.code {
			t.Fatalf("RFC 6238 time %d: %s", vector.at, actual)
		}
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
	now := time.Unix(1234567890, 0)
	step := now.Unix() / 30
	for _, offset := range []int64{-2, -1, 0, 1, 2} {
		got, valid := matchTOTP(secret, hotp(key, step+offset, 6), now, -1)
		want := offset >= -1 && offset <= 1
		if valid != want || (valid && got != step+offset) {
			t.Fatalf("window offset %d: %d %v", offset, got, valid)
		}
	}
	if _, ok := matchTOTP(secret, hotp(key, step, 6), now, step); ok {
		t.Fatal("accepted consumed step")
	}
}

func configuredAuth(t *testing.T) (*Service, string) {
	t.Helper()
	s := NewService(storage.NewPaths(t.TempDir()))
	s.now = func() time.Time { return time.Unix(1730000010, 0) }
	if err := s.SetSecret("correct"); err != nil {
		t.Fatal(err)
	}
	token, err := s.Login("one", "correct", "")
	if err != nil {
		t.Fatal(err)
	}
	return s, token
}
func enrollAuth(t *testing.T, s *Service, token string) (Enrollment, RecoveryResult) {
	t.Helper()
	pending, err := s.BeginTOTP("one", token, "correct")
	if err != nil {
		t.Fatal(err)
	}
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(pending.Secret)
	result, err := s.EnableTOTP("one", token, hotp(key, s.now().Unix()/30, 6))
	if err != nil {
		t.Fatal(err)
	}
	return pending, result
}
func TestLegacyPasswordMigratesOnlyAfterSuccessfulLogin(t *testing.T) {
	paths := storage.NewPaths(t.TempDir())
	if err := os.MkdirAll(paths.Resolve("data"), 0700); err != nil {
		t.Fatal(err)
	}
	original := HashSecret("short legacy")
	if err := os.WriteFile(paths.Resolve(secretPath), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewService(paths)
	if _, err := s.Login("one", "wrong", ""); !errors.Is(err, ErrCredentials) {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(paths.Resolve(secretPath))
	if string(before) != original {
		t.Fatal("failed login migrated credentials")
	}
	token, err := s.Login("one", "short legacy", "")
	if err != nil || !s.ValidateSession(token) {
		t.Fatal(err)
	}
	state, err := s.readState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Legacy != "" || state.Password.Algorithm != "pbkdf2-sha256" || state.Password.Iterations != 600000 || len(state.Password.Salt) != 32 {
		t.Fatalf("not migrated: %+v", state.Password)
	}
	reloaded := NewService(paths)
	if !reloaded.VerifySecret("short legacy") || reloaded.VerifySecret("wrong") {
		t.Fatal("migrated verification failed")
	}
	other, _ := newPassword("short legacy")
	if other.Salt == state.Password.Salt || other.Hash == state.Password.Hash {
		t.Fatal("KDF not randomly salted")
	}
}
func TestEnrollmentBoundToSessionAndConfirmation(t *testing.T) {
	s, token := configuredAuth(t)
	s.AddSession("other")
	pending, err := s.BeginTOTP("one", token, "correct")
	if err != nil {
		t.Fatal(err)
	}
	status, _ := s.Status()
	if status.TOTPEnabled {
		t.Fatal("enabled before confirmation")
	}
	content, _ := os.ReadFile(s.paths.Resolve(secretPath))
	if strings.Contains(string(content), pending.Secret) {
		t.Fatal("pending secret persisted")
	}
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(pending.Secret)
	code := hotp(key, s.now().Unix()/30, 6)
	if _, err := s.EnableTOTP("other", "other", code); err == nil {
		t.Fatal("another session enabled pending factor")
	}
	s.CancelTOTP(token)
	if _, err := s.EnableTOTP("one", token, code); err == nil {
		t.Fatal("canceled enrollment remained")
	}
	if _, err := s.BeginTOTP("one", token, "wrong"); !errors.Is(err, ErrCredentials) {
		t.Fatal("setup did not reauthenticate")
	}
	_, result := enrollAuth(t, s, token)
	if len(result.RecoveryCodes) != 10 || !s.ValidateSession(result.Token) || s.ValidateSession(token) || s.ValidateSession("other") {
		t.Fatal("enable did not rotate sessions")
	}
	if _, err := s.BeginTOTP("one", result.Token, "correct"); err == nil {
		t.Fatal("secret of enabled factor can be replaced")
	}
	if s.VerifySecret("correct") {
		t.Fatal("password-only helper bypassed TOTP")
	}
}
func TestTOTPReplayAndRecoveryConsumptionSurviveRestart(t *testing.T) {
	s, token := configuredAuth(t)
	pending, result := enrollAuth(t, s, token)
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(pending.Secret)
	code := hotp(key, s.now().Unix()/30, 6)
	if _, err := s.Login("one", "correct", code); !errors.Is(err, ErrCredentials) {
		t.Fatal("enable code replayed", err)
	}
	if _, err := s.Login("one", "correct", ""); !errors.Is(err, ErrCredentials) {
		t.Fatal("second factor optional", err)
	}
	recovery := result.RecoveryCodes[0]
	if _, err := s.Login("one", "wrong", recovery); !errors.Is(err, ErrCredentials) {
		t.Fatal(err)
	}
	if _, err := s.Login("one", "correct", recovery); err != nil {
		t.Fatal("wrong password consumed recovery code", err)
	}
	restarted := NewService(s.paths)
	restarted.now = s.now
	if _, err := restarted.Login("two", "correct", recovery); !errors.Is(err, ErrCredentials) {
		t.Fatal("recovery replay after restart", err)
	}
	if _, err := restarted.Login("two", "correct", code); !errors.Is(err, ErrCredentials) {
		t.Fatal("TOTP replay after restart", err)
	}
	future := hotp(key, s.now().Unix()/30+1, 6)
	if _, err := restarted.Login("two", "correct", future); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Login("two", "correct", future); !errors.Is(err, ErrCredentials) {
		t.Fatal("TOTP replay", err)
	}
	content, _ := os.ReadFile(s.paths.Resolve(secretPath))
	if strings.Contains(string(content), recovery) {
		t.Fatal("plaintext recovery code persisted")
	}
}
func TestConcurrentOneTimeCodeIssuesAtMostOneSession(t *testing.T) {
	for _, kind := range []string{"totp", "recovery"} {
		t.Run(kind, func(t *testing.T) {
			s, token := configuredAuth(t)
			pending, result := enrollAuth(t, s, token)
			code := result.RecoveryCodes[0]
			if kind == "totp" {
				key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(pending.Secret)
				code = hotp(key, s.now().Unix()/30+1, 6)
			}
			var successes atomic.Int32
			var wait sync.WaitGroup
			for range 12 {
				wait.Add(1)
				go func() {
					defer wait.Done()
					if _, err := s.Login("one", "correct", code); err == nil {
						successes.Add(1)
					}
				}()
			}
			wait.Wait()
			if successes.Load() != 1 {
				t.Fatalf("one-time code authenticated %d requests", successes.Load())
			}
		})
	}
}
func TestChangesRequireBothFactorsAndRevokeSessions(t *testing.T) {
	s, old := configuredAuth(t)
	_, result := enrollAuth(t, s, old)
	if _, err := s.ChangePassword("one", result.Token, "a sufficiently long new password", "correct", ""); !errors.Is(err, ErrCredentials) {
		t.Fatal("password change bypassed factor", err)
	}
	if _, err := s.DisableTOTP("one", result.Token, "wrong", result.RecoveryCodes[0]); !errors.Is(err, ErrCredentials) {
		t.Fatal(err)
	}
	if _, err := s.ChangePassword("one", result.Token, "", "correct", result.RecoveryCodes[0]); err == nil {
		t.Fatal("cleared password under enabled TOTP")
	}
	replacement, err := s.RegenerateRecovery("one", result.Token, "correct", result.RecoveryCodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if s.ValidateSession(result.Token) || !s.ValidateSession(replacement.Token) {
		t.Fatal("recovery regeneration did not rotate sessions")
	}
	if _, err := s.Login("one", "correct", result.RecoveryCodes[1]); !errors.Is(err, ErrCredentials) {
		t.Fatal("old recovery code survived regeneration", err)
	}
	disabled, err := s.DisableTOTP("one", replacement.Token, "correct", replacement.RecoveryCodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if s.ValidateSession(replacement.Token) || !s.ValidateSession(disabled) {
		t.Fatal("disable did not rotate sessions")
	}
	status, _ := s.Status()
	if status.TOTPEnabled || !status.Enabled {
		t.Fatal(status)
	}
	cleared, err := s.ChangePassword("one", disabled, "", "correct", "")
	if err != nil {
		t.Fatal(err)
	}
	if !s.ValidateSession(cleared) || s.ValidateSession(disabled) {
		t.Fatal("clear did not rotate sessions")
	}
	status, _ = s.Status()
	if status.Enabled {
		t.Fatal(status)
	}
}
func TestFailedPersistenceDoesNotConsumeCodeOrChangeSessions(t *testing.T) {
	s, token := configuredAuth(t)
	_, result := enrollAuth(t, s, token)
	before, _ := os.ReadFile(s.paths.Resolve(secretPath))
	original := s.writeFile
	s.writeFile = func(string, []byte) error { return errors.New("simulated disk failure") }
	if _, err := s.Login("one", "correct", result.RecoveryCodes[0]); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := s.DisableTOTP("one", result.Token, "correct", result.RecoveryCodes[0]); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if !s.ValidateSession(result.Token) {
		t.Fatal("failed write invalidated original session")
	}
	after, _ := os.ReadFile(s.paths.Resolve(secretPath))
	if string(after) != string(before) {
		t.Fatal("failed transaction changed disk")
	}
	s.writeFile = original
	if _, err := s.Login("one", "correct", result.RecoveryCodes[0]); err != nil {
		t.Fatal("failed write consumed recovery", err)
	}
}
func TestLocalRecoveryInvalidatesOtherServiceSessionsAndDamagedState(t *testing.T) {
	s, token := configuredAuth(t)
	_, result := enrollAuth(t, s, token)
	local := NewService(s.paths)
	if err := local.SetSecret("replacement"); err != nil {
		t.Fatal(err)
	}
	if s.ValidateSession(result.Token) {
		t.Fatal("other process reset left session valid")
	}
	status, _ := s.Status()
	if status.TOTPEnabled || !s.VerifySecret("replacement") {
		t.Fatal("local recovery retained factor")
	}
	s.AddSession("damaged")
	for _, data := range []string{`{}`, `{"version":1}`, strings.Repeat("x", maxCredentialBytes+1)} {
		if err := os.WriteFile(s.paths.Resolve(secretPath), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Status(); err == nil {
			t.Fatal("damaged state opened login")
		}
		if s.ValidateSession("damaged") {
			t.Fatal("damaged state accepted session")
		}
	}
	if err := local.SetSecret("restored"); err != nil || !s.VerifySecret("restored") {
		t.Fatal("CLI failed to repair corrupt state", err)
	}
}
func TestEnrollmentExpiryAndResourceLimits(t *testing.T) {
	s, token := configuredAuth(t)
	pending, err := s.BeginTOTP("one", token, "correct")
	if err != nil {
		t.Fatal(err)
	}
	expired := pending.ExpiresAt
	s.now = func() time.Time { return expired }
	if _, err := s.EnableTOTP("one", token, "000000"); err == nil {
		t.Fatal("expired enrollment enabled")
	}
	for i := range 1000 {
		s.AddSession(string(rune(i + 1)))
		s.RecordLoginFailure(string(rune(i + 1)))
	}
	if len(s.sessions) > maxSessions || len(s.failures) > maxFailureKeys {
		t.Fatal("unbounded authentication maps")
	}
	if !s.IsLoginRateLimited("new-address") {
		t.Fatal("rotating addresses bypassed account limit")
	}
	if _, err := s.ChangePassword("one", token, "short", "correct", ""); err == nil {
		t.Fatal("new weak password accepted")
	}
}
func TestCredentialWorkFactorIsBounded(t *testing.T) {
	s, _ := configuredAuth(t)
	state, _ := s.readState()
	state.Password.Iterations = 999999999
	data, _ := json.Marshal(state)
	if err := os.WriteFile(s.paths.Resolve(secretPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Status(); err == nil {
		t.Fatal("unbounded password work factor loaded")
	}
}
