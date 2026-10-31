package plugincatalog

import (
	"encoding/json"
	"regexp"
	"strings"

	"guiforcores/bridge/plugins"
)

// Upgrade only the in-memory catalog loaded from disk. An explicit successful
// remote refresh remains authoritative for the rest of this process. Installed
// plugins and the downloaded cache file are not changed by this merge.
func mergeBundledCatalog(cached, bundled []plugins.Plugin) ([]plugins.Plugin, string) {
	result := append([]plugins.Plugin(nil), cached...)
	indices := make(map[string]int, len(result))
	sizes := make([]int, len(result))
	total := 2 // JSON array brackets; both inputs have already passed validation.
	for i, item := range result {
		indices[item.ID] = i
		raw, _ := json.Marshal(item)
		sizes[i] = len(raw)
		total += len(raw)
		if i > 0 {
			total++
		}
	}
	skipped := false
	for _, item := range bundled {
		index, exists := indices[item.ID]
		if exists && !strictlyNewerVersion(item.Version, result[index].Version) {
			continue
		}
		raw, _ := json.Marshal(item)
		nextSize := total + len(raw)
		if exists {
			nextSize -= sizes[index]
		} else if len(result) > 0 {
			nextSize++
		}
		if nextSize > maxCatalogBytes || (!exists && len(result) >= plugins.MaxPlugins) {
			skipped = true
			continue
		}
		if exists {
			result[index], sizes[index] = item, len(raw)
		} else {
			indices[item.ID] = len(result)
			result, sizes = append(result, item), append(sizes, len(raw))
		}
		total = nextSize
	}
	if skipped {
		return result, "Some bundled plugins could not be merged because the catalog reached its item or size limit. Refresh the catalog to use the repository's complete list."
	}
	return result, ""
}

// Only full SemVer versions participate in automatic replacement. Older custom
// version strings remain untouched; optional v prefixes and build metadata do
// not alter precedence. Numeric strings are compared without integer overflow.
var fullSemver = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

func parseCatalogVersion(version string) []string {
	parts := fullSemver.FindStringSubmatch(version)
	if parts == nil {
		return nil
	}
	for _, identifier := range strings.Split(parts[4], ".") {
		if numericIdentifier(identifier) && len(identifier) > 1 && identifier[0] == '0' {
			return nil
		}
	}
	return parts
}

func numericIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func compareNumeric(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func strictlyNewerVersion(candidate, installed string) bool {
	left, right := parseCatalogVersion(candidate), parseCatalogVersion(installed)
	if left == nil || right == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		if compared := compareNumeric(left[i], right[i]); compared != 0 {
			return compared > 0
		}
	}
	if left[4] == right[4] {
		return false
	}
	if left[4] == "" {
		return true
	}
	if right[4] == "" {
		return false
	}
	leftPre, rightPre := strings.Split(left[4], "."), strings.Split(right[4], ".")
	for i := 0; i < min(len(leftPre), len(rightPre)); i++ {
		leftNumeric, rightNumeric := numericIdentifier(leftPre[i]), numericIdentifier(rightPre[i])
		if leftNumeric != rightNumeric {
			return !leftNumeric
		}
		compared := strings.Compare(leftPre[i], rightPre[i])
		if leftNumeric {
			compared = compareNumeric(leftPre[i], rightPre[i])
		}
		if compared != 0 {
			return compared > 0
		}
	}
	return len(leftPre) > len(rightPre)
}
