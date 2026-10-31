//go:build !windows

package platform

func decodeCommandOutput(data []byte) string {
	return string(data)
}
