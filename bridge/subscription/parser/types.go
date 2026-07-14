// Package parser converts third-party proxy subscription formats into
// sing-box outbound objects. The conversion rules are independently
// implemented in Go with Sub-Store's parser pipeline used as a behavioral
// reference: https://github.com/sub-store-org/Sub-Store
//
// Sub-Store's current repository is distributed under AGPL-3.0; this project
// is distributed under GPL-3.0. No Sub-Store source code is embedded here.
package parser

import "fmt"

// Node is the normalized, format-independent representation used between the
// input parsers and the sing-box producer. It intentionally remains flexible
// because proxy protocols evolve faster than the application's public API.
type Node map[string]any

// Issue describes one skipped candidate without retaining its source text,
// which can contain passwords, UUIDs, or private keys.
type Issue struct {
	Index  int
	Parser string
	Reason string
}

// Result contains successfully converted outbounds and aggregate diagnostics.
type Result struct {
	Outbounds []map[string]any
	Total     int
	Skipped   int
	Issues    []Issue
}

// Error is returned when the fallback parser cannot produce any outbound.
type Error struct {
	Total  int
	Issues []Issue
}

func (e *Error) Error() string {
	if e == nil {
		return "subscription parsing failed"
	}
	if len(e.Issues) == 0 {
		return "subscription parsing failed: no proxy candidates found"
	}
	return fmt.Sprintf("subscription parsing failed: no valid proxies (%d candidates, %d skipped)", e.Total, len(e.Issues))
}
