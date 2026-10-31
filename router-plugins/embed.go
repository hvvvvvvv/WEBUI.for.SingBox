// Package routerplugins contains the offline, declarative router plugin catalog.
package routerplugins

import "embed"

// Catalog contains data only. The host never evaluates code from a plugin.
//
//go:embed catalog/*.json
var Catalog embed.FS
