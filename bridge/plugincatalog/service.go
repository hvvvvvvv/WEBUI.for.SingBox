package plugincatalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"guiforcores/bridge/network"
	"guiforcores/bridge/plugins"
	"guiforcores/bridge/storage"
	plugindata "guiforcores/plugin-catalog"
)

const Repository = "Ghostdehole/WEBUI.for.SingBox-Plugins"
const APIVersion = "webui.plugins/v1"
const maxCatalogBytes = 4 << 20
const maxScriptBytes = 256 << 10

var scriptPath = regexp.MustCompile(`^plugins/[a-z0-9][a-z0-9._-]{0,100}[.]js$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Entry struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	Version             string            `json:"version"`
	Triggers            []string          `json:"triggers"`
	Configuration       map[string]any    `json:"configuration"`
	ConfigurationSchema []plugins.Setting `json:"configuration_schema,omitempty"`
	Path                string            `json:"path"`
	SHA256              string            `json:"sha256"`
}

type Index struct {
	APIVersion string  `json:"apiVersion"`
	Plugins    []Entry `json:"plugins"`
}

type Result struct {
	Plugins    []plugins.Plugin `json:"plugins"`
	Source     string           `json:"source"`
	Repository string           `json:"repository"`
	Warning    string           `json:"warning,omitempty"`
}

type Service struct {
	gate           chan struct{}
	cachePath      string
	baseURL        string
	client         *http.Client
	items          []plugins.Plugin
	lastAttempt    time.Time
	warning        string
	networkOptions func() network.Options
}

// SetNetworkOptions is configured before the HTTP server starts.
func (s *Service) SetNetworkOptions(provider func() network.Options) { s.networkOptions = provider }

func NewService(cachePath string) *Service {
	return &Service{
		gate: make(chan struct{}, 1), cachePath: cachePath,
		baseURL: "https://raw.githubusercontent.com/" + Repository + "/main/",
		client:  &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return fmt.Errorf("catalog redirects are not allowed") }},
	}
}

func Bundled() ([]plugins.Plugin, error) { return Read(plugindata.Files.ReadFile) }

// Read checks the index and script digests before exposing any new catalog.
// It compiles scripts but never evaluates or executes them.
func Read(read func(string) ([]byte, error)) ([]plugins.Plugin, error) {
	data, err := read("index.json")
	if err != nil {
		return nil, err
	}
	if len(data) > 256<<10 {
		return nil, fmt.Errorf("plugin index exceeds 256 KiB")
	}
	var index Index
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&index); err != nil {
		return nil, fmt.Errorf("invalid plugin index: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF || index.APIVersion != APIVersion || len(index.Plugins) == 0 || len(index.Plugins) > 32 {
		return nil, fmt.Errorf("unsupported or invalid plugin index")
	}
	seenIDs, seenPaths := map[string]bool{}, map[string]bool{}
	result := make([]plugins.Plugin, 0, len(index.Plugins))
	total := 0
	for _, entry := range index.Plugins {
		if !scriptPath.MatchString(entry.Path) || strings.Contains(entry.Path, "..") || !digestPattern.MatchString(entry.SHA256) || seenPaths[entry.Path] || seenIDs[entry.ID] {
			return nil, fmt.Errorf("invalid or duplicate plugin path, id or digest")
		}
		seenIDs[entry.ID], seenPaths[entry.Path] = true, true
		source, err := read(entry.Path)
		if err != nil {
			return nil, err
		}
		total += len(source)
		if len(source) > maxScriptBytes || total > maxCatalogBytes {
			return nil, fmt.Errorf("plugin catalog exceeds size limits")
		}
		digest := sha256.Sum256(source)
		if hex.EncodeToString(digest[:]) != entry.SHA256 {
			return nil, fmt.Errorf("checksum mismatch for %s", entry.Path)
		}
		item := plugins.Plugin{ID: entry.ID, Name: entry.Name, Description: entry.Description, Version: entry.Version, Triggers: entry.Triggers, Configuration: entry.Configuration, ConfigurationSchema: entry.ConfigurationSchema, Source: string(source)}
		if err := plugins.ValidatePlugin(item); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.ID, err)
		}
		result = append(result, item)
	}
	if raw, err := json.Marshal(result); err != nil || len(raw) > maxCatalogBytes {
		return nil, fmt.Errorf("plugin catalog exceeds serialized size limit")
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, refresh bool) (Result, error) {
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result := Result{Source: "bundled", Repository: Repository}
	if s.items == nil && s.cachePath != "" {
		if file, err := os.Open(s.cachePath); err == nil {
			raw, readErr := io.ReadAll(io.LimitReader(file, maxCatalogBytes+1))
			_ = file.Close()
			var items []plugins.Plugin
			if readErr == nil && len(raw) <= maxCatalogBytes && json.Unmarshal(raw, &items) == nil && validateItems(items) == nil {
				s.items = items
			}
		}
	}
	if refresh && (s.lastAttempt.IsZero() || time.Since(s.lastAttempt) >= 5*time.Second) {
		s.warning = ""
		fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		items, err := Read(func(path string) ([]byte, error) { return s.download(fetchCtx, path) })
		cancel()
		s.lastAttempt = time.Now()
		if err == nil {
			s.items = items
			result.Source = "remote"
			if s.cachePath != "" {
				raw, _ := json.Marshal(items)
				if err := storage.AtomicWriteFile(s.cachePath, raw, 0600); err != nil {
					s.warning = "Catalog downloaded, but the offline cache could not be saved."
				}
			}
		} else {
			s.warning = "Update unavailable; keeping the previous plugin catalog. " + err.Error()
		}
	}
	if s.items != nil {
		result.Plugins = cloneItems(s.items)
		if result.Source != "remote" {
			result.Source = "cache"
		}
	} else {
		var err error
		result.Plugins, err = Bundled()
		if err != nil {
			return result, err
		}
	}
	result.Warning = s.warning
	return result, nil
}

func cloneItems(items []plugins.Plugin) []plugins.Plugin {
	raw, _ := json.Marshal(items)
	var result []plugins.Plugin
	_ = json.Unmarshal(raw, &result)
	return result
}

func validateItems(items []plugins.Plugin) error {
	if len(items) == 0 || len(items) > 32 {
		return fmt.Errorf("invalid plugin count")
	}
	seen := map[string]bool{}
	for _, item := range items {
		if seen[item.ID] {
			return fmt.Errorf("duplicate plugin id")
		}
		seen[item.ID] = true
		if err := plugins.ValidatePlugin(item); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) download(ctx context.Context, path string) ([]byte, error) {
	client := s.client
	options := network.Options{UserAgent: "WEBUI.for.SingBox-Plugin-Hub"}
	if s.networkOptions != nil {
		options = s.networkOptions()
		if options.UserAgent == "" {
			options.UserAgent = "WEBUI.for.SingBox-Plugin-Hub"
		}
		var err error
		client, err = network.NewClient(options, 15*time.Second, false)
		if err != nil {
			return nil, fmt.Errorf("plugin request policy is unavailable: %w", err)
		}
		defer client.CloseIdleConnections()
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return fmt.Errorf("catalog redirects are not allowed") }
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if err := network.PrepareRequest(req, options, false); err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plugin source could not be reached")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plugin source returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxScriptBytes+1))
	if err != nil || len(data) > maxScriptBytes {
		return nil, fmt.Errorf("plugin response is incomplete or too large")
	}
	return data, nil
}
