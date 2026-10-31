package auth

import (
	"bytes"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const passwordIterations = 600000 // OWASP PBKDF2-HMAC-SHA256 baseline.
const maxCredentialBytes = 16 * 1024

type passwordCredential struct {
	Algorithm  string `json:"algorithm"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Hash       string `json:"hash"`
}
type totpCredential struct {
	Secret         string   `json:"secret"`
	LastStep       int64    `json:"last_step"`
	RecoveryHashes []string `json:"recovery_hashes"`
}
type credentials struct {
	Version    int                 `json:"version"`
	Generation string              `json:"generation"`
	Password   *passwordCredential `json:"password"`
	TOTP       *totpCredential     `json:"totp"`
	Legacy     string              `json:"legacy,omitempty"`
}

func (c credentials) enabled() bool { return c.Password != nil || c.Legacy != "" }
func validHex(value string, size int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == size
}
func newPassword(password string) (*passwordCredential, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if err != nil {
		return nil, err
	}
	return &passwordCredential{Algorithm: "pbkdf2-sha256", Iterations: passwordIterations, Salt: hex.EncodeToString(salt), Hash: hex.EncodeToString(key)}, nil
}
func verifyPassword(state credentials, password string) bool {
	if len(password) > maxPasswordBytes {
		return false
	}
	if state.Legacy != "" {
		candidate := HashSecret(password)
		return subtle.ConstantTimeCompare([]byte(candidate), []byte(state.Legacy)) == 1
	}
	if state.Password == nil {
		return true
	}
	salt, _ := hex.DecodeString(state.Password.Salt)
	key, err := pbkdf2.Key(sha256.New, password, salt, state.Password.Iterations, 32)
	if err != nil {
		return false
	}
	expected, _ := hex.DecodeString(state.Password.Hash)
	return subtle.ConstantTimeCompare(key, expected) == 1
}
func cloneCredentials(state credentials) credentials {
	if state.Password != nil {
		password := *state.Password
		state.Password = &password
	}
	if state.TOTP != nil {
		factor := *state.TOTP
		factor.RecoveryHashes = append([]string{}, factor.RecoveryHashes...)
		state.TOTP = &factor
	}
	return state
}
func (s *Service) readState() (credentials, error) { return s.readStateWithCache(false) }

// Only session validation uses the cache. Authentication and mutations always
// reread disk. Atomic CLI replacement changes the file identity and immediately
// invalidates the cache; corruption/removal and permission changes fail closed.
func (s *Service) readStateWithCache(allowCache bool) (state credentials, err error) {
	defer func() {
		if err != nil {
			s.cachedInfo = nil
			s.cachedState = credentials{}
		}
	}()
	path := s.paths.Resolve(secretPath)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing installation is valid; broken links and invalid parents are not.
			if _, linkErr := os.Lstat(path); os.IsNotExist(linkErr) {
				parent, parentErr := os.Stat(filepath.Dir(path))
				if os.IsNotExist(parentErr) || (parentErr == nil && parent.IsDir()) {
					return credentials{Version: 1, Generation: "unconfigured"}, nil
				}
			}
		}
		return credentials{}, fmt.Errorf("inspect authentication secret: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxCredentialBytes {
		return credentials{}, errors.New("authentication secret has invalid type or size")
	}
	file, err := os.Open(path)
	if err != nil {
		return credentials{}, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() {
		return credentials{}, errors.New("cannot inspect opened authentication secret")
	}
	// Opening first also checks current read access. Stat alone can succeed for
	// a locked or ACL-denied file, and must not turn a read failure into access.
	if allowCache && s.cachedInfo != nil && os.SameFile(openedInfo, s.cachedInfo) && openedInfo.Size() == s.cachedInfo.Size() && openedInfo.ModTime().Equal(s.cachedInfo.ModTime()) && openedInfo.Mode() == s.cachedInfo.Mode() {
		return cloneCredentials(s.cachedState), nil
	}
	defer func() {
		if err == nil {
			s.cachedInfo = openedInfo
			s.cachedState = cloneCredentials(state)
		}
	}()
	data, err := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
	if err != nil {
		return credentials{}, err
	}
	if len(data) > maxCredentialBytes {
		return credentials{}, errors.New("authentication secret too large")
	}
	if len(data) == 0 {
		return credentials{Version: 1, Generation: "legacy-disabled"}, nil
	}
	value := strings.TrimSpace(string(data))
	if validHex(value, sha256.Size) {
		return credentials{Version: 1, Generation: value, Legacy: value}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return credentials{}, errors.New("invalid authentication state")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return credentials{}, errors.New("invalid authentication state")
	}
	// Require every top-level field: a damaged/truncated JSON object must never
	// silently become an unconfigured installation through zero values.
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	for _, key := range []string{"version", "generation", "password", "totp"} {
		if _, ok := fields[key]; !ok {
			return credentials{}, errors.New("incomplete authentication state")
		}
	}
	if state.Version != 1 || !validHex(state.Generation, 32) || (state.Legacy != "" && !validHex(state.Legacy, 32)) {
		return credentials{}, errors.New("invalid authentication state")
	}
	if state.Password != nil {
		p := state.Password
		// Reject attacker-controlled excessive work factors; upgrading the work
		// factor requires an explicit format migration instead of unbounded CPU.
		if p.Algorithm != "pbkdf2-sha256" || p.Iterations != passwordIterations || !validHex(p.Salt, 16) || !validHex(p.Hash, 32) || state.Legacy != "" {
			return credentials{}, errors.New("invalid password credential")
		}
	}
	if state.TOTP != nil {
		t := state.TOTP
		var factorFields map[string]json.RawMessage
		_ = json.Unmarshal(fields["totp"], &factorFields)
		for _, key := range []string{"secret", "last_step", "recovery_hashes"} {
			value, ok := factorFields[key]
			if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return credentials{}, errors.New("incomplete authenticator credential")
			}
		}
		secret, secretErr := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(t.Secret)
		if !state.enabled() || secretErr != nil || len(secret) != 20 || t.LastStep < 0 || len(t.RecoveryHashes) > 10 {
			return credentials{}, errors.New("invalid authenticator credential")
		}
		seen := map[string]bool{}
		for _, hash := range t.RecoveryHashes {
			if !validHex(hash, 32) || seen[hash] {
				return credentials{}, errors.New("invalid recovery credential")
			}
			seen[hash] = true
		}
	}
	return state, nil
}
func (s *Service) saveState(state credentials) error {
	s.cachedInfo = nil
	s.cachedState = credentials{}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	path := s.paths.Resolve(secretPath)
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return s.writeFile(path, data)
}
func durableWrite(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".secret.key.tmp-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	defer file.Close()
	if err = file.Chmod(0o600); err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = replaceCredentialFile(temporary, path); err != nil {
		return err
	}
	return syncCredentialDirectory(filepath.Dir(path))
}
