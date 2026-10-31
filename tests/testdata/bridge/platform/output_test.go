package platform

import "testing"

func TestCommandOutputPreservesUTF8(t *testing.T) {
	const output = "路由器接口 eth0 — café ✓\n"
	if actual := decodeCommandOutput([]byte(output)); actual != output {
		t.Fatalf("UTF-8 output corrupted: %q", actual)
	}
}
