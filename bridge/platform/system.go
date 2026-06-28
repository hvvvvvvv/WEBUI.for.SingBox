package platform

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func DetectLibc() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	matches, _ := filepath.Glob("/lib/ld-musl-*")
	if len(matches) > 0 {
		return "musl"
	}
	output, err := exec.Command("ldd", "--version").CombinedOutput()
	if err == nil && strings.Contains(strings.ToLower(string(output)), "musl") {
		return "musl"
	}
	return "glibc"
}

func (s *Service) GetEnv(key string) any {
	if key != "" {
		return os.Getenv(key)
	}
	return s.environment
}

func (s *Service) GetInterfaces() Result {
	interfaces, err := net.Interfaces()
	if err != nil {
		return Result{Flag: false, Data: err.Error()}
	}
	names := make([]string, 0, len(interfaces))
	for _, networkInterface := range interfaces {
		names = append(names, networkInterface.Name)
	}
	return Result{Flag: true, Data: strings.Join(names, "|")}
}
