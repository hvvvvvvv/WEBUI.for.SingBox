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
)

const Repository = "Ghostdehole/WEBUI.for.SingBox-Plugins"
const APIVersion = "webui.plugins/v1"
const maxCatalogBytes = 4 << 20
const maxScriptBytes = 256 << 10
const maxIndexBytes = 256 << 10

var scriptPath = regexp.MustCompile(`^plugins/[a-z0-9][a-z0-9._-]{0,100}[.]js$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Entry struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	Version             string            `json:"version"`
	Triggers            []string          `json:"triggers"`
	ManualTarget        string            `json:"manual_target,omitempty"`
	Conflicts           []string          `json:"conflicts,omitempty"`
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
	Plugins    []Entry `json:"plugins"`
	Source     string  `json:"source"`
	Repository string  `json:"repository"`
	Warning    string  `json:"warning,omitempty"`
}

type Service struct {
	gate           chan struct{}
	cachePath      string
	baseURL        string
	client         *http.Client
	index          *Index
	lastAttempt    time.Time
	warning        string
	loadedCache    bool
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

// ReadIndex only reads and validates metadata. Browsing a catalog must never
// request plugin source or compile installation code.
func ReadIndex(read func(string) ([]byte, error)) (Index, error) {
	data, err := read("index.json")
	if err != nil {
		return Index{}, err
	}
	return parseIndex(data)
}

func parseIndex(data []byte) (Index, error) {
	if len(data) > maxIndexBytes {
		return Index{}, fmt.Errorf("plugin index exceeds 256 KiB")
	}
	var index Index
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&index); err != nil {
		return Index{}, fmt.Errorf("invalid plugin index: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF || index.APIVersion != APIVersion || len(index.Plugins) == 0 || len(index.Plugins) > 32 {
		return Index{}, fmt.Errorf("unsupported or invalid plugin index")
	}
	seenIDs, seenPaths := map[string]bool{}, map[string]bool{}
	for _, entry := range index.Plugins {
		if !scriptPath.MatchString(entry.Path) || strings.Contains(entry.Path, "..") || !digestPattern.MatchString(entry.SHA256) || seenPaths[entry.Path] || seenIDs[entry.ID] {
			return Index{}, fmt.Errorf("invalid or duplicate plugin path, id or digest")
		}
		seenIDs[entry.ID], seenPaths[entry.Path] = true, true
		if err := plugins.ValidateMetadata(entry.plugin("")); err != nil {
			return Index{}, fmt.Errorf("%s: %w", entry.ID, err)
		}
	}
	if raw, err := json.Marshal(index); err != nil || len(raw) > maxIndexBytes {
		return Index{}, fmt.Errorf("plugin index exceeds serialized size limit")
	}
	return index, nil
}

func (entry Entry) plugin(source string) plugins.Plugin {
	return plugins.Plugin{ID: entry.ID, Name: entry.Name, Description: entry.Description, Version: entry.Version, Triggers: entry.Triggers, ManualTarget: entry.ManualTarget, Conflicts: entry.Conflicts, Configuration: entry.Configuration, ConfigurationSchema: entry.ConfigurationSchema, Source: source}
}

func validateSource(entry Entry, source []byte) (plugins.Plugin, error) {
	if len(source) > maxScriptBytes {
		return plugins.Plugin{}, fmt.Errorf("plugin catalog exceeds size limits")
	}
	digest := sha256.Sum256(source)
	if hex.EncodeToString(digest[:]) != entry.SHA256 {
		return plugins.Plugin{}, fmt.Errorf("checksum mismatch for %s", entry.Path)
	}
	item := entry.plugin(string(source))
	if err := plugins.ValidatePlugin(item); err != nil {
		return plugins.Plugin{}, fmt.Errorf("%s: %w", entry.ID, err)
	}
	return item, nil
}

// Read validates a complete repository for the CLI and integration tests. It
// checks every digest and compiles source without evaluating installation code.
func Read(read func(string) ([]byte, error)) ([]plugins.Plugin, error) {
	index, err := ReadIndex(read)
	if err != nil {
		return nil, err
	}
	result := make([]plugins.Plugin, 0, len(index.Plugins))
	total := 0
	for _, entry := range index.Plugins {
		source, err := read(entry.Path)
		if err != nil {
			return nil, err
		}
		total += len(source)
		if len(source) > maxScriptBytes || total > maxCatalogBytes {
			return nil, fmt.Errorf("plugin catalog exceeds size limits")
		}
		item, err := validateSource(entry, source)
		if err != nil {
			return nil, err
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
	result := Result{Source: "empty", Repository: Repository, Plugins: []Entry{}}
	if !s.loadedCache {
		s.loadedCache = true
		if s.cachePath != "" {
			if file, err := os.Open(s.cachePath); err == nil {
				raw, readErr := io.ReadAll(io.LimitReader(file, maxIndexBytes+1))
				_ = file.Close()
				if index, err := parseIndex(raw); readErr == nil && err == nil {
					s.index = &index
				}
			}
		}
	}
	// A first visit without a valid downloaded cache loads the independent
	// repository. Cached catalogs are refreshed only on an explicit request.
	if (refresh || s.index == nil) && (s.lastAttempt.IsZero() || time.Since(s.lastAttempt) >= 5*time.Second) {
		s.warning = ""
		fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		index, err := ReadIndex(func(path string) ([]byte, error) { return s.download(fetchCtx, path) })
		cancel()
		s.lastAttempt = time.Now()
		if err == nil {
			s.index = &index
			result.Source = "remote"
			if s.cachePath != "" {
				raw, _ := json.Marshal(index)
				if err := storage.AtomicWriteFile(s.cachePath, raw, 0600); err != nil {
					s.warning = "Catalog downloaded, but the offline cache could not be saved."
				}
			}
		} else {
			if s.index == nil {
				s.warning = "Plugin catalog unavailable from " + Repository + ". Check the router network settings and retry. " + err.Error()
			} else {
				s.warning = "Update unavailable; keeping the previously downloaded plugin catalog. " + err.Error()
			}
		}
	}
	if s.index != nil {
		result.Plugins = cloneEntries(s.index.Plugins)
		if result.Source != "remote" {
			result.Source = "cache"
		}
	}
	result.Warning = s.warning
	return result, nil
}

func cloneEntries(items []Entry) []Entry {
	raw, _ := json.Marshal(items)
	var result []Entry
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	_ = decoder.Decode(&result)
	return result
}

// Download is used only by an explicit install/update. Snapshot the selected
// metadata before fetching one script, so a concurrent catalog refresh cannot
// change the expected digest or settings halfway through an installation.
func (s *Service) Download(ctx context.Context, id string) (plugins.Plugin, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	catalog, err := s.Get(ctx, false)
	if err != nil {
		return plugins.Plugin{}, err
	}
	if err := ctx.Err(); err != nil {
		return plugins.Plugin{}, err
	}
	for _, entry := range catalog.Plugins {
		if entry.ID != id {
			continue
		}
		source, err := s.download(ctx, entry.Path)
		if err != nil {
			return plugins.Plugin{}, err
		}
		item, err := validateSource(entry, source)
		if err != nil {
			return plugins.Plugin{}, err
		}
		if err := ctx.Err(); err != nil {
			return plugins.Plugin{}, err
		}
		return item, nil
	}
	if catalog.Warning != "" && len(catalog.Plugins) == 0 {
		return plugins.Plugin{}, fmt.Errorf("%s", catalog.Warning)
	}
	return plugins.Plugin{}, plugins.ErrNotFound
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
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("plugin source could not be reached")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plugin source returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxScriptBytes+1))
	if err != nil || len(data) > maxScriptBytes {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("plugin response is incomplete or too large")
	}
	return data, nil
}
