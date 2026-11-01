package config

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"guiforcores/bridge/storage"
)

// The complete default rules are shipped with the app. Only runtime configs
// receive these device-local paths; previews, clipboard exports and QRS shares
// stay portable. The core still owns remote updates and its newer disk cache.
//
//go:embed ruleset_seed/*.srs ruleset_seed/manifest.json
var ruleSetSeedFiles embed.FS

type ruleSetSeed struct {
	File   string `json:"file"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type ruleSetSeedManifest struct {
	Snapshot string        `json:"snapshot"`
	Files    []ruleSetSeed `json:"files"`
}

var ruleSetSeedWriteMu sync.Mutex

func readRuleSetSeedManifest() (*ruleSetSeedManifest, error) {
	data, err := ruleSetSeedFiles.ReadFile("ruleset_seed/manifest.json")
	if err != nil {
		return nil, err
	}
	var manifest ruleSetSeedManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if snapshot, err := hex.DecodeString(manifest.Snapshot); err != nil || len(snapshot) != 20 || len(manifest.Files) != 6 {
		return nil, fmt.Errorf("invalid embedded rule-set manifest")
	}
	for _, file := range manifest.Files {
		if filepath.Base(file.File) != file.File || strings.ContainsAny(file.File, `/\`) || !strings.HasSuffix(file.File, ".srs") || file.Bytes <= 0 {
			return nil, fmt.Errorf("invalid embedded rule-set file name or size")
		}
		if sum, err := hex.DecodeString(file.SHA256); err != nil || len(sum) != sha256.Size {
			return nil, fmt.Errorf("invalid embedded rule-set checksum")
		}
	}
	return &manifest, nil
}

// Match the resource identity, never the user-editable tag. A custom list with
// the same tag must not silently receive unrelated rules. Pinned revisions,
// third-party replacements and source-format lists retain their own semantics.
func defaultRuleSetResource(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
		return ""
	}
	var prefix string
	switch strings.ToLower(u.Hostname()) {
	case "testingcf.jsdelivr.net", "cdn.jsdelivr.net", "fastly.jsdelivr.net", "gcore.jsdelivr.net":
		prefix = "/gh/MetaCubeX/meta-rules-dat@sing/"
	case "raw.githubusercontent.com":
		prefix = "/MetaCubeX/meta-rules-dat/sing/"
	default:
		return ""
	}
	if !strings.HasPrefix(u.Path, prefix) {
		return ""
	}
	return strings.TrimPrefix(u.Path, prefix)
}

// PrepareRuleSetBootstrap runs after mixins/plugins and before the core config
// check (and before stopping an existing core). Failure to install a seed is an
// actionable preparation error, never permission to start with empty rules.
func (s *Service) PrepareRuleSetBootstrap(ctx context.Context, config map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	route, _ := config["route"].(map[string]any)
	rules, _ := route["rule_set"].([]any)
	manifest, err := readRuleSetSeedManifest()
	if err != nil {
		return fmt.Errorf("read built-in initial rule sets: %w", err)
	}
	type pendingSeed struct {
		rule map[string]any
		file ruleSetSeed
		path string
	}
	var pending []pendingSeed
	for _, value := range rules {
		rule, _ := value.(map[string]any)
		if rule["type"] != "remote" || (rule["format"] != nil && rule["format"] != "" && rule["format"] != "binary") {
			continue
		}
		rawURL, _ := rule["url"].(string)
		resource := defaultRuleSetResource(rawURL)
		for _, file := range manifest.Files {
			if resource != file.Path {
				continue
			}
			initialPath := s.paths.Resolve(filepath.Join("data", "rule-set-bootstrap", manifest.Snapshot, file.File))
			if explicit, exists := rule["initial_path"]; exists && explicit != initialPath {
				// Even an explicit empty path is a user choice to disable seeding.
				continue
			}
			pending = append(pending, pendingSeed{rule, file, initialPath})
		}
	}
	if len(pending) == 0 {
		return nil
	}
	ruleSetSeedWriteMu.Lock()
	defer ruleSetSeedWriteMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	// Users may put the entire data directory on external router storage. Keep
	// that supported layout, but reject redirects inside our own seed directory.
	if err := os.MkdirAll(s.paths.Resolve("data"), 0700); err != nil {
		return fmt.Errorf("prepare initial rule-set data directory: %w", err)
	}
	for _, relative := range []string{"data/rule-set-bootstrap", "data/rule-set-bootstrap/" + manifest.Snapshot} {
		dir := s.paths.Resolve(relative)
		if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return fmt.Errorf("prepare initial rule-set directory: %w", err)
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return fmt.Errorf("inspect initial rule-set directory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("initial rule-set directory is not a regular directory: %s", dir)
		}
	}
	for _, item := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := installRuleSetSeed(item.path, item.file); err != nil {
			return fmt.Errorf("prepare initial rule set %s: %w", item.file.File, err)
		}
	}
	for _, item := range pending {
		item.rule["initial_path"] = item.path
		if interval, exists := item.rule["update_interval"]; !exists || interval == "" {
			item.rule["update_interval"] = "24h"
		}
	}
	return nil
}

func installRuleSetSeed(target string, file ruleSetSeed) error {
	info, err := os.Lstat(target)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("initial rule-set path is not a regular file")
		}
		if info.Size() == file.Bytes {
			data, err := os.ReadFile(target)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) == file.SHA256 {
				return nil // Avoid rewriting flash on every preview or core start.
			}
		}
	}
	data, err := ruleSetSeedFiles.ReadFile("ruleset_seed/" + file.File)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != file.Bytes || hex.EncodeToString(sum[:]) != file.SHA256 {
		return fmt.Errorf("embedded initial rule-set checksum mismatch")
	}
	return storage.AtomicWriteFile(target, data, 0600)
}
