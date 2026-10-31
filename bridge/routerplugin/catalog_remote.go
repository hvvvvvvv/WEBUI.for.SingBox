package routerplugin

import (
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

	"guiforcores/bridge/storage"
)

const Repository = "Ghostdehole/WEBUI.for.SingBox-Plugins"
const catalogAPI = "webui.router.catalog/v1"
const catalogLimit = 1024 * 1024

var pluginPathPattern = regexp.MustCompile(`^plugins/[a-z0-9][a-z0-9._-]{0,80}[.]json$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type CatalogResult struct {
	Plugins    []Manifest `json:"plugins"`
	Source     string     `json:"source"`
	Repository string     `json:"repository"`
	Warning    string     `json:"warning,omitempty"`
}

type catalogIndex struct {
	APIVersion string `json:"apiVersion"`
	Plugins    []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"plugins"`
}

// CatalogService fetches data only from this fork's independent plugin repository.
// Profiles contain their own immutable manifest snapshots; a refresh never edits them.
type CatalogService struct {
	gate        chan struct{}
	cachePath   string
	baseURL     string
	client      *http.Client
	plugins     []Manifest
	lastAttempt time.Time
	lastWarning string
}

func NewCatalogService(cachePath string) *CatalogService {
	return &CatalogService{
		gate:      make(chan struct{}, 1),
		cachePath: cachePath,
		baseURL:   "https://raw.githubusercontent.com/" + Repository + "/main/",
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return fmt.Errorf("plugin source redirects are not allowed")
			},
		},
	}
}

func (s *CatalogService) Get(ctx context.Context, refresh bool) (CatalogResult, error) {
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return CatalogResult{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return CatalogResult{}, err
	}
	result := CatalogResult{Repository: Repository, Source: "bundled"}
	if s.plugins == nil && s.cachePath != "" {
		if file, err := os.Open(s.cachePath); err == nil {
			data, readErr := io.ReadAll(io.LimitReader(file, catalogLimit+1))
			_ = file.Close()
			var cached []Manifest
			if readErr == nil && len(data) <= catalogLimit && json.Unmarshal(data, &cached) == nil && ValidateCatalog(cached) == nil {
				s.plugins = cached
			}
		}
	}
	if refresh && (s.lastAttempt.IsZero() || time.Since(s.lastAttempt) >= 5*time.Second) {
		s.lastWarning = ""
		fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		fetched, err := s.fetch(fetchCtx)
		cancel()
		// Measure from completion so queued requests reuse this result instead
		// of each repeating a slow refresh once they acquire the gate.
		s.lastAttempt = time.Now()
		if err == nil {
			s.plugins = fetched
			result.Source = "remote"
			if s.cachePath != "" {
				data, marshalErr := json.Marshal(fetched)
				if marshalErr != nil {
					return result, marshalErr
				}
				if err := storage.AtomicWriteFile(s.cachePath, data, 0600); err != nil {
					s.lastWarning = "Catalog downloaded; unable to save offline cache."
				}
			}
		} else {
			s.lastWarning = "Plugin update unavailable; keeping the previous catalog. " + err.Error()
		}
	}
	if s.plugins != nil {
		result.Plugins = append([]Manifest(nil), s.plugins...)
		if result.Source != "remote" {
			result.Source = "cache"
		}
	} else {
		var err error
		result.Plugins, err = Catalog()
		if err != nil {
			return result, err
		}
	}
	result.Warning = s.lastWarning
	return result, nil
}

// ValidateCatalog validates a complete catalog before it replaces a known-good one.
func ValidateCatalog(plugins []Manifest) error {
	if len(plugins) == 0 || len(plugins) > 32 {
		return fmt.Errorf("catalog must contain 1 to 32 plugins")
	}
	seen := map[string]bool{}
	total := 0
	for _, plugin := range plugins {
		data, err := json.Marshal(plugin)
		if err != nil {
			return err
		}
		total += len(data)
		if total > catalogLimit {
			return fmt.Errorf("catalog is too large")
		}
		parsed, err := ParseManifest(string(data))
		if err != nil {
			return err
		}
		if seen[parsed.ID] {
			return fmt.Errorf("duplicate plugin id %q", parsed.ID)
		}
		seen[parsed.ID] = true
	}
	return nil
}

func (s *CatalogService) fetch(ctx context.Context) ([]Manifest, error) {
	data, err := s.download(ctx, "index.json", 64*1024)
	if err != nil {
		return nil, err
	}
	var index catalogIndex
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&index); err != nil {
		return nil, fmt.Errorf("invalid plugin index: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF || index.APIVersion != catalogAPI || len(index.Plugins) == 0 || len(index.Plugins) > 32 {
		return nil, fmt.Errorf("unsupported or invalid plugin index")
	}
	plugins := make([]Manifest, 0, len(index.Plugins))
	paths := map[string]bool{}
	for _, entry := range index.Plugins {
		if !pluginPathPattern.MatchString(entry.Path) || strings.Contains(entry.Path, "..") || !digestPattern.MatchString(entry.SHA256) || paths[entry.Path] {
			return nil, fmt.Errorf("invalid plugin path or digest")
		}
		paths[entry.Path] = true
		raw, err := s.download(ctx, entry.Path, 32*1024)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != entry.SHA256 {
			return nil, fmt.Errorf("checksum mismatch for %s", entry.Path)
		}
		plugin, err := ParseManifest(string(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid plugin %s: %w", entry.Path, err)
		}
		plugins = append(plugins, plugin)
	}
	if err := ValidateCatalog(plugins); err != nil {
		return nil, err
	}
	return plugins, nil
}

func (s *CatalogService) download(ctx context.Context, path string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "WEBUI.for.SingBox-Router-Plugins")
	response, err := s.client.Do(req)
	if err != nil {
		// Network errors can contain proxy credentials; do not send their text to the UI.
		return nil, fmt.Errorf("plugin source could not be reached")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plugin source returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, fmt.Errorf("plugin response is incomplete or too large")
	}
	return data, nil
}
