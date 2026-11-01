package config

import (
	"os"
	"strings"
	"testing"
)

func selectedConfigTestCore(t *testing.T) (string, string) {
	t.Helper()
	core := os.Getenv("SING_BOX_TEST_BINARY")
	if core == "" {
		core = os.Getenv("SING_BOX_114_PATH")
	}
	if core == "" {
		t.Skip("set SING_BOX_TEST_BINARY (or historical SING_BOX_114_PATH) for real core checks")
	}
	output, err := runHTTPClientCoreCommand(core, "version")
	fields := strings.Fields(strings.SplitN(string(output), "\n", 2)[0])
	if err != nil || len(fields) != 3 || fields[0] != "sing-box" || fields[1] != "version" {
		t.Fatalf("read core version: %v\n%s", err, output)
	}
	t.Logf("real core: %s", fields[2])
	return core, fields[2]
}
