package routerplugin

import (
	"fmt"
	"io/fs"

	routerplugins "guiforcores/router-plugins"
)

// Catalog returns fresh parsed templates in stable filename order.
func Catalog() ([]Manifest, error) {
	names, err := fs.Glob(routerplugins.Catalog, "catalog/*.json")
	if err != nil {
		return nil, err
	}
	result := make([]Manifest, 0, len(names))
	for _, name := range names {
		data, err := routerplugins.Catalog.ReadFile(name)
		if err != nil {
			return nil, err
		}
		manifest, err := ParseManifest(string(data))
		if err != nil {
			return nil, fmt.Errorf("catalog %s: %w", name, err)
		}
		result = append(result, manifest)
	}
	return result, nil
}
