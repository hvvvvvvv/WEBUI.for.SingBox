package appupdate

import "testing"

func TestAppUpdateAssetNameForPlatform(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   string
	}{
		{
			name:   "Linux AMD64",
			goos:   "linux",
			goarch: "amd64",
			want:   "webui.for.singbox-linux-amd64.zip",
		},
		{
			name:   "Linux ARM64",
			goos:   "linux",
			goarch: "arm64",
			want:   "webui.for.singbox-linux-arm64.zip",
		},
		{
			name:   "Linux ARMv7",
			goos:   "linux",
			goarch: "arm",
			want:   "webui.for.singbox-linux-armv7.zip",
		},
		{
			name:   "Windows ARM64",
			goos:   "windows",
			goarch: "arm64",
			want:   "webui.for.singbox-windows-arm64.zip",
		},
		{
			name:   "Windows 386",
			goos:   "windows",
			goarch: "386",
			want:   "webui.for.singbox-windows-386.zip",
		},
		{
			name:   "macOS AMD64",
			goos:   "darwin",
			goarch: "amd64",
			want:   "webui.for.singbox-darwin-amd64.zip",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := appUpdateAssetNameForPlatform(test.goos, test.goarch); got != test.want {
				t.Fatalf("asset name = %q, want %q", got, test.want)
			}
		})
	}
}
