package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"guiforcores/bridge/event"
	"guiforcores/bridge/logging"
	"guiforcores/bridge/rpcutil"
	"guiforcores/bridge/storage"
	"guiforcores/bridge/syncstate"
	profilev1 "guiforcores/gen/profile/v1"

	connect "connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

const profilesFilePath = "data/profiles.yaml"

type profileService struct {
	paths         *storage.Paths
	events        *event.Hub
	changeHandler ChangeHandler
	state         *syncstate.Coordinator
	mu            sync.Mutex
}

type Service = profileService

type ChangeHandler interface {
	ProfilesChanged(ids []string)
}

func NewService(paths *storage.Paths, events *event.Hub, coordinators ...*syncstate.Coordinator) *Service {
	state := syncstate.NewCoordinator()
	if len(coordinators) > 0 && coordinators[0] != nil {
		state = coordinators[0]
	}
	return &profileService{paths: paths, events: events, state: state}
}

func (s *profileService) SetChangeHandler(handler ChangeHandler) {
	s.changeHandler = handler
}

func (s *profileService) publish(eventName string, data ...any) {
	if s.events != nil {
		s.events.Publish(eventName, data...)
	}
}

func (s *profileService) notifyChanged(ids ...string) {
	if s.changeHandler != nil && len(ids) > 0 {
		s.changeHandler.ProfilesChanged(ids)
	}
}

func (s *profileService) publishResourceChanged(operation syncstate.Operation, ids []string, state interface {
	GetInstanceId() string
	GetStateRevision() uint64
}) {
	if ids == nil {
		ids = []string{}
	}
	s.publish("resourceChanged", map[string]any{
		"domain":        string(syncstate.DomainProfiles),
		"operation":     string(operation),
		"ids":           ids,
		"instanceId":    state.GetInstanceId(),
		"stateRevision": state.GetStateRevision(),
	})
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

	return connect.NewResponse(&profilev1.ListProfilesResponse{
		Profiles: cloneProfiles(profiles),
		State:    s.state.Snapshot(syncstate.DomainProfiles, profileIDs(profiles)),
	}), nil
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
	ctx context.Context,
	req *connect.Request[profilev1.CreateProfileRequest],
) (response *connect.Response[profilev1.CreateProfileResponse], responseErr error) {
	started := time.Now()
	profile := req.Msg.GetProfile()
	profileID := profile.GetId()
	defer func() {
		logging.Complete(ctx, "profile", "create", "profile created", started, responseErr, "profile_id", profileID, "name", profile.GetName())
	}()
	if profile == nil || profile.GetId() == "" {
		return nil, asConnectError(invalidArgumentError{message: "profile.id is required"})
	}

	s.mu.Lock()

	profiles, err := s.loadProfiles()
	if err != nil {
		s.mu.Unlock()
		return nil, asConnectError(err)
	}

	for _, existing := range profiles {
		if existing != nil && existing.GetId() == profile.GetId() {
			s.mu.Unlock()
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("profile %q already exists", profile.GetId()))
		}
	}

	profiles = append(profiles, cloneProfile(profile))
	if err := s.saveProfiles(profiles); err != nil {
		s.mu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainProfiles, profileIDs(profiles), []string{profile.GetId()}, nil, true, profile.GetId())
	s.mu.Unlock()

	s.publishResourceChanged(syncstate.OperationUpsert, []string{profile.GetId()}, state)
	s.notifyChanged(profile.GetId())
	return connect.NewResponse(&profilev1.CreateProfileResponse{Profile: cloneProfile(profile), State: state}), nil
}

func (s *profileService) UpdateProfile(
	ctx context.Context,
	req *connect.Request[profilev1.UpdateProfileRequest],
) (response *connect.Response[profilev1.UpdateProfileResponse], responseErr error) {
	started := time.Now()
	profile := req.Msg.GetProfile()
	profileID := profile.GetId()
	defer func() {
		logging.Complete(ctx, "profile", "update", "profile updated", started, responseErr, "profile_id", profileID, "name", profile.GetName())
	}()
	if profile == nil || profile.GetId() == "" {
		return nil, asConnectError(invalidArgumentError{message: "profile.id is required"})
	}

	s.mu.Lock()

	profiles, err := s.loadProfiles()
	if err != nil {
		s.mu.Unlock()
		return nil, asConnectError(err)
	}

	updated := false
	changed := false
	var saved *profilev1.Profile
	for idx, existing := range profiles {
		if existing != nil && existing.GetId() == profile.GetId() {
			if err := s.state.CheckItem(syncstate.DomainProfiles, profileIDs(profiles), profile.GetId(), req.Msg.GetExpectedRevision(), true); err != nil {
				s.mu.Unlock()
				return nil, err
			}
			changed = !proto.Equal(existing, profile)
			if changed {
				profiles[idx] = cloneProfile(profile)
			}
			saved = cloneProfile(profiles[idx])
			updated = true
			break
		}
	}
	if !updated {
		s.mu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("profile %q not found", profile.GetId()))
	}

	if !changed {
		state := s.state.Mutation(syncstate.DomainProfiles, profileIDs(profiles), profile.GetId())
		s.mu.Unlock()
		return connect.NewResponse(&profilev1.UpdateProfileResponse{Profile: saved, State: state}), nil
	}
	if err := s.saveProfiles(profiles); err != nil {
		s.mu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainProfiles, profileIDs(profiles), []string{profile.GetId()}, nil, false, profile.GetId())
	s.mu.Unlock()

	s.publishResourceChanged(syncstate.OperationUpsert, []string{profile.GetId()}, state)
	s.notifyChanged(profile.GetId())
	return connect.NewResponse(&profilev1.UpdateProfileResponse{Profile: saved, State: state}), nil
}

func (s *profileService) DeleteProfile(
	ctx context.Context,
	req *connect.Request[profilev1.DeleteProfileRequest],
) (response *connect.Response[profilev1.DeleteProfileResponse], responseErr error) {
	started := time.Now()
	id := req.Msg.GetId()
	defer func() {
		logging.Complete(ctx, "profile", "delete", "profile deleted", started, responseErr, "profile_id", id)
	}()
	if id == "" {
		return nil, asConnectError(invalidArgumentError{message: "id is required"})
	}

	s.mu.Lock()

	profiles, err := s.loadProfiles()
	if err != nil {
		s.mu.Unlock()
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
		s.mu.Unlock()
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("profile %q not found", id))
	}
	if err := s.state.CheckItem(syncstate.DomainProfiles, profileIDs(profiles), id, req.Msg.GetExpectedRevision(), true); err != nil {
		s.mu.Unlock()
		return nil, err
	}

	if err := s.saveProfiles(remaining); err != nil {
		s.mu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainProfiles, profileIDs(remaining), nil, []string{id}, true, id)
	s.mu.Unlock()

	s.publishResourceChanged(syncstate.OperationDelete, []string{id}, state)
	s.notifyChanged(id)
	return connect.NewResponse(&profilev1.DeleteProfileResponse{State: state}), nil
}

func (s *profileService) ReorderProfiles(
	ctx context.Context,
	req *connect.Request[profilev1.ReorderProfilesRequest],
) (response *connect.Response[profilev1.ReorderProfilesResponse], responseErr error) {
	started := time.Now()
	defer func() {
		logging.Complete(ctx, "profile", "reorder", "profiles reordered", started, responseErr, "total", len(req.Msg.GetIds()))
	}()
	s.mu.Lock()
	profiles, err := s.loadProfiles()
	if err != nil {
		s.mu.Unlock()
		return nil, asConnectError(err)
	}
	currentIDs := profileIDs(profiles)
	if err := s.state.CheckOrder(syncstate.DomainProfiles, currentIDs, req.Msg.GetExpectedOrderRevision(), true); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if err := validateOrderIDs(currentIDs, req.Msg.GetIds()); err != nil {
		s.mu.Unlock()
		return nil, asConnectError(err)
	}
	if slicesEqual(currentIDs, req.Msg.GetIds()) {
		state := s.state.Mutation(syncstate.DomainProfiles, currentIDs, "")
		s.mu.Unlock()
		return connect.NewResponse(&profilev1.ReorderProfilesResponse{Ids: currentIDs, State: state}), nil
	}
	byID := make(map[string]*profilev1.Profile, len(profiles))
	for _, profile := range profiles {
		byID[profile.GetId()] = profile
	}
	reordered := make([]*profilev1.Profile, 0, len(profiles))
	for _, id := range req.Msg.GetIds() {
		reordered = append(reordered, byID[id])
	}
	if err := s.saveProfiles(reordered); err != nil {
		s.mu.Unlock()
		return nil, asConnectError(err)
	}
	state := s.state.Advance(syncstate.DomainProfiles, req.Msg.GetIds(), nil, nil, true, "")
	s.mu.Unlock()

	s.publishResourceChanged(syncstate.OperationReorder, nil, state)
	return connect.NewResponse(&profilev1.ReorderProfilesResponse{Ids: append([]string(nil), req.Msg.GetIds()...), State: state}), nil
}

func profileIDs(profiles []*profilev1.Profile) []string {
	ids := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if profile != nil && profile.GetId() != "" {
			ids = append(ids, profile.GetId())
		}
	}
	return ids
}

func validateOrderIDs(current []string, requested []string) error {
	if len(current) != len(requested) {
		return invalidArgumentError{message: "order must contain every profile id exactly once"}
	}
	seen := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		if _, exists := seen[id]; exists {
			return invalidArgumentError{message: fmt.Sprintf("duplicate profile id %q in order", id)}
		}
		seen[id] = struct{}{}
	}
	for _, id := range current {
		if _, exists := seen[id]; !exists {
			return invalidArgumentError{message: "order must contain every profile id exactly once"}
		}
	}
	return nil
}

func slicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
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
	if err := storage.AtomicWriteFile(fullPath, payload, 0644); err != nil {
		return fmt.Errorf("write profiles file: %w", err)
	}
	return nil
}

func normalizeProfilePayloadsForYAML(profiles []*profilev1.Profile) {
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		if dns := profile.GetDns(); dns != nil {
			for _, rule := range dns.GetRules() {
				if rule != nil {
					rule.Raw = normalizeMultilinePayloadForYAML(rule.GetRaw())
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
			"bridge":   "OUTBOUND_TYPE_BRIDGE",
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
		convertEnumString(m, "action", map[string]string{
			"route":         "RULE_ACTION_ROUTE",
			"bypass":        "RULE_ACTION_BYPASS",
			"route-options": "RULE_ACTION_ROUTE_OPTIONS",
			"reject":        "RULE_ACTION_REJECT",
			"hijack-dns":    "RULE_ACTION_HIJACK_DNS",
			"sniff":         "RULE_ACTION_SNIFF",
			"resolve":       "RULE_ACTION_RESOLVE",
			"inline":        "RULE_ACTION_INLINE",
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
		convertEnumString(m, "action", map[string]string{
			"route":         "DNS_RULE_ACTION_ROUTE",
			"evaluate":      "DNS_RULE_ACTION_EVALUATE",
			"respond":       "DNS_RULE_ACTION_RESPOND",
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
