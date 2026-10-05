package kernel

import "testing"

func TestCorePlatformNamesMatchOfficialReleaseAssets(t *testing.T) {
	// Independent asset-name contract from the sing-box v1.14.2 release:
	// https://github.com/SagerNet/sing-box/releases/expanded_assets/v1.14.2
	// In particular, no linux-arm archive exists in this release.
	for _, test := range []struct {
		goos, goarch, libc string
		officialAssetName  string
	}{
		{"linux", "arm", "glibc", "sing-box-1.14.2-linux-armv7-glibc.tar.gz"},
		{"linux", "arm", "musl", "sing-box-1.14.2-linux-armv7-musl.tar.gz"},
		{"linux", "arm", "", "sing-box-1.14.2-linux-armv7.tar.gz"},
		{"linux", "amd64", "glibc", "sing-box-1.14.2-linux-amd64-glibc.tar.gz"},
		{"linux", "amd64", "musl", "sing-box-1.14.2-linux-amd64-musl.tar.gz"},
		{"linux", "arm64", "glibc", "sing-box-1.14.2-linux-arm64-glibc.tar.gz"},
		{"linux", "arm64", "musl", "sing-box-1.14.2-linux-arm64-musl.tar.gz"},
		{"windows", "amd64", "", "sing-box-1.14.2-windows-amd64.zip"},
		{"windows", "arm64", "", "sing-box-1.14.2-windows-arm64.zip"},
		{"windows", "386", "", "sing-box-1.14.2-windows-386.zip"},
		{"darwin", "amd64", "", "sing-box-1.14.2-darwin-amd64.tar.gz"},
		{"darwin", "arm64", "", "sing-box-1.14.2-darwin-arm64.tar.gz"},
	} {
		t.Run(test.goos+"/"+test.goarch+"/"+test.libc, func(t *testing.T) {
			assetName := getKernelAssetFileNameForPlatform("1.14.2", test.goos, test.goarch, test.libc)
			if assetName != test.officialAssetName {
				t.Fatalf("download would select %s instead of this platform's official asset %s", assetName, test.officialAssetName)
			}
		})
	}
}
