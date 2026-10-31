package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"guiforcores/bridge/storage"
	"guiforcores/bridge/syncstate"

	connect "connectrpc.com/connect"
)

// SubscriptionPlugins is deliberately independent of the plugin implementation.
// The runtime owns subscription persistence and serializes it with refreshes.
type SubscriptionPlugins interface {
	ApplySubscription(context.Context, []map[string]any, map[string]any) ([]map[string]any, error)
	RunSubscription(context.Context, string, []map[string]any, map[string]any) ([]map[string]any, []string, error)
}

type PluginSubscriptionResult struct {
	Logs           []string `json:"logs"`
	Before         int      `json:"before"`
	After          int      `json:"after"`
	SubscriptionID string   `json:"subscription_id"`
}

func (s *appRuntimeService) SetPlugins(plugins SubscriptionPlugins) {
	s.subscriptionsMu.Lock()
	s.plugins = plugins
	s.subscriptionsMu.Unlock()
}

func subscriptionPluginMetadata(sub subscription) (map[string]any, error) {
	data, err := json.Marshal(sub)
	if err != nil {
		return nil, err
	}
	var metadata map[string]any
	err = json.Unmarshal(data, &metadata)
	return metadata, err
}

func subscriptionPluginDigest(proxies []map[string]any) ([32]byte, error) {
	data, err := json.Marshal(proxies)
	return sha256.Sum256(data), err
}

// Carry GUI identity only through the isolated script input, never the core
// cache. Scripts that clone a node with object spread retain this identity.
func annotatePluginProxyIDs(proxies []map[string]any, refs []proxyRef) {
	for index, proxy := range proxies {
		if proxy == nil {
			continue
		}
		delete(proxy, "__id_in_gui")
		tag, _ := proxy["tag"].(string)
		id := ""
		if index < len(refs) && refs[index].Tag == tag {
			id = refs[index].ID
		} else {
			id = findPreviousProxyID(refs, tag)
		}
		if id != "" {
			proxy["__id_in_gui"] = id
		}
	}
}

func pluginProxyRefs(proxies []map[string]any, previous, intermediate []proxyRef) ([]proxyRef, error) {
	ids := make([]string, len(proxies))
	used := make(map[string]bool)
	assign := func(index int, id string) {
		if ids[index] == "" && id != "" && !used[id] {
			ids[index], used[id] = id, true
		}
	}
	// Prefer an identity already persisted before this refresh. If the raw
	// subscription uses pre-rename tags, match the final tags against those
	// persisted refs before falling back to newly created intermediate IDs.
	for _, refs := range [][]proxyRef{previous, intermediate} {
		known := make(map[string]bool, len(refs))
		for _, ref := range refs {
			known[ref.ID] = true
		}
		for index, proxy := range proxies {
			id, _ := proxy["__id_in_gui"].(string)
			if known[id] {
				assign(index, id)
			}
		}
		for index, proxy := range proxies {
			tag, _ := proxy["tag"].(string)
			for _, ref := range refs {
				if ref.Tag == tag {
					assign(index, ref.ID)
				}
			}
		}
	}
	for _, proxy := range proxies {
		delete(proxy, "__id_in_gui")
	}
	return proxyRefsFromOutboundsWithIDs(proxies, nil, ids)
}

// RunPluginOnSubscription uses the selected subscription's existing cache only.
// A manual transformation does not request a core restart or fetch its source.
func (s *appRuntimeService) RunPluginOnSubscription(ctx context.Context, pluginID, subscriptionID string) (PluginSubscriptionResult, error) {
	result := PluginSubscriptionResult{Logs: []string{}, SubscriptionID: subscriptionID}
	if strings.TrimSpace(pluginID) == "" || strings.TrimSpace(subscriptionID) == "" {
		return result, connect.NewError(connect.CodeInvalidArgument, errors.New("plugin_id and subscription_id are required"))
	}
	s.subscriptionsMu.Lock()
	locked := true
	defer func() {
		if locked {
			s.subscriptionsMu.Unlock()
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.plugins == nil {
		return result, connect.NewError(connect.CodeFailedPrecondition, errors.New("plugin service is unavailable"))
	}
	sub, items, err := findSubscription(subscriptionID)
	if err != nil {
		return result, connect.NewError(connect.CodeNotFound, err)
	}
	cachePath := GetPath(subscriptionContentPath(subscriptionID))
	previous, err := readPluginSubscriptionCache(cachePath)
	if os.IsNotExist(err) {
		return result, connect.NewError(connect.CodeFailedPrecondition, errors.New("subscription has no cached nodes; refresh or save its content first"))
	}
	if err != nil {
		return result, err
	}
	var proxies []map[string]any
	trimmed := bytes.TrimSpace(previous)
	if len(trimmed) == 0 || trimmed[0] != '[' || json.Unmarshal(trimmed, &proxies) != nil {
		return result, connect.NewError(connect.CodeFailedPrecondition, errors.New("subscription cache is not a valid node array"))
	}
	metadata, err := subscriptionPluginMetadata(*sub)
	if err != nil {
		return result, err
	}
	result.Before = len(proxies)
	result.After = result.Before
	annotatePluginProxyIDs(proxies, sub.Proxies)
	processed, logs, err := s.plugins.RunSubscription(ctx, pluginID, proxies, metadata)
	if logs != nil {
		result.Logs = logs
	}
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if processed == nil {
		processed = []map[string]any{}
	}
	refs, err := pluginProxyRefs(processed, sub.Proxies, nil)
	if err != nil {
		return result, err
	}
	data, err := json.MarshalIndent(processed, "", "  ")
	if err != nil {
		return result, err
	}
	if len(data) > maxSubscriptionBodyBytes {
		return result, connect.NewError(connect.CodeResourceExhausted, errors.New("processed subscription exceeds cache size limit"))
	}
	sub.Proxies = refs
	sub.UpdateTime = time.Now().UnixMilli()
	sub.Updating = false
	if err := commitPluginSubscription(ctx, cachePath, previous, data, func() error { return saveSubscriptions(items) }); err != nil {
		return result, err
	}
	result.After = len(processed)
	state := s.state.Advance(syncstate.DomainSubscriptions, subscriptionIDs(items), []string{subscriptionID}, nil, false, subscriptionID)
	s.subscriptionsMu.Unlock()
	locked = false
	s.publishResourceChanged(syncstate.DomainSubscriptions, syncstate.OperationUpsert, []string{subscriptionID}, state)
	return result, nil
}

func readPluginSubscriptionCache(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSubscriptionBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSubscriptionBodyBytes {
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("subscription cache exceeds size limit"))
	}
	return data, nil
}

// Both files use atomic replacement. If committing metadata fails, restore the
// cache while the subscription lock is still held so failed runs retain nodes.
func commitPluginSubscription(ctx context.Context, path string, previous, next []byte, saveMetadata func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	changed := !bytes.Equal(previous, next)
	if changed {
		if err := storage.AtomicWriteFile(path, next, 0644); err != nil {
			return err
		}
	}
	err := ctx.Err()
	if err == nil {
		err = saveMetadata()
	}
	if err != nil && changed {
		if rollbackErr := storage.AtomicWriteFile(path, previous, 0644); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("restore previous subscription cache: %w", rollbackErr))
		}
	}
	return err
}
