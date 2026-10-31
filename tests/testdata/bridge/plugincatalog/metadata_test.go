package plugincatalog

import (
	"guiforcores/bridge/plugins"
	"testing"
)

func TestCatalogPreservesAndValidatesDiagnosticConflictAndTextAreaMetadata(t *testing.T) {
	index, files := catalogFixture(t, `throw Error('compile only');function onRun(){return {ok:true}}`)
	entry := &index.Plugins[0]
	entry.Triggers = []string{plugins.OnManual}
	entry.ManualTarget = plugins.ManualDiagnostic
	entry.Conflicts = []string{"another-plugin"}
	entry.ConfigurationSchema = []plugins.Setting{{Key: "hosts", Title: "Hosts", Component: "TextArea", Value: "127.0.0.1 example.test\n"}}
	files["index.json"] = jsonBytes(t, index)
	items, err := Read(readFixture(files))
	if err != nil || len(items) != 1 || items[0].ManualTarget != plugins.ManualDiagnostic || len(items[0].Conflicts) != 1 || items[0].Conflicts[0] != "another-plugin" || items[0].ConfigurationSchema[0].Component != "TextArea" {
		t.Fatalf("catalog metadata lost: %#v %v", items, err)
	}
	entry.Triggers = []string{plugins.OnSubscribe}
	files["index.json"] = jsonBytes(t, index)
	if _, err := Read(readFixture(files)); err == nil {
		t.Fatal("automatic diagnostic in catalog accepted")
	}
	entry.Triggers = []string{plugins.OnManual}
	entry.Conflicts = []string{entry.ID}
	files["index.json"] = jsonBytes(t, index)
	if _, err := Read(readFixture(files)); err == nil {
		t.Fatal("self-conflicting catalog metadata accepted")
	}
}
