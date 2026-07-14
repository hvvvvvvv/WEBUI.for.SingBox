package parser

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/titanous/json5"
	"gopkg.in/yaml.v3"
)

const (
	maxSubscriptionCandidates = 10000
	candidateLimitReason      = "subscription candidate limit exceeded"
)

// parseInput applies the same broad ordering as Sub-Store: unwrap an encoded
// subscription, prefer structured Clash/Mihomo documents, then try the URI and
// application-specific line parsers in a stable order. A parser failure only
// skips that candidate; no Issue contains the candidate text.
func parseInput(raw string) ([]Node, []Issue, int) {
	content := unwrapSubscriptionBase64(raw)
	if content == "" {
		return nil, nil, 0
	}
	if nodes, issues, total, matched := parseDocument(content); matched {
		return nodes, issues, total
	}

	lines := strings.Split(content, "\n")
	nodes := make([]Node, 0, len(lines))
	issues := make([]Issue, 0)
	total := 0
	for _, rawLine := range lines {
		line := strings.TrimSpace(strings.TrimPrefix(rawLine, "\ufeff"))
		if shouldIgnoreLine(line) {
			continue
		}
		if total >= maxSubscriptionCandidates {
			total++
			issues = append(issues, Issue{Index: total, Parser: "input limit", Reason: candidateLimitReason})
			return nil, issues, total
		}

		// A flow-style Clash object is commonly mixed into an otherwise
		// line-oriented subscription. YAML list markers are accepted too.
		documentLine := strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if strings.HasPrefix(documentLine, "{") || strings.HasPrefix(documentLine, "[") {
			if parsed, parsedIssues, parsedTotal, matched := parseDocument(documentLine); matched {
				if parsedTotal > maxSubscriptionCandidates-total {
					total += parsedTotal
					issues = append(issues, Issue{
						Index:  maxSubscriptionCandidates + 1,
						Parser: "input limit",
						Reason: candidateLimitReason,
					})
					return nil, issues, total
				}
				offset := total
				total += parsedTotal
				for _, node := range parsed {
					if sourceIndex, ok := nodeInt(node, "_source_index"); ok {
						node["_source_index"] = sourceIndex + offset
					}
				}
				nodes = append(nodes, parsed...)
				for _, issue := range parsedIssues {
					issue.Index += offset
					issues = append(issues, issue)
				}
				continue
			}
		}

		if node, parserName, matched, err := parseURI(line); matched {
			total++
			if err != nil {
				issues = append(issues, newInputIssue(total, parserName, err))
			} else {
				node["_source_index"] = total
				nodes = append(nodes, node)
			}
			continue
		}
		if node, parserName, matched, err := parsePlatformLine(line); matched {
			total++
			if err != nil {
				issues = append(issues, newInputIssue(total, parserName, err))
			} else {
				node["_source_index"] = total
				nodes = append(nodes, node)
			}
			continue
		}

		total++
		issues = append(issues, Issue{
			Index:  total,
			Parser: "input",
			Reason: "unsupported subscription line format",
		})
	}
	return nodes, issues, total
}

func shouldIgnoreLine(line string) bool {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "//") {
		return true
	}
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && !strings.Contains(line, ",") {
		return true
	}
	return false
}

func newInputIssue(index int, parserName string, err error) Issue {
	reason := "proxy candidate is invalid"
	switch {
	case errors.Is(err, errMalformedURI):
		reason = "malformed proxy URI"
	case errors.Is(err, errMissingEndpoint):
		reason = "proxy endpoint is missing or invalid"
	case errors.Is(err, errMissingCredentials):
		reason = "proxy credentials are missing or invalid"
	case errors.Is(err, errInvalidPayload):
		reason = "encoded proxy payload is invalid"
	case errors.Is(err, errUnknownPlatformType):
		reason = "unsupported application proxy type"
	case err != nil && err.Error() == "unsupported proxy URI scheme":
		reason = "unsupported proxy URI scheme"
	}
	return Issue{Index: index, Parser: parserName, Reason: reason}
}

func parseDocument(content string) ([]Node, []Issue, int, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, nil, 0, false
	}

	var decoded any
	jsonOK := json.Unmarshal([]byte(trimmed), &decoded) == nil
	json5OK := false
	if !jsonOK && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
		decoder := json5.NewDecoder(strings.NewReader(trimmed))
		if decoder.Decode(&decoded) == nil {
			var trailing any
			trailingErr := decoder.Decode(&trailing)
			json5OK = errors.Is(trailingErr, io.EOF)
			if trailingErr == nil {
				// The JSON5 decoder accepts a stream of top-level values. This
				// parser expects exactly one document; let the line parser handle
				// each value so aggregate limits and diagnostics remain accurate.
				return nil, nil, 0, false
			}
		}
	}
	if !jsonOK && !json5OK {
		// Avoid treating an arbitrary URI or platform line as a YAML scalar.
		lower := strings.ToLower(trimmed)
		looksLikeSingleProxy := (strings.HasPrefix(lower, "type:") || strings.Contains(lower, "\ntype:")) &&
			(strings.HasPrefix(lower, "server:") || strings.Contains(lower, "\nserver:"))
		looksStructured := strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") ||
			strings.HasPrefix(trimmed, "- ") || hasTopLevelYAMLKey(trimmed, "proxies") ||
			strings.HasPrefix(trimmed, "---") || looksLikeSingleProxy
		if !looksStructured {
			return nil, nil, 0, false
		}
		if yaml.Unmarshal([]byte(trimmed), &decoded) != nil {
			return nil, nil, 0, false
		}
	}
	decoded = normalizeDecodedValue(decoded)
	candidates, matched := documentCandidates(decoded)
	if !matched {
		return nil, nil, 0, false
	}
	if len(candidates) > maxSubscriptionCandidates {
		return nil, []Issue{{
			Index:  maxSubscriptionCandidates + 1,
			Parser: "input limit",
			Reason: candidateLimitReason,
		}}, len(candidates), true
	}

	nodes := make([]Node, 0, len(candidates))
	issues := make([]Issue, 0)
	for index, candidate := range candidates {
		node, err := normalizeDocumentNode(candidate)
		if err != nil {
			issues = append(issues, newInputIssue(index+1, "Clash/Mihomo document", err))
			continue
		}
		node["_source_index"] = index + 1
		nodes = append(nodes, node)
	}
	return nodes, issues, len(candidates), true
}

func documentCandidates(decoded any) ([]any, bool) {
	switch typed := decoded.(type) {
	case []any:
		return typed, true
	case map[string]any:
		for key, value := range typed {
			if !strings.EqualFold(key, "proxies") {
				continue
			}
			if proxies, ok := value.([]any); ok {
				return proxies, true
			}
			return []any{value}, true
		}
		if stringValue(firstMapValue(typed, "type")) != "" {
			return []any{typed}, true
		}
	}
	return nil, false
}

func normalizeDecodedValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = normalizeDecodedValue(item)
		}
		return result
	case map[any]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[stringValue(key)] = normalizeDecodedValue(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = normalizeDecodedValue(item)
		}
		return result
	default:
		return value
	}
}

func normalizeDocumentNode(candidate any) (Node, error) {
	source, ok := candidate.(map[string]any)
	if !ok {
		return nil, errInvalidPayload
	}
	typ := normalizeType(stringValue(firstMapValue(source, "type", "protocol")))
	if typ == "" {
		return nil, errInvalidPayload
	}
	node := make(Node, len(source)+2)
	for key, value := range source {
		node[key] = normalizeDecodedValue(value)
	}
	node["type"] = typ
	if port, ok := intValue(firstMapValue(source, "port", "server_port", "server-port")); ok {
		node["port"] = port
	}
	server := stringValue(firstMapValue(source, "server", "address", "host"))
	if server != "" {
		node["server"] = strings.Trim(server, "[]")
	}
	if value := firstMapValue(source, "servername", "server-name", "peer"); value != nil && stringValue(node["sni"]) == "" {
		node["sni"] = stringValue(value)
	}
	if value := firstMapValue(source, "client-fingerprint", "fp"); value != nil {
		node["client-fingerprint"] = stringValue(value)
	}
	if value := firstMapValue(source, "fingerprint"); value != nil && stringValue(value) != "" {
		// Clash/Mihomo `fingerprint` is a certificate hash, not a uTLS
		// ClientHello profile. sing-box uses a different SPKI pin format, so
		// weakening the node by silently treating or dropping it is unsafe.
		node["_unsupported_certificate_fingerprint"] = true
	}
	if value := firstMapValue(source, "skip-cert-verify", "allow-insecure", "insecure"); value != nil {
		if parsed, ok := boolValue(value); ok {
			node["skip-cert-verify"] = parsed
		}
	}
	if typ != "direct" && typ != "reject" {
		port, portOK := intValue(node["port"])
		if server == "" || !portOK || !validPort(port) {
			return nil, errMissingEndpoint
		}
	}
	name := stringValue(firstMapValue(source, "name", "tag", "remarks", "remark"))
	if name == "" {
		if server == "" {
			name = strings.ToUpper(typ)
		} else {
			port, _ := intValue(node["port"])
			name = defaultName(typ, server, port)
		}
	}
	node["name"] = name
	return node, nil
}
