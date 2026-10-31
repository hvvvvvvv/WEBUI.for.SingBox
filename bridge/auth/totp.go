package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func newTOTPSecret() (string, error) {
	data := make([]byte, 20)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(data), nil
}
func totpURI(secret string) string {
	values := url.Values{"secret": {secret}, "issuer": {"WEBUI.for.SingBox"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/WEBUI.for.SingBox:admin?" + values.Encode()
}

// hotp implements RFC 4226 dynamic truncation. SHA-1 is required by the
// authenticator interoperability contract, not used for password storage.
func hotp(secret []byte, step int64, digits int) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	modulus := uint32(1000000)
	if digits == 8 {
		modulus = 100000000
	}
	return fmt.Sprintf("%0*d", digits, value%modulus)
}
func matchTOTP(secret, code string, now time.Time, last int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, char := range code {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return 0, false
	}
	step := now.Unix() / 30
	matched := int64(-1)
	for offset := int64(-1); offset <= 1; offset++ {
		candidate := step + offset
		if candidate >= 0 && candidate > last && subtle.ConstantTimeCompare([]byte(hotp(key, candidate, 6)), []byte(code)) == 1 {
			matched = candidate
		}
	}
	return matched, matched >= 0
}
func newRecoveryCodes() ([]string, []string, error) {
	codes, hashes := make([]string, 10), make([]string, 10)
	for i := range codes {
		data := make([]byte, 16)
		if _, err := rand.Read(data); err != nil {
			return nil, nil, err
		}
		raw := hex.EncodeToString(data)
		codes[i] = raw[:8] + "-" + raw[8:16] + "-" + raw[16:24] + "-" + raw[24:]
		hashes[i] = HashSecret(raw)
	}
	return codes, hashes, nil
}
func consumeFactor(state *credentials, code string, now time.Time) bool {
	if state.TOTP == nil {
		return true
	}
	if len(code) > 64 {
		return false
	}
	if step, ok := matchTOTP(state.TOTP.Secret, code, now, state.TOTP.LastStep); ok {
		state.TOTP.LastStep = step
		return true
	}
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(code)), "-", "")
	if !validHex(normalized, 16) {
		return false
	}
	candidate := HashSecret(normalized)
	matched := -1
	for i, expected := range state.TOTP.RecoveryHashes {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) == 1 {
			matched = i
		}
	}
	if matched < 0 {
		return false
	}
	state.TOTP.RecoveryHashes = append(state.TOTP.RecoveryHashes[:matched], state.TOTP.RecoveryHashes[matched+1:]...)
	return true
}
