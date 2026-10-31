package main

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/kardianos/service"
)

func TestProcdPreservesArgumentsAndShutdownGrace(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell not available")
	}
	config := &service.Config{
		Executable: "/opt/web ui/'literal$HOME{{core}}/server",
		Arguments:  []string{"--addr", "[::]:9090", "--log-days", "0", "literal $(false) `false` {{value}}"},
		Option:     service.KeyValue{},
	}
	configureServicePlatform(config, procdServicePlatform)
	script := config.Option["SysvScript"].(string)
	if strings.Contains(script, "{{") {
		t.Fatal("literal path interpreted as a service template")
	}
	// Execute with a fake procd API, never install or start an actual service.
	script += `
procd_open_instance() { :; }
procd_close_instance() { :; }
procd_set_param() { printf '%s\000' "$@"; }
start_service
`
	command := exec.Command(shell)
	command.Env = append(os.Environ(), "MSYS_NO_PATHCONV=1", "MSYS2_ARG_CONV_EXCL=*")
	command.Stdin = strings.NewReader(script)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v: %s", err, output)
	}
	want := append([]string{"command", config.Executable}, config.Arguments...)
	want = append(want, "respawn", "3600", "5", "5", "term_timeout", "35", "stdout", "1", "stderr", "1", "")
	got := strings.Split(string(output), "\x00")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("procd arguments: %#v, want %#v", got, want)
	}
}
