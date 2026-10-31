//go:build windows

package platform

import (
	"math"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

var procGetOEMCP = modKernel32.NewProc("GetOEMCP")

func decodeCommandOutput(data []byte) string {
	if len(data) == 0 || utf8.Valid(data) {
		return string(data)
	}
	codePage, err := windows.GetConsoleOutputCP()
	if err != nil || codePage == 0 {
		oemPage, _, _ := procGetOEMCP.Call()
		codePage = uint32(oemPage)
	}
	return decodeWindowsCodePage(data, codePage)
}

func decodeWindowsCodePage(data []byte, codePage uint32) string {
	if len(data) == 0 || len(data) > math.MaxInt32 || codePage == 0 {
		return string(data)
	}
	length, err := windows.MultiByteToWideChar(codePage, 0, &data[0], int32(len(data)), nil, 0)
	if err != nil || length <= 0 {
		return string(data)
	}
	wide := make([]uint16, length)
	length, err = windows.MultiByteToWideChar(codePage, 0, &data[0], int32(len(data)), &wide[0], length)
	if err != nil {
		return string(data)
	}
	return string(utf16.Decode(wide[:length]))
}
