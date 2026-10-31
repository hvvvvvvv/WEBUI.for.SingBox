// Package plugins executes administrator-installed JavaScript hooks with a small
// host API. Time, stack and serialized size limits are not strong memory
// isolation: only install scripts from sources you trust.
package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"

	"guiforcores/bridge/storage"

	"github.com/dop251/goja"
)

const (
	OnSubscribe = "on::subscribe"
	OnGenerate  = "on::generate"
	OnManual    = "on::manual"

	MaxPlugins            = 32
	MaxSourceBytes        = 256 << 10
	MaxConfigurationBytes = 64 << 10
	MaxStorageBytes       = 4 << 20
	MaxIOBytes            = 8 << 20
	MaxProxies            = 10000
)

var (
	ErrConflict = errors.New("plugin revision conflict")
	ErrNotFound = errors.New("plugin not found")
	ErrInvalid  = errors.New("invalid plugin")
	pluginID    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

type Plugin struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	Description         string         `json:"description"`
	Version             string         `json:"version"`
	Triggers            []string       `json:"triggers"`
	Disabled            bool           `json:"disabled"`
	Source              string         `json:"source"`
	Configuration       map[string]any `json:"configuration,omitempty"`
	ConfigurationSchema []Setting      `json:"configuration_schema,omitempty"`
}

// Setting describes an allowlisted form control, never a component or script to load.
type Setting struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Component   string   `json:"component"`
	Value       any      `json:"value"`
	Options     []string `json:"options,omitempty"`
}

type Snapshot struct {
	Plugins  []Plugin `json:"plugins"`
	Revision uint64   `json:"revision"`
}

type Service struct {
	mu       sync.RWMutex
	path     string
	state    Snapshot
	programs map[string]*goja.Program
	runSlot  chan struct{}
}

func NewService(paths *storage.Paths) (*Service, error) {
	if paths == nil {
		return nil, fmt.Errorf("%w: storage paths are required", ErrInvalid)
	}
	s := &Service{
		path: paths.Resolve("data/plugins.json"), state: Snapshot{Plugins: []Plugin{}},
		programs: make(map[string]*goja.Program), runSlot: make(chan struct{}, 1),
	}
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read plugins: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxStorageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read plugins: %w", err)
	}
	if len(data) > MaxStorageBytes {
		return nil, fmt.Errorf("%w: plugin storage exceeds %d bytes", ErrInvalid, MaxStorageBytes)
	}
	var loaded Snapshot
	if err := decodeJSON(data, &loaded, true); err != nil {
		return nil, fmt.Errorf("%w: read plugin storage: %v", ErrInvalid, err)
	}
	s.state = loaded
	if s.state.Plugins == nil || len(s.state.Plugins) > MaxPlugins {
		return nil, fmt.Errorf("%w: plugins must be an array of at most %d entries", ErrInvalid, MaxPlugins)
	}
	for _, plugin := range s.state.Plugins {
		if _, exists := s.programs[plugin.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate plugin id %q", ErrInvalid, plugin.ID)
		}
		program, err := validatePlugin(plugin)
		if err != nil {
			return nil, err
		}
		s.programs[plugin.ID] = program
	}
	return s, nil
}

// ValidatePlugin compiles source without evaluating it or running install hooks.
func ValidatePlugin(plugin Plugin) error {
	_, err := validatePlugin(plugin)
	return err
}

func validatePlugin(plugin Plugin) (*goja.Program, error) {
	if !pluginID.MatchString(plugin.ID) {
		return nil, fmt.Errorf("%w: id must contain 1-128 letters, digits, '.', '_' or '-' and start with a letter or digit", ErrInvalid)
	}
	if strings.TrimSpace(plugin.Name) == "" || len(plugin.Name) > 256 {
		return nil, fmt.Errorf("%w: name must contain 1-256 bytes", ErrInvalid)
	}
	if len(plugin.Description) > 16<<10 {
		return nil, fmt.Errorf("%w: description exceeds 16 KiB", ErrInvalid)
	}
	if strings.TrimSpace(plugin.Version) == "" || len(plugin.Version) > 128 {
		return nil, fmt.Errorf("%w: version must contain 1-128 bytes", ErrInvalid)
	}
	if len(plugin.Triggers) == 0 || len(plugin.Triggers) > 3 {
		return nil, fmt.Errorf("%w: select 1-3 triggers", ErrInvalid)
	}
	seen := make(map[string]bool)
	for _, trigger := range plugin.Triggers {
		if trigger != OnSubscribe && trigger != OnGenerate && trigger != OnManual {
			return nil, fmt.Errorf("%w: unsupported trigger %q", ErrInvalid, trigger)
		}
		if seen[trigger] {
			return nil, fmt.Errorf("%w: duplicate trigger %q", ErrInvalid, trigger)
		}
		seen[trigger] = true
	}
	if strings.TrimSpace(plugin.Source) == "" || len(plugin.Source) > MaxSourceBytes {
		return nil, fmt.Errorf("%w: source must contain 1-%d bytes", ErrInvalid, MaxSourceBytes)
	}
	configuration, err := json.Marshal(plugin.Configuration)
	if err != nil || len(configuration) > MaxConfigurationBytes {
		return nil, fmt.Errorf("%w: configuration must be JSON data of at most %d bytes", ErrInvalid, MaxConfigurationBytes)
	}
	if err := validateSettings(plugin); err != nil {
		return nil, err
	}
	program, err := goja.Compile(plugin.ID+".js", plugin.Source, false)
	if err != nil {
		return nil, fmt.Errorf("%w: plugin %q syntax: %v", ErrInvalid, plugin.ID, err)
	}
	return program, nil
}

func validateSettings(plugin Plugin) error {
	invalid := func() error { return fmt.Errorf("%w: invalid configuration schema or setting value", ErrInvalid) }
	raw, err := json.Marshal(plugin.ConfigurationSchema)
	if err != nil || len(raw) > MaxConfigurationBytes || len(plugin.ConfigurationSchema) > 64 {
		return invalid()
	}
	seen := map[string]bool{}
	for _, setting := range plugin.ConfigurationSchema {
		if !pluginID.MatchString(setting.Key) || seen[setting.Key] || strings.TrimSpace(setting.Title) == "" || len(setting.Title) > 256 || len(setting.Description) > 4096 || len(setting.Options) > 64 {
			return invalid()
		}
		seen[setting.Key] = true
		validate := func(value any) bool {
			switch setting.Component {
			case "Switch":
				_, ok := value.(bool)
				return ok
			case "Input":
				text, ok := value.(string)
				return ok && len(text) <= 16384
			case "Radio":
				text, ok := value.(string)
				if !ok || len(setting.Options) == 0 {
					return false
				}
				for _, option := range setting.Options {
					parts := strings.SplitN(option, ",", 2)
					candidate := parts[0]
					if len(parts) == 2 {
						candidate = parts[1]
					}
					if candidate == text {
						return true
					}
				}
			}
			return false
		}
		for _, option := range setting.Options {
			if len(option) == 0 || len(option) > 512 {
				return invalid()
			}
		}
		if !validate(setting.Value) {
			return invalid()
		}
		if value, exists := plugin.Configuration[setting.Key]; exists && !validate(value) {
			return invalid()
		}
	}
	return nil
}

func (s *Service) List() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.state)
}

func (s *Service) Save(plugin Plugin, expectedRevision uint64) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision != s.state.Revision {
		return Snapshot{}, ErrConflict
	}
	program, err := validatePlugin(plugin)
	if err != nil {
		return Snapshot{}, err
	}
	// Canonicalize caller-owned configuration before retaining it. This also
	// removes Go-specific values from the service's public JSON model.
	data, err := json.Marshal(plugin)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var canonical Plugin
	if err := decodeJSON(data, &canonical, false); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	plugin = canonical
	next := cloneSnapshot(s.state)
	index := -1
	for i := range next.Plugins {
		if next.Plugins[i].ID == plugin.ID {
			index = i
			break
		}
	}
	if index < 0 {
		if len(next.Plugins) >= MaxPlugins {
			return Snapshot{}, fmt.Errorf("%w: at most %d plugins may be installed", ErrInvalid, MaxPlugins)
		}
		next.Plugins = append(next.Plugins, plugin)
	} else {
		next.Plugins[index] = plugin
	}
	if err := s.persist(next); err != nil {
		return Snapshot{}, err
	}
	s.programs[plugin.ID] = program
	return cloneSnapshot(s.state), nil
}

func (s *Service) Delete(id string, expectedRevision uint64) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision != s.state.Revision {
		return Snapshot{}, ErrConflict
	}
	next := cloneSnapshot(s.state)
	for index, plugin := range next.Plugins {
		if plugin.ID == id {
			next.Plugins = append(next.Plugins[:index], next.Plugins[index+1:]...)
			if err := s.persist(next); err != nil {
				return Snapshot{}, err
			}
			delete(s.programs, id)
			return cloneSnapshot(s.state), nil
		}
	}
	return Snapshot{}, ErrNotFound
}

func (s *Service) Reorder(ids []string, expectedRevision uint64) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision != s.state.Revision {
		return Snapshot{}, ErrConflict
	}
	if len(ids) != len(s.state.Plugins) {
		return Snapshot{}, fmt.Errorf("%w: reorder must include every installed plugin exactly once", ErrInvalid)
	}
	byID := make(map[string]Plugin, len(s.state.Plugins))
	for _, plugin := range s.state.Plugins {
		byID[plugin.ID] = plugin
	}
	next := Snapshot{Plugins: make([]Plugin, 0, len(ids))}
	for _, id := range ids {
		plugin, exists := byID[id]
		if !exists {
			return Snapshot{}, fmt.Errorf("%w: reorder contains duplicate or unknown id %q", ErrInvalid, id)
		}
		next.Plugins = append(next.Plugins, plugin)
		delete(byID, id)
	}
	if err := s.persist(next); err != nil {
		return Snapshot{}, err
	}
	return cloneSnapshot(s.state), nil
}

// The caller holds mu. Publish the new in-memory state only after durable write.
func (s *Service) persist(next Snapshot) error {
	if s.state.Revision == math.MaxUint64 {
		return fmt.Errorf("%w: revision exhausted", ErrInvalid)
	}
	next.Revision = s.state.Revision + 1
	data, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("%w: serialize plugins: %v", ErrInvalid, err)
	}
	if len(data) > MaxStorageBytes {
		return fmt.Errorf("%w: plugin storage exceeds %d bytes", ErrInvalid, MaxStorageBytes)
	}
	if err := storage.AtomicWriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("save plugins: %w", err)
	}
	s.state = next
	return nil
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	result := Snapshot{Revision: snapshot.Revision, Plugins: make([]Plugin, len(snapshot.Plugins))}
	for index, plugin := range snapshot.Plugins {
		plugin.Triggers = append([]string{}, plugin.Triggers...)
		if plugin.ConfigurationSchema != nil {
			plugin.ConfigurationSchema = append([]Setting{}, plugin.ConfigurationSchema...)
			for i := range plugin.ConfigurationSchema {
				setting := &plugin.ConfigurationSchema[i]
				setting.Options = append([]string(nil), setting.Options...)
				setting.Value = cloneValue(setting.Value)
			}
		}
		if plugin.Configuration != nil {
			plugin.Configuration = cloneValue(plugin.Configuration).(map[string]any)
		}
		result.Plugins[index] = plugin
	}
	return result
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(value))
		for key, item := range value {
			clone[key] = cloneValue(item)
		}
		return clone
	case []any:
		clone := make([]any, len(value))
		for index, item := range value {
			clone[index] = cloneValue(item)
		}
		return clone
	default:
		return value
	}
}

func decodeJSON(data []byte, target any, strict bool) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("expected a single JSON value")
	}
	return nil
}
