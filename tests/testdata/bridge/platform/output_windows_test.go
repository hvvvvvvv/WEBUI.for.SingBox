//go:build windows

package platform

import "testing"

func TestWindowsCommandOutputUsesSelectedCodePage(t *testing.T) {
	for _, test := range []struct {
		page uint32
		data []byte
		want string
	}{
		{936, []byte{0xd6, 0xd0, 0xce, 0xc4}, "中文"},
		{1252, []byte{0x63, 0x61, 0x66, 0xe9}, "café"},
	} {
		if actual := decodeWindowsCodePage(test.data, test.page); actual != test.want {
			t.Fatalf("CP %d output=%q, want %q", test.page, actual, test.want)
		}
	}
}
