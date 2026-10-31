package plugindata

import "embed"

// Files is the offline snapshot of this fork's independently maintained script catalog.
//
//go:embed index.json plugins/*.js
var Files embed.FS
