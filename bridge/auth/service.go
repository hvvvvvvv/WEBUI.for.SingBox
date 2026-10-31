package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"time"
	"unicode/utf8"

	"guiforcores/bridge/storage"
)

const (
	secretPath       = "data/secret.key"
	maxLoginFailures = 5
	loginLockout     = 30 * time.Second
	sessionTTL       = 8 * time.Hour
	maxSessions      = 64
	maxFailureKeys   = 256
	maxPasswordBytes = 1024
)

var (
	ErrCredentials = errors.New("Invalid password or verification code")
	ErrRateLimited = errors.New("Too many failed attempts, try again later")
	ErrSession     = errors.New("Session expired; sign in again")
	ErrUnavailable = errors.New("Authentication storage unavailable; use local recovery")
)

type loginFailure struct {
	count            int
	blocked, updated time.Time
}
type session struct {
	generation string
	expires    time.Time
}
type pendingEnrollment struct {
	secret, generation string
	expires            time.Time
}

// Service serializes verification, one-time factor consumption and writes.
// The admission gate bounds expensive KDF work and queued authentication calls.
type Service struct {
	paths          *storage.Paths
	mu             sync.Mutex
	sessions       map[string]session
	pending        map[string]pendingEnrollment
	failures       map[string]loginFailure
	accountFailure loginFailure
	operations     chan struct{}
	now            func() time.Time
	writeFile      func(string, []byte) error
	cachedInfo     os.FileInfo
	cachedState    credentials
}
type Status struct {
	Enabled     bool `json:"enabled"`
	TOTPEnabled bool `json:"totp_enabled"`
}
type Enrollment struct {
	Secret    string    `json:"secret"`
	URI       string    `json:"otpauth_uri"`
	ExpiresAt time.Time `json:"expires_at"`
}
type RecoveryResult struct {
	Token         string   `json:"token"`
	RecoveryCodes []string `json:"recovery_codes"`
}

func NewService(paths *storage.Paths) *Service {
	return &Service{paths: paths, sessions: make(map[string]session), pending: make(map[string]pendingEnrollment), failures: make(map[string]loginFailure), operations: make(chan struct{}, 2), now: time.Now, writeFile: durableWrite}
}
func HashSecret(plain string) string {
	hash := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(hash[:])
}

// VerifySecret is a password-only compatibility helper; HTTP must use Login.
func (s *Service) VerifySecret(plain string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readState()
	return err == nil && state.TOTP == nil && verifyPassword(state, plain)
}

// SecretHash is retained for local diagnostics, never returns a TOTP secret.
func (s *Service) SecretHash() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readState()
	if err != nil {
		return "", err
	}
	if state.Legacy != "" {
		return state.Legacy, nil
	}
	if state.Password != nil {
		return state.Password.Hash, nil
	}
	return "", nil
}
func (s *Service) Status() (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readState()
	if err != nil {
		return Status{}, ErrUnavailable
	}
	return Status{Enabled: state.enabled(), TOTPEnabled: state.TOTP != nil}, nil
}

// SetSecret is local administrator recovery: deliberately reset TOTP and all
// recovery codes too, including when the previous file is damaged.
func (s *Service) SetSecret(secret string) error {
	if len(secret) > maxPasswordBytes {
		return errors.New("Password exceeds 1024 bytes")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := credentials{Version: 1}
	var err error
	if secret != "" {
		state.Password, err = newPassword(secret)
		if err != nil {
			return err
		}
	}
	if state.Generation, err = s.GenerateToken(); err != nil {
		return err
	}
	if err = s.saveState(state); err != nil {
		return err
	}
	clear(s.sessions)
	clear(s.pending)
	return nil
}
func (s *Service) GenerateToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
func (s *Service) attempt(remote string, fn func() error) error {
	select {
	case s.operations <- struct{}{}:
	default:
		return ErrRateLimited
	}
	defer func() { <-s.operations }()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rateLimited(remote) {
		return ErrRateLimited
	}
	err := fn()
	if errors.Is(err, ErrCredentials) {
		s.recordFailure(remote)
	}
	if err == nil {
		delete(s.failures, loginFailureKey(remote))
		s.accountFailure = loginFailure{}
	}
	return err
}
func (s *Service) Login(remote, password, code string) (token string, err error) {
	err = s.attempt(remote, func() error {
		state, readErr := s.readState()
		if readErr != nil {
			return ErrUnavailable
		}
		if !verifyPassword(state, password) || !consumeFactor(&state, code, s.now()) {
			return ErrCredentials
		}
		var tokenErr error
		token, tokenErr = s.GenerateToken()
		if tokenErr != nil {
			return ErrUnavailable
		}
		migrate := state.Legacy != ""
		if migrate {
			state.Password, tokenErr = newPassword(password)
			if tokenErr != nil {
				return ErrUnavailable
			}
			state.Legacy = ""
		}
		// Persist consumption/migration before issuing any session.
		if migrate || state.TOTP != nil {
			if s.saveState(state) != nil {
				return ErrUnavailable
			}
		}
		s.addSession(token, state.Generation)
		return nil
	})
	if err != nil {
		return "", err
	}
	return token, nil
}
func (s *Service) authenticated(token string) (credentials, error) {
	state, err := s.readState()
	if err != nil {
		return credentials{}, ErrUnavailable
	}
	if !s.sessionValid(token, state, false) {
		return credentials{}, ErrSession
	}
	return state, nil
}

// Generate all randomness before writing; revoke sessions only once committed.
func (s *Service) commitChange(state credentials) (string, error) {
	generation, err := s.GenerateToken()
	if err != nil {
		return "", ErrUnavailable
	}
	token, err := s.GenerateToken()
	if err != nil {
		return "", ErrUnavailable
	}
	state.Generation = generation
	if s.saveState(state) != nil {
		return "", ErrUnavailable
	}
	clear(s.sessions)
	clear(s.pending)
	s.addSession(token, generation)
	return token, nil
}
func (s *Service) ChangePassword(remote, token, next, current, code string) (replacement string, err error) {
	if len(next) > maxPasswordBytes || (next != "" && (!utf8.ValidString(next) || utf8.RuneCountInString(next) < 15)) {
		return "", errors.New("New password must contain at least 15 characters and at most 1024 UTF-8 bytes")
	}
	err = s.attempt(remote, func() error {
		state, authErr := s.authenticated(token)
		if authErr != nil {
			return authErr
		}
		if !verifyPassword(state, current) {
			return ErrCredentials
		}
		if state.TOTP != nil && next == "" {
			return errors.New("Disable two-factor authentication before clearing the password")
		}
		if !consumeFactor(&state, code, s.now()) {
			return ErrCredentials
		}
		state.Legacy = ""
		state.Password = nil
		if next != "" {
			state.Password, authErr = newPassword(next)
			if authErr != nil {
				return ErrUnavailable
			}
		}
		replacement, authErr = s.commitChange(state)
		return authErr
	})
	return
}
func (s *Service) BeginTOTP(remote, token, password string) (result Enrollment, err error) {
	err = s.attempt(remote, func() error {
		state, authErr := s.authenticated(token)
		if authErr != nil {
			return authErr
		}
		if !state.enabled() {
			return errors.New("Set a password before enabling two-factor authentication")
		}
		if !verifyPassword(state, password) {
			return ErrCredentials
		}
		if state.TOTP != nil {
			return errors.New("Two-factor authentication is already enabled")
		}
		secret, secretErr := newTOTPSecret()
		if secretErr != nil {
			return ErrUnavailable
		}
		expires := s.now().Add(10 * time.Minute)
		s.pending[token] = pendingEnrollment{secret: secret, generation: state.Generation, expires: expires}
		result = Enrollment{Secret: secret, URI: totpURI(secret), ExpiresAt: expires}
		return nil
	})
	return
}
func (s *Service) CancelTOTP(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, token)
}
func (s *Service) EnableTOTP(remote, token, code string) (result RecoveryResult, err error) {
	err = s.attempt(remote, func() error {
		state, authErr := s.authenticated(token)
		if authErr != nil {
			return authErr
		}
		pending, ok := s.pending[token]
		if !ok || pending.generation != state.Generation || !s.now().Before(pending.expires) {
			delete(s.pending, token)
			return errors.New("Authenticator setup expired; start again")
		}
		if state.TOTP != nil || !state.enabled() {
			return ErrCredentials
		}
		step, valid := matchTOTP(pending.secret, code, s.now(), -1)
		if !valid {
			return ErrCredentials
		}
		codes, hashes, codesErr := newRecoveryCodes()
		if codesErr != nil {
			return ErrUnavailable
		}
		state.TOTP = &totpCredential{Secret: pending.secret, LastStep: step, RecoveryHashes: hashes}
		result.Token, authErr = s.commitChange(state)
		if authErr != nil {
			return authErr
		}
		result.RecoveryCodes = codes
		return nil
	})
	return
}
func (s *Service) DisableTOTP(remote, token, password, code string) (replacement string, err error) {
	err = s.attempt(remote, func() error {
		state, authErr := s.authenticated(token)
		if authErr != nil {
			return authErr
		}
		if state.TOTP == nil {
			return errors.New("Two-factor authentication is not enabled")
		}
		if !verifyPassword(state, password) || !consumeFactor(&state, code, s.now()) {
			return ErrCredentials
		}
		state.TOTP = nil
		replacement, authErr = s.commitChange(state)
		return authErr
	})
	return
}
func (s *Service) RegenerateRecovery(remote, token, password, code string) (result RecoveryResult, err error) {
	err = s.attempt(remote, func() error {
		state, authErr := s.authenticated(token)
		if authErr != nil {
			return authErr
		}
		if state.TOTP == nil {
			return errors.New("Two-factor authentication is not enabled")
		}
		if !verifyPassword(state, password) || !consumeFactor(&state, code, s.now()) {
			return ErrCredentials
		}
		codes, hashes, codesErr := newRecoveryCodes()
		if codesErr != nil {
			return ErrUnavailable
		}
		state.TOTP.RecoveryHashes = hashes
		result.Token, authErr = s.commitChange(state)
		if authErr != nil {
			return authErr
		}
		result.RecoveryCodes = codes
		return nil
	})
	return
}
