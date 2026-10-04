package parser

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const testUUID = "123e4567-e89b-12d3-a456-426614174000"

func TestParseBase64URILists(t *testing.T) {
	t.Parallel()

	// The non-ASCII fragment makes the standard and URL-safe alphabets differ,
	// while the odd payload length exercises padded and raw encodings.
	plain := strings.Join([]string{
		ssURI("aes-128-gcm", "base64-password", "ss.example.com", 8388, "Rocket🚀"),
		"socks5://base64-user:base64-pass@socks.example.com:1080#Base64%20SOCKS",
	}, "\n")
	encodings := []struct {
		name   string
		encode func([]byte) string
	}{
		{name: "standard padded", encode: base64.StdEncoding.EncodeToString},
		{name: "standard raw", encode: base64.RawStdEncoding.EncodeToString},
		{name: "URL-safe padded", encode: base64.URLEncoding.EncodeToString},
		{name: "URL-safe raw", encode: base64.RawURLEncoding.EncodeToString},
	}

	for _, tc := range encodings {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Parse("\ufeff  " + tc.encode([]byte(plain)) + "\n")
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if result.Total != 2 || result.Skipped != 0 || len(result.Outbounds) != 2 {
				t.Fatalf("Parse() counts = total %d, skipped %d, outbounds %d; want 2, 0, 2", result.Total, result.Skipped, len(result.Outbounds))
			}
			assertOutbound(t, outboundWithTag(t, result, "Rocket🚀"), outboundWant{
				typeName: "shadowsocks",
				server:   "ss.example.com",
				port:     8388,
				fields: map[string]any{
					"method":   "aes-128-gcm",
					"password": "base64-password",
				},
			})
			assertOutbound(t, outboundWithTag(t, result, "Base64 SOCKS"), outboundWant{
				typeName: "socks",
				server:   "socks.example.com",
				port:     1080,
				fields: map[string]any{
					"username": "base64-user",
					"password": "base64-pass",
				},
			})
		})
	}
}

func TestParseFullClashYAMLWithLeadingGlobalKeys(t *testing.T) {
	t.Parallel()

	raw := `mixed-port: 7890
mode: rule
dns:
  enable: true
proxies:
  - name: Full Clash SS
    type: ss
    server: full-clash.example.com
    port: 8388
    cipher: aes-128-gcm
    password: full-clash-password
proxy-groups:
  - name: Proxy
    type: select
    proxies: [Full Clash SS]
`
	inputs := map[string]string{
		"plain":          raw,
		"base64 wrapped": base64.RawStdEncoding.EncodeToString([]byte(raw)),
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			result, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if result.Total != 1 || result.Skipped != 0 || len(result.Outbounds) != 1 {
				t.Fatalf("Parse() counts = total %d, skipped %d, outbounds %d; want 1, 0, 1", result.Total, result.Skipped, len(result.Outbounds))
			}
			assertOutbound(t, result.Outbounds[0], outboundWant{
				tag: "Full Clash SS", typeName: "shadowsocks", server: "full-clash.example.com", port: 8388,
				fields: map[string]any{"method": "aes-128-gcm", "password": "full-clash-password"},
			})
		})
	}
}

func TestParsePartialSuccessSkipsUnsupportedAndInvalid(t *testing.T) {
	t.Parallel()

	ss := ssURI("aes-128-gcm", "kept-password", "kept.example.com", 8388, "Kept SS")
	ssr := ssrURI("ssr.example.com", 443, "auth_sha1_v4", "aes-256-cfb", "tls1.2_ticket_auth", "ssr-password", "Skipped SSR")
	wireGuard := "wireguard://WG-PRIVATE-KEY@wg.example.com:51820?publickey=WG-PUBLIC-KEY&address=10.0.0.2%2F32#Skipped%20WireGuard"
	snell := "Kept Snell = snell, snell.example.com, 443, psk=snell-password, version=4"
	invalidSnell := "Skipped Snell = snell, snell.example.com, 443, psk=snell-password, version=3"
	raw := strings.Join([]string{ss, ssr, wireGuard, snell, invalidSnell, "not-a-proxy://invalid-candidate"}, "\n")

	result, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(result.Outbounds) != 2 {
		t.Fatalf("len(Outbounds) = %d, want 2", len(result.Outbounds))
	}
	if result.Total != 6 || result.Skipped != 4 || len(result.Issues) != 4 {
		t.Fatalf("Parse() counts = total %d, skipped %d, issues %d; want 6, 4, 4", result.Total, result.Skipped, len(result.Issues))
	}
	assertOutbound(t, result.Outbounds[0], outboundWant{
		tag: "Kept SS", typeName: "shadowsocks", server: "kept.example.com", port: 8388,
		fields: map[string]any{"method": "aes-128-gcm", "password": "kept-password"},
	})
	assertOutbound(t, outboundWithTag(t, result, "Kept Snell"), outboundWant{
		tag: "Kept Snell", typeName: "snell", server: "snell.example.com", port: 443,
		fields: map[string]any{"version": 4, "psk": "snell-password"},
	})
	if !issuesMention(result.Issues, "unsupported") {
		t.Fatalf("Issues = %#v, want an unsupported-protocol diagnostic", result.Issues)
	}
	for _, issue := range result.Issues {
		if strings.TrimSpace(issue.Parser) == "" || strings.TrimSpace(issue.Reason) == "" {
			t.Errorf("Issue = %#v, want a parser category and sanitized reason", issue)
		}
	}
	indices := map[int]bool{}
	for _, issue := range result.Issues {
		indices[issue.Index] = true
	}
	for _, want := range []int{2, 3, 5, 6} {
		if !indices[want] {
			t.Errorf("Issues = %#v, want original candidate index %d", result.Issues, want)
		}
	}
}

func TestParseAssignsUniqueStableOutboundTags(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		ssURI("aes-128-gcm", "password-1", "one.example.com", 8388, "Duplicate"),
		ssURI("aes-128-gcm", "password-2", "two.example.com", 8388, "Duplicate 2"),
		ssURI("aes-128-gcm", "password-3", "three.example.com", 8388, "Duplicate"),
		ssURI("aes-128-gcm", "password-4", "four.example.com", 8388, "Duplicate"),
	}, "\n")
	result, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got := make([]string, 0, len(result.Outbounds))
	for _, outbound := range result.Outbounds {
		got = append(got, stringValue(outbound["tag"]))
	}
	want := []string{"Duplicate", "Duplicate 2", "Duplicate 3", "Duplicate 4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outbound tags = %#v, want %#v", got, want)
	}
}

func TestParseAllFailureAndDiagnosticsAreSanitized(t *testing.T) {
	t.Parallel()

	const (
		ssrSecret     = "SSR-SUPER-SECRET"
		unknownSecret = "UNKNOWN-SUPER-SECRET"
		unknownScheme = "SCHEME-SUPER-SECRET"
	)
	ssr := ssrURI("ssr.example.com", 443, "auth_sha1_v4", "aes-256-cfb", "tls1.2_ticket_auth", ssrSecret, "Unsupported SSR")
	unknown := unknownScheme + "://" + unknownSecret + "@private.example.com:443#Private"
	raw := strings.Join([]string{ssr, unknown}, "\n")

	result, err := Parse(raw)
	if err == nil {
		t.Fatal("Parse() error = nil, want failure")
	}
	var parseErr *Error
	if !errors.As(err, &parseErr) {
		t.Fatalf("Parse() error type = %T, want *Error", err)
	}
	if len(result.Outbounds) != 0 || result.Total != 2 || result.Skipped != 2 || len(result.Issues) != 2 {
		t.Fatalf("Parse() counts = outbounds %d, total %d, skipped %d, issues %d; want 0, 2, 2, 2", len(result.Outbounds), result.Total, result.Skipped, len(result.Issues))
	}
	if parseErr.Total != result.Total || len(parseErr.Issues) != len(result.Issues) {
		t.Fatalf("Error diagnostics = total %d, issues %d; result = total %d, issues %d", parseErr.Total, len(parseErr.Issues), result.Total, len(result.Issues))
	}

	diagnostics := err.Error() + " " + fmt.Sprint(result.Issues) + " " + fmt.Sprint(parseErr.Issues)
	for _, secret := range []string{ssrSecret, unknownSecret, unknownScheme, ssr, unknown, raw} {
		if strings.Contains(diagnostics, secret) {
			t.Errorf("diagnostics leaked source secret or candidate %q: %s", secret, diagnostics)
		}
	}
	if !issuesMention(result.Issues, "unsupported") {
		t.Fatalf("Issues = %#v, want an unsupported-protocol diagnostic", result.Issues)
	}
	for _, issue := range result.Issues {
		if strings.TrimSpace(issue.Parser) == "" || strings.TrimSpace(issue.Reason) == "" {
			t.Errorf("Issue = %#v, want a parser category and sanitized reason", issue)
		}
	}
}

func TestParseRejectsIncompleteRealityInsteadOfDowngradingTLS(t *testing.T) {
	result, err := Parse("vless://" + testUUID + "@reality.example.com:443?security=reality&sni=reality.example.com#Incomplete%20Reality")
	if err == nil {
		t.Fatal("Parse() error = nil, want incomplete Reality failure")
	}
	if len(result.Outbounds) != 0 || result.Total != 1 || result.Skipped != 1 {
		t.Fatalf("Parse() result = %#v, want one skipped Reality candidate", result)
	}
	if !issuesMention(result.Issues, "Reality public key") {
		t.Fatalf("Issues = %#v, want missing Reality public key diagnostic", result.Issues)
	}
}

func TestParseRejectsCandidateCountOverLimit(t *testing.T) {
	lines := make([]string, maxSubscriptionCandidates+1)
	for index := range lines {
		lines[index] = "unsupported-candidate"
	}
	result, err := Parse(strings.Join(lines, "\n"))
	if err == nil {
		t.Fatal("Parse() error = nil, want candidate-limit failure")
	}
	if len(result.Outbounds) != 0 || result.Total != maxSubscriptionCandidates+1 {
		t.Fatalf("Parse() result = %#v, want no outbounds and total %d", result, maxSubscriptionCandidates+1)
	}
	if !issuesMention(result.Issues, candidateLimitReason) {
		t.Fatalf("Issues = %#v, want candidate limit diagnostic", result.Issues)
	}
}

func TestParseRejectsAggregateInlineDocumentCountOverLimit(t *testing.T) {
	countPerLine := maxSubscriptionCandidates/2 + 1
	candidates := make([]any, countPerLine)
	for index := range candidates {
		candidates[index] = map[string]any{"type": "direct", "name": "Direct"}
	}
	encoded, err := json.Marshal(candidates)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Parse(string(encoded) + "\n" + string(encoded))
	if err == nil {
		t.Fatal("Parse() error = nil, want aggregate candidate-limit failure")
	}
	wantTotal := countPerLine * 2
	if len(result.Outbounds) != 0 || result.Total != wantTotal {
		t.Fatalf("Parse() result has %d outbounds and total %d, want 0 and %d", len(result.Outbounds), result.Total, wantTotal)
	}
	if !issuesMention(result.Issues, candidateLimitReason) {
		t.Fatalf("Issues = %#v, want aggregate candidate limit diagnostic", result.Issues)
	}
}

type outboundWant struct {
	tag      string
	typeName string
	server   string
	port     int
	fields   map[string]any
}

func ssURI(method, password, host string, port int, tag string) string {
	credentials := base64.RawURLEncoding.EncodeToString([]byte(method + ":" + password))
	return "ss://" + credentials + "@" + host + ":" + strconv.Itoa(port) + "#" + tag
}

func ssrURI(host string, port int, protocol, method, obfs, password, tag string) string {
	password64 := base64.RawURLEncoding.EncodeToString([]byte(password))
	tag64 := base64.RawURLEncoding.EncodeToString([]byte(tag))
	payload := fmt.Sprintf("%s:%d:%s:%s:%s:%s/?remarks=%s", host, port, protocol, method, obfs, password64, url.QueryEscape(tag64))
	return "ssr://" + base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func outboundWithTag(t *testing.T, result Result, tag string) map[string]any {
	t.Helper()
	for _, outbound := range result.Outbounds {
		if outbound["tag"] == tag {
			return outbound
		}
	}
	t.Fatalf("no outbound tagged %q in %#v", tag, result.Outbounds)
	return nil
}

func assertOutbound(t *testing.T, got map[string]any, want outboundWant) {
	t.Helper()
	fields := map[string]any{"type": want.typeName, "server": want.server, "server_port": want.port}
	if want.tag != "" {
		fields["tag"] = want.tag
	}
	for key, value := range want.fields {
		fields[key] = value
	}
	for key, value := range fields {
		if !reflect.DeepEqual(got[key], value) {
			t.Errorf("outbound field %q = %#v, want %#v", key, got[key], value)
		}
	}
}

func issuesMention(issues []Issue, substring string) bool {
	for _, issue := range issues {
		if strings.Contains(strings.ToLower(issue.Parser+" "+issue.Reason), strings.ToLower(substring)) {
			return true
		}
	}
	return false
}
