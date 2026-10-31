// Package routerplugin applies a small, declarative configuration vocabulary.
// Manifests are data: they cannot execute code or request arbitrary host access.
package routerplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	profilev1 "guiforcores/gen/profile/v1"
)

const (
	APIVersion            = "webui.router/v1"
	MaxPlugins            = 32
	MaxManifestBytes      = 16 << 10
	MaxTotalManifestBytes = 256 << 10
	MaxCIDRs              = 256
	MaxInterfaces         = 16
)

var (
	idPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	versionPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	interfacePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,15}$`)
)

type Manifest struct {
	APIVersion  string          `json:"apiVersion"`
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Version     string          `json:"version"`
	Kind        string          `json:"kind"`
	Config      json.RawMessage `json:"config"`
}

type tunRoutingConfig struct {
	Inbound          string   `json:"inbound"`
	AutoRedirect     bool     `json:"auto_redirect"`
	StrictRoute      bool     `json:"strict_route"`
	IncludeInterface []string `json:"include_interface"`
	ExcludeAddress   []string `json:"exclude_address"`
}

type sourceRoutingConfig struct {
	Inbound      string   `json:"inbound"`
	SourceIPCIDR []string `json:"source_ip_cidr"`
	Outbound     string   `json:"outbound"`
}

type dnsHijackConfig struct {
	Inbound string `json:"inbound"`
}

// ParseManifest validates the protocol, including templates with empty references.
// Enabled instances additionally require nonempty references in Validate.
func ParseManifest(raw string) (Manifest, error) {
	var manifest Manifest
	if len(raw) == 0 || len(raw) > MaxManifestBytes || !utf8.ValidString(raw) {
		return manifest, fmt.Errorf("manifest must be valid UTF-8 JSON of at most %d bytes", MaxManifestBytes)
	}
	if err := strictDecode([]byte(raw), &manifest); err != nil {
		return manifest, fmt.Errorf("manifest: %w", err)
	}
	if manifest.APIVersion != APIVersion {
		return manifest, fmt.Errorf("unsupported apiVersion %q", manifest.APIVersion)
	}
	if !idPattern.MatchString(manifest.ID) {
		return manifest, fmt.Errorf("manifest.id must contain 1-64 letters, digits, dots, underscores or hyphens and start with a letter or digit")
	}
	if strings.TrimSpace(manifest.Name) == "" || len(manifest.Name) > 128 || hasControl(manifest.Name) {
		return manifest, fmt.Errorf("manifest.name must contain 1-128 bytes of text")
	}
	if len(manifest.Description) > 1024 || strings.ContainsRune(manifest.Description, 0) {
		return manifest, fmt.Errorf("manifest.description exceeds its text limit")
	}
	if !validVersion(manifest.Version) {
		return manifest, fmt.Errorf("manifest.version must be a semantic version of at most 64 bytes")
	}
	if _, err := decodeConfig(manifest, false); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func validVersion(version string) bool {
	if len(version) > 64 {
		return false
	}
	match := versionPattern.FindStringSubmatch(version)
	if match == nil {
		return false
	}
	for _, part := range strings.Split(match[4], ".") {
		if len(part) > 1 && part[0] == '0' && strings.Trim(part, "0123456789") == "" {
			return false
		}
	}
	return true
}

func hasControl(value string) bool { return strings.ContainsFunc(value, unicode.IsControl) }

func validateTag(tag, field string, required bool) error {
	if len(tag) > 128 || hasControl(tag) || tag != strings.TrimSpace(tag) || (required && tag == "") {
		return fmt.Errorf("%s must be a %s tag of at most 128 bytes", field, map[bool]string{true: "nonempty", false: "valid"}[required])
	}
	return nil
}

func validateCIDRs(values []string, field string, required bool) error {
	if len(values) > MaxCIDRs || (required && len(values) == 0) {
		return fmt.Errorf("%s must contain %d-%d CIDRs", field, map[bool]int{true: 1, false: 0}[required], MaxCIDRs)
	}
	seen := make(map[netip.Prefix]bool, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return fmt.Errorf("%s contains invalid CIDR %q", field, value)
		}
		prefix = prefix.Masked()
		if seen[prefix] {
			return fmt.Errorf("%s contains duplicate CIDR %q", field, value)
		}
		seen[prefix] = true
	}
	return nil
}

func decodeConfig(manifest Manifest, required bool) (any, error) {
	switch manifest.Kind {
	case "tun-routing":
		var value tunRoutingConfig
		if err := strictDecode(manifest.Config, &value); err != nil {
			return nil, fmt.Errorf("tun-routing.config: %w", err)
		}
		if err := validateTag(value.Inbound, "config.inbound", required); err != nil {
			return nil, err
		}
		if len(value.IncludeInterface) > MaxInterfaces {
			return nil, fmt.Errorf("config.include_interface exceeds %d entries", MaxInterfaces)
		}
		seen := make(map[string]bool, len(value.IncludeInterface))
		for _, name := range value.IncludeInterface {
			if !interfacePattern.MatchString(name) || name == "." || name == ".." || seen[name] {
				return nil, fmt.Errorf("config.include_interface contains invalid or duplicate interface %q", name)
			}
			seen[name] = true
		}
		if err := validateCIDRs(value.ExcludeAddress, "config.exclude_address", false); err != nil {
			return nil, err
		}
		return value, nil
	case "source-routing":
		var value sourceRoutingConfig
		if err := strictDecode(manifest.Config, &value); err != nil {
			return nil, fmt.Errorf("source-routing.config: %w", err)
		}
		if err := validateTag(value.Inbound, "config.inbound", required); err != nil {
			return nil, err
		}
		if err := validateTag(value.Outbound, "config.outbound", required); err != nil {
			return nil, err
		}
		if err := validateCIDRs(value.SourceIPCIDR, "config.source_ip_cidr", required); err != nil {
			return nil, err
		}
		return value, nil
	case "dns-hijack":
		var value dnsHijackConfig
		if err := strictDecode(manifest.Config, &value); err != nil {
			return nil, fmt.Errorf("dns-hijack.config: %w", err)
		}
		if err := validateTag(value.Inbound, "config.inbound", required); err != nil {
			return nil, err
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported plugin kind %q", manifest.Kind)
	}
}

func Validate(plugins []*profilev1.RouterPlugin) error {
	_, err := parseInstances(plugins)
	return err
}

type instance struct {
	id     string
	config any
}

func parseInstances(plugins []*profilev1.RouterPlugin) ([]instance, error) {
	if len(plugins) > MaxPlugins {
		return nil, fmt.Errorf("router_plugins exceeds %d instances", MaxPlugins)
	}
	result := make([]instance, 0, len(plugins))
	seen := make(map[string]bool, len(plugins))
	total := 0
	for index, plugin := range plugins {
		if plugin == nil {
			return nil, fmt.Errorf("router_plugins[%d] must be an instance", index)
		}
		if !idPattern.MatchString(plugin.GetId()) || seen[plugin.GetId()] {
			return nil, fmt.Errorf("router_plugins[%d].id is invalid or duplicated", index)
		}
		seen[plugin.GetId()] = true
		total += len(plugin.GetManifestJson())
		if total > MaxTotalManifestBytes {
			return nil, fmt.Errorf("router_plugins exceeds %d total manifest bytes", MaxTotalManifestBytes)
		}
		manifest, err := ParseManifest(plugin.GetManifestJson())
		if err != nil {
			return nil, fmt.Errorf("router_plugins[%d]: %w", index, err)
		}
		parsed, err := decodeConfig(manifest, plugin.GetEnabled())
		if err != nil {
			return nil, fmt.Errorf("router_plugins[%d]: %w", index, err)
		}
		if plugin.GetEnabled() {
			result = append(result, instance{id: plugin.GetId(), config: parsed})
		}
	}
	return result, nil
}

// Disallow duplicate keys, null values and excessive nesting in addition to
// unknown fields. encoding/json alone otherwise silently accepts these forms.
func strictDecode(raw []byte, target any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("expected a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := validateJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("unexpected content after JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return err
	}
	allowed := make(map[string]bool)
	typ := reflect.TypeOf(target).Elem()
	for index := 0; index < typ.NumField(); index++ {
		allowed[strings.Split(typ.Field(index).Tag.Get("json"), ",")[0]] = true
	}
	for name := range fields {
		if !allowed[name] {
			return fmt.Errorf("unknown JSON field %q", name)
		}
	}
	for name := range allowed {
		if _, exists := fields[name]; !exists {
			return fmt.Errorf("missing JSON field %q", name)
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func validateJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 8 {
		return fmt.Errorf("JSON nesting exceeds 8 levels")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("null values are not supported")
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate or invalid JSON field %q", name)
			}
			seen[name] = true
			if err := validateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}
