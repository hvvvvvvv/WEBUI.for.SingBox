package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"guiforcores/bridge/event"
	"guiforcores/bridge/rpcutil"
	"guiforcores/bridge/storage"
	profilev1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

const profilesFilePath = "data/profiles.yaml"

type profileService struct {
	paths  *storage.Paths
	events *event.Hub
	mu     sync.Mutex
}

type Service = profileService

func NewService(paths *storage.Paths, events *event.Hub) *Service {
	return &profileService{paths: paths, events: events}
}

func (s *profileService) publish(eventName string, data ...any) {
	if s.events != nil {
		s.events.Publish(eventName, data...)
	}
}

type invalidArgumentError struct {
	message string
}

func (e invalidArgumentError) Error() string {
	return e.message
}

func asConnectError(err error) error {
	if invalid, ok := err.(invalidArgumentError); ok {
		return rpcutil.AsConnectError(rpcutil.InvalidArgumentError{Message: invalid.message})
	}
	return rpcutil.AsConnectError(err)
}

func (s *profileService) ListProfiles(
	_ context.Context,
	_ *connect.Request[profilev1.ListProfilesRequest],
) (*connect.Response[profilev1.ListProfilesResponse], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	profiles, err := s.loadProfiles()
	if err != nil {
		return nil, asConnectError(err)
	}

	return connect.NewResponse(&profilev1.ListProfilesResponse{Profiles: cloneProfiles(profiles)}), nil
}

func (s *profileService) GetProfile(
	_ context.Context,
	req *connect.Request[profilev1.GetProfileRequest],
) (*connect.Response[profilev1.GetProfileResponse], error) {
	if req.Msg.GetId() == "" {
		return nil, asConnectError(invalidArgumentError{message: "id is required"})
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	profiles, err := s.loadProfiles()
	if err != nil {
		return nil, asConnectError(err)
	}

	for _, profile := range profiles {
		if profile != nil && profile.GetId() == req.Msg.GetId() {
			return connect.NewResponse(&profilev1.GetProfileResponse{Profile: cloneProfile(profile)}), nil
		}
	}

	return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("profile %q not found", req.Msg.GetId()))
}

func (s *profileService) CreateProfile(
	_ context.Context,
	req *connect.Request[profilev1.CreateProfileRequest],
) (*connect.Response[profilev1.CreateProfileResponse], error) {
	profile := req.Msg.GetProfile()
	if profile == nil || profile.GetId() == "" {
		return nil, asConnectError(invalidArgumentError{message: "profile.id is required"})
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	profiles, err := s.loadProfiles()
	if err != nil {
		return nil, asConnectError(err)
	}

	for _, existing := range profiles {
		if existing != nil && existing.GetId() == profile.GetId() {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("profile %q already exists", profile.GetId()))
		}
	}

	profiles = append(profiles, cloneProfile(profile))
	if err := s.saveProfiles(profiles); err != nil {
		return nil, asConnectError(err)
	}

	s.publish("profileChange", map[string]any{"id": profile.GetId()})
	return connect.NewResponse(&profilev1.CreateProfileResponse{Profile: cloneProfile(profile)}), nil
}

func (s *profileService) UpdateProfile(
	_ context.Context,
	req *connect.Request[profilev1.UpdateProfileRequest],
) (*connect.Response[profilev1.UpdateProfileResponse], error) {
	profile := req.Msg.GetProfile()
	if profile == nil || profile.GetId() == "" {
		return nil, asConnectError(invalidArgumentError{message: "profile.id is required"})
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	profiles, err := s.loadProfiles()
	if err != nil {
		return nil, asConnectError(err)
	}

	updated := false
	for idx, existing := range profiles {
		if existing != nil && existing.GetId() == profile.GetId() {
			profiles[idx] = cloneProfile(profile)
			updated = true
			break
		}
	}
	if !updated {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("profile %q not found", profile.GetId()))
	}

	if err := s.saveProfiles(profiles); err != nil {
		return nil, asConnectError(err)
	}

	s.publish("profileChange", map[string]any{"id": profile.GetId()})
	return connect.NewResponse(&profilev1.UpdateProfileResponse{Profile: cloneProfile(profile)}), nil
}

func (s *profileService) DeleteProfile(
	_ context.Context,
	req *connect.Request[profilev1.DeleteProfileRequest],
) (*connect.Response[profilev1.DeleteProfileResponse], error) {
	id := req.Msg.GetId()
	if id == "" {
		return nil, asConnectError(invalidArgumentError{message: "id is required"})
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	profiles, err := s.loadProfiles()
	if err != nil {
		return nil, asConnectError(err)
	}

	removed := false
	remaining := make([]*profilev1.Profile, 0, len(profiles))
	for _, profile := range profiles {
		if profile != nil && profile.GetId() == id {
			removed = true
			continue
		}
		remaining = append(remaining, profile)
	}
	if !removed {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("profile %q not found", id))
	}

	if err := s.saveProfiles(remaining); err != nil {
		return nil, asConnectError(err)
	}

	s.publish("profileChange", map[string]any{"id": id})
	return connect.NewResponse(&profilev1.DeleteProfileResponse{}), nil
}

func (s *profileService) SaveProfiles(
	_ context.Context,
	req *connect.Request[profilev1.SaveProfilesRequest],
) (*connect.Response[profilev1.SaveProfilesResponse], error) {
	profiles := cloneProfiles(req.Msg.GetProfiles())
	if err := validateProfilesForSave(profiles); err != nil {
		return nil, asConnectError(err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.saveProfiles(profiles); err != nil {
		return nil, asConnectError(err)
	}

	s.publish("profileChange", map[string]any{"id": ""})
	return connect.NewResponse(&profilev1.SaveProfilesResponse{Profiles: cloneProfiles(profiles)}), nil
}

func (s *profileService) loadProfiles() ([]*profilev1.Profile, error) {
	filePath := s.paths.Resolve(profilesFilePath)
	bytes, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*profilev1.Profile{}, nil
		}
		return nil, fmt.Errorf("read profiles file: %w", err)
	}
	if len(bytes) == 0 {
		return []*profilev1.Profile{}, nil
	}

	var profiles []*profilev1.Profile
	if err := yaml.Unmarshal(bytes, &profiles); err != nil {
		migrated, migrateErr := migrateLegacyProfilesYAML(bytes)
		if migrateErr != nil {
			return nil, invalidArgumentError{message: "profiles.yaml format is incompatible with protobuf enum values; auto migration failed: " + migrateErr.Error()}
		}
		// Persist migrated data immediately so subsequent reads are stable.
		if saveErr := s.saveProfiles(migrated); saveErr != nil {
			return nil, fmt.Errorf("save migrated profiles: %w", saveErr)
		}
		return migrated, nil
	}
	return profiles, nil
}

func (s *profileService) Load() ([]*profilev1.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profiles, err := s.loadProfiles()
	if err != nil {
		return nil, err
	}
	return cloneProfiles(profiles), nil
}

func (s *profileService) FindByID(id string) (*profilev1.Profile, error) {
	profiles, err := s.Load()
	if err != nil {
		return nil, err
	}
	for _, item := range profiles {
		if item != nil && item.GetId() == id {
			return cloneProfile(item), nil
		}
	}
	return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("profile %q not found", id))
}

func (s *profileService) saveProfiles(profiles []*profilev1.Profile) error {
	fullPath := s.paths.Resolve(profilesFilePath)
	if err := os.MkdirAll(s.paths.Resolve("data"), os.ModePerm); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	profilesForYAML := cloneProfiles(profiles)
	normalizeProfilePayloadsForYAML(profilesForYAML)

	payload, err := yaml.Marshal(profilesForYAML)
	if err != nil {
		return fmt.Errorf("marshal profiles: %w", err)
	}
	if err := os.WriteFile(fullPath, payload, 0644); err != nil {
		return fmt.Errorf("write profiles file: %w", err)
	}
	return nil
}

func validateProfilesForSave(profiles []*profilev1.Profile) error {
	seen := make(map[string]struct{}, len(profiles))
	for idx, profile := range profiles {
		if profile == nil || profile.GetId() == "" {
			return invalidArgumentError{message: fmt.Sprintf("profiles[%d].id is required", idx)}
		}
		id := profile.GetId()
		if _, ok := seen[id]; ok {
			return invalidArgumentError{message: fmt.Sprintf("duplicate profile id %q", id)}
		}
		seen[id] = struct{}{}
	}
	return nil
}

func normalizeProfilePayloadsForYAML(profiles []*profilev1.Profile) {
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		if route := profile.GetRoute(); route != nil {
			for _, rule := range route.GetRules() {
				if rule != nil {
					rule.Payload = normalizeMultilinePayloadForYAML(rule.GetPayload())
				}
			}
		}
		if dns := profile.GetDns(); dns != nil {
			for _, rule := range dns.GetRules() {
				if rule != nil {
					rule.Payload = normalizeMultilinePayloadForYAML(rule.GetPayload())
				}
			}
		}
	}
}

func normalizeMultilinePayloadForYAML(payload string) string {
	if !strings.ContainsAny(payload, "\r\n") {
		return payload
	}
	return strings.TrimLeft(payload, " \t\r\n")
}

func cloneProfiles(input []*profilev1.Profile) []*profilev1.Profile {
	output := make([]*profilev1.Profile, 0, len(input))
	for _, item := range input {
		output = append(output, cloneProfile(item))
	}
	return output
}

func cloneProfile(profile *profilev1.Profile) *profilev1.Profile {
	if profile == nil {
		return nil
	}
	return proto.Clone(profile).(*profilev1.Profile)
}

func migrateLegacyProfilesYAML(raw []byte) ([]*profilev1.Profile, error) {
	var entries []map[string]any
	if err := yaml.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse legacy yaml: %w", err)
	}

	result := make([]*profilev1.Profile, 0, len(entries))
	for _, entry := range entries {
		normalized := normalizeLegacyProfileMap(entry)
		jsonBytes, err := json.Marshal(normalized)
		if err != nil {
			return nil, fmt.Errorf("marshal normalized profile json: %w", err)
		}

		profile := &profilev1.Profile{}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(jsonBytes, profile); err != nil {
			return nil, fmt.Errorf("unmarshal migrated profile to protobuf: %w", err)
		}
		result = append(result, profile)
	}

	return result, nil
}

func normalizeLegacyProfileMap(root map[string]any) map[string]any {
	normalized := normalizeValue(root)
	profile, ok := normalized.(map[string]any)
	if !ok {
		return map[string]any{}
	}

	pruneLegacyAliasKeys(profile)

	convertEnumField(profile, "log", "level", map[string]string{
		"trace": "LOG_LEVEL_TRACE",
		"debug": "LOG_LEVEL_DEBUG",
		"info":  "LOG_LEVEL_INFO",
		"warn":  "LOG_LEVEL_WARN",
		"error": "LOG_LEVEL_ERROR",
		"fatal": "LOG_LEVEL_FATAL",
		"panic": "LOG_LEVEL_PANIC",
	})

	convertInbounds(profile)
	convertOutbounds(profile)
	convertRoute(profile)
	convertDNS(profile)
	convertMixin(profile)

	return profile
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = normalizeValue(item)
		}
		return result
	case map[any]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[fmt.Sprint(key)] = normalizeValue(item)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, normalizeValue(item))
		}
		return result
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, normalizeValue(item))
		}
		return result
	default:
		return value
	}
}

func pruneLegacyAliasKeys(node map[string]any) {
	canonicalByCompact := map[string]string{}

	for key := range node {
		if strings.Contains(key, "_") || strings.Contains(key, "-") {
			canonicalByCompact[compactLegacyKey(key)] = key
		}
	}

	for key := range node {
		compact := compactLegacyKey(key)
		if canonical, ok := canonicalByCompact[compact]; ok && key != canonical {
			delete(node, key)
			continue
		}

		switch typed := node[key].(type) {
		case map[string]any:
			pruneLegacyAliasKeys(typed)
		case []any:
			for _, item := range typed {
				child, ok := item.(map[string]any)
				if ok {
					pruneLegacyAliasKeys(child)
				}
			}
		}
	}
}

func compactLegacyKey(key string) string {
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	return strings.ToLower(key)
}

func convertInbounds(profile map[string]any) {
	inbounds, ok := profile["inbounds"].([]any)
	if !ok {
		return
	}
	for _, item := range inbounds {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		convertEnumString(m, "type", map[string]string{
			"mixed":  "INBOUND_TYPE_MIXED",
			"socks":  "INBOUND_TYPE_SOCKS",
			"http":   "INBOUND_TYPE_HTTP",
			"tun":    "INBOUND_TYPE_TUN",
			"direct": "INBOUND_TYPE_DIRECT",
		})
		if tun, ok := m["tun"].(map[string]any); ok {
			convertEnumString(tun, "stack", map[string]string{
				"system": "TUN_STACK_SYSTEM",
				"gvisor": "TUN_STACK_GVISOR",
				"mixed":  "TUN_STACK_MIXED",
			})
		}
		if direct, ok := m["direct"].(map[string]any); ok {
			convertEnumString(direct, "network", map[string]string{
				"tcp": "INBOUND_NETWORK_TCP",
				"udp": "INBOUND_NETWORK_UDP",
			})
		}
	}
}

func convertOutbounds(profile map[string]any) {
	outbounds, ok := profile["outbounds"].([]any)
	if !ok {
		return
	}
	for _, item := range outbounds {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		convertEnumString(m, "type", map[string]string{
			"direct":   "OUTBOUND_TYPE_DIRECT",
			"block":    "OUTBOUND_TYPE_BLOCK",
			"selector": "OUTBOUND_TYPE_SELECTOR",
			"urltest":  "OUTBOUND_TYPE_URLTEST",
		})
	}
}

func convertRoute(profile map[string]any) {
	route, ok := profile["route"].(map[string]any)
	if !ok {
		return
	}

	rules, _ := route["rules"].([]any)
	for _, item := range rules {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		convertEnumString(m, "type", map[string]string{
			"inbound":            "RULE_TYPE_INBOUND",
			"network":            "RULE_TYPE_NETWORK",
			"protocol":           "RULE_TYPE_PROTOCOL",
			"domain":             "RULE_TYPE_DOMAIN",
			"domain_suffix":      "RULE_TYPE_DOMAIN_SUFFIX",
			"domain_keyword":     "RULE_TYPE_DOMAIN_KEYWORD",
			"domain_regex":       "RULE_TYPE_DOMAIN_REGEX",
			"source_ip_cidr":     "RULE_TYPE_SOURCE_IP_CIDR",
			"ip_cidr":            "RULE_TYPE_IP_CIDR",
			"ip_is_private":      "RULE_TYPE_IP_IS_PRIVATE",
			"source_port":        "RULE_TYPE_SOURCE_PORT",
			"source_port_range":  "RULE_TYPE_SOURCE_PORT_RANGE",
			"port":               "RULE_TYPE_PORT",
			"port_range":         "RULE_TYPE_PORT_RANGE",
			"process_name":       "RULE_TYPE_PROCESS_NAME",
			"process_path":       "RULE_TYPE_PROCESS_PATH",
			"process_path_regex": "RULE_TYPE_PROCESS_PATH_REGEX",
			"clash_mode":         "RULE_TYPE_CLASH_MODE",
			"rule_set":           "RULE_TYPE_RULE_SET",
			"ip_accept_any":      "RULE_TYPE_IP_ACCEPT_ANY",
			"inline":             "RULE_TYPE_INLINE",
			"InsertionPoint":     "RULE_TYPE_INSERTION_POINT",
		})
		convertEnumString(m, "action", map[string]string{
			"route":         "RULE_ACTION_ROUTE",
			"route-options": "RULE_ACTION_ROUTE_OPTIONS",
			"reject":        "RULE_ACTION_REJECT",
			"hijack-dns":    "RULE_ACTION_HIJACK_DNS",
			"sniff":         "RULE_ACTION_SNIFF",
			"resolve":       "RULE_ACTION_RESOLVE",
			"inline":        "RULE_ACTION_INLINE",
		})
		convertEnumString(m, "strategy", map[string]string{
			"default":     "STRATEGY_DEFAULT",
			"prefer_ipv4": "STRATEGY_PREFER_IPV4",
			"prefer_ipv6": "STRATEGY_PREFER_IPV6",
			"ipv4_only":   "STRATEGY_IPV4_ONLY",
			"ipv6_only":   "STRATEGY_IPV6_ONLY",
		})
	}

	ruleSet, _ := route["rule_set"].([]any)
	for _, item := range ruleSet {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		convertEnumString(m, "type", map[string]string{
			"inline": "RULESET_TYPE_INLINE",
			"local":  "RULESET_TYPE_LOCAL",
			"remote": "RULESET_TYPE_REMOTE",
		})
		convertEnumString(m, "format", map[string]string{
			"source": "RULESET_FORMAT_SOURCE",
			"binary": "RULESET_FORMAT_BINARY",
		})
	}
}

func convertDNS(profile map[string]any) {
	dns, ok := profile["dns"].(map[string]any)
	if !ok {
		return
	}

	convertEnumString(dns, "strategy", map[string]string{
		"default":     "STRATEGY_DEFAULT",
		"prefer_ipv4": "STRATEGY_PREFER_IPV4",
		"prefer_ipv6": "STRATEGY_PREFER_IPV6",
		"ipv4_only":   "STRATEGY_IPV4_ONLY",
		"ipv6_only":   "STRATEGY_IPV6_ONLY",
	})

	servers, _ := dns["servers"].([]any)
	for _, item := range servers {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		convertEnumString(m, "type", map[string]string{
			"local":     "DNS_SERVER_TYPE_LOCAL",
			"hosts":     "DNS_SERVER_TYPE_HOSTS",
			"tcp":       "DNS_SERVER_TYPE_TCP",
			"udp":       "DNS_SERVER_TYPE_UDP",
			"tls":       "DNS_SERVER_TYPE_TLS",
			"https":     "DNS_SERVER_TYPE_HTTPS",
			"quic":      "DNS_SERVER_TYPE_QUIC",
			"h3":        "DNS_SERVER_TYPE_H3",
			"dhcp":      "DNS_SERVER_TYPE_DHCP",
			"fakeip":    "DNS_SERVER_TYPE_FAKEIP",
			"tailscale": "DNS_SERVER_TYPE_TAILSCALE",
		})
	}

	rules, _ := dns["rules"].([]any)
	for _, item := range rules {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		convertEnumString(m, "type", map[string]string{
			"inbound":            "RULE_TYPE_INBOUND",
			"network":            "RULE_TYPE_NETWORK",
			"protocol":           "RULE_TYPE_PROTOCOL",
			"domain":             "RULE_TYPE_DOMAIN",
			"domain_suffix":      "RULE_TYPE_DOMAIN_SUFFIX",
			"domain_keyword":     "RULE_TYPE_DOMAIN_KEYWORD",
			"domain_regex":       "RULE_TYPE_DOMAIN_REGEX",
			"source_ip_cidr":     "RULE_TYPE_SOURCE_IP_CIDR",
			"ip_cidr":            "RULE_TYPE_IP_CIDR",
			"ip_is_private":      "RULE_TYPE_IP_IS_PRIVATE",
			"source_port":        "RULE_TYPE_SOURCE_PORT",
			"source_port_range":  "RULE_TYPE_SOURCE_PORT_RANGE",
			"port":               "RULE_TYPE_PORT",
			"port_range":         "RULE_TYPE_PORT_RANGE",
			"process_name":       "RULE_TYPE_PROCESS_NAME",
			"process_path":       "RULE_TYPE_PROCESS_PATH",
			"process_path_regex": "RULE_TYPE_PROCESS_PATH_REGEX",
			"clash_mode":         "RULE_TYPE_CLASH_MODE",
			"rule_set":           "RULE_TYPE_RULE_SET",
			"ip_accept_any":      "RULE_TYPE_IP_ACCEPT_ANY",
			"inline":             "RULE_TYPE_INLINE",
			"InsertionPoint":     "RULE_TYPE_INSERTION_POINT",
		})
		convertEnumString(m, "action", map[string]string{
			"route":         "DNS_RULE_ACTION_ROUTE",
			"route-options": "DNS_RULE_ACTION_ROUTE_OPTIONS",
			"reject":        "DNS_RULE_ACTION_REJECT",
			"predefined":    "DNS_RULE_ACTION_PREDEFINED",
			"inline":        "DNS_RULE_ACTION_INLINE",
		})
	}
}

func convertMixin(profile map[string]any) {
	mixin, ok := profile["mixin"].(map[string]any)
	if !ok {
		return
	}
	convertEnumString(mixin, "priority", map[string]string{
		"mixin": "MIXIN_PRIORITY_MIXIN",
		"gui":   "MIXIN_PRIORITY_GUI",
	})
	convertEnumString(mixin, "format", map[string]string{
		"json": "MIXIN_FORMAT_JSON",
		"yaml": "MIXIN_FORMAT_YAML",
	})
}

func convertEnumField(root map[string]any, parentKey, key string, mapping map[string]string) {
	parent, ok := root[parentKey].(map[string]any)
	if !ok {
		return
	}
	convertEnumString(parent, key, mapping)
}

func convertEnumString(node map[string]any, key string, mapping map[string]string) {
	raw, ok := node[key]
	if !ok {
		return
	}
	text, ok := raw.(string)
	if !ok {
		return
	}
	if text == "" {
		return
	}
	if strings.HasPrefix(text, "LOG_LEVEL_") || strings.HasPrefix(text, "INBOUND_TYPE_") ||
		strings.HasPrefix(text, "INBOUND_NETWORK_") || strings.HasPrefix(text, "OUTBOUND_TYPE_") ||
		strings.HasPrefix(text, "TUN_STACK_") ||
		strings.HasPrefix(text, "RULESET_TYPE_") || strings.HasPrefix(text, "RULESET_FORMAT_") ||
		strings.HasPrefix(text, "RULE_TYPE_") || strings.HasPrefix(text, "STRATEGY_") ||
		strings.HasPrefix(text, "DNS_SERVER_TYPE_") || strings.HasPrefix(text, "RULE_ACTION_") ||
		strings.HasPrefix(text, "DNS_RULE_ACTION_") || strings.HasPrefix(text, "MIXIN_PRIORITY_") ||
		strings.HasPrefix(text, "MIXIN_FORMAT_") {
		return
	}
	if converted, ok := mapping[text]; ok {
		node[key] = converted
	}
}
