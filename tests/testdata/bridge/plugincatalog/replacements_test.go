package plugincatalog

import "testing"

func TestHistoricalCatalogMetadataRequiresValidReplacement(t *testing.T) {
	for _, invalid := range []string{"", "self", "missing", "deprecated", "not-deprecated"} {
		t.Run(invalid, func(t *testing.T) {
			index, files := catalogFixture(t, "function onSubscribe(p){return p}", "function onSubscribe(p){return p}")
			index.Plugins[0].Deprecated = true
			index.Plugins[0].ReplacedBy = index.Plugins[1].ID
			switch invalid {
			case "self":
				index.Plugins[0].ReplacedBy = index.Plugins[0].ID
			case "missing":
				index.Plugins[0].ReplacedBy = "absent"
			case "deprecated":
				index.Plugins[1].Deprecated = true
			case "not-deprecated":
				index.Plugins[0].Deprecated = false
			}
			files["index.json"] = jsonBytes(t, index)
			metadata, err := ReadIndex(readFixture(files))
			if invalid != "" {
				if err == nil {
					t.Fatal("invalid replacement accepted")
				}
				return
			}
			if err != nil || len(metadata.Plugins) != 2 || !metadata.Plugins[0].Deprecated || metadata.Plugins[0].ReplacedBy != index.Plugins[1].ID {
				t.Fatalf("legacy metadata lost: %+v %v", metadata, err)
			}
			installed, err := Read(readFixture(files))
			if err != nil || len(installed) != 2 || installed[0].ID != index.Plugins[0].ID {
				t.Fatalf("historical source metadata cannot be validated: %+v %v", installed, err)
			}
		})
	}
}
