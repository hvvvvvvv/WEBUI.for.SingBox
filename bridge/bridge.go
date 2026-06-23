package bridge

import (
	"embed"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	sysruntime "runtime"
)

func detectLibc() string {
	if sysruntime.GOOS != "linux" {
		return ""
	}
	// Check for musl by looking for musl dynamic linker
	matches, _ := filepath.Glob("/lib/ld-musl-*")
	if len(matches) > 0 {
		return "musl"
	}
	// Also check for musl via ldd output
	out, err := exec.Command("ldd", "--version").CombinedOutput()
	if err == nil && strings.Contains(strings.ToLower(string(out)), "musl") {
		return "musl"
	}
	return "glibc"
}

var Config = &AppConfig{}

var ServerAddr string

var Env = &EnvResult{
	PreventExit:  true,
	FromTaskSch:  false,
	AppName:      "",
	AppVersion:   "1.0.0",
	BasePath:     "",
	OS:           sysruntime.GOOS,
	ARCH:         sysruntime.GOARCH,
	Libc:         detectLibc(),
	IsPrivileged: false,
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

func CreateApp(fs embed.FS) *App {
	exePath, err := os.Executable()
	if err != nil {
		panic(err)
	}

	Env.BasePath = filepath.ToSlash(filepath.Dir(exePath))
	Env.AppName = filepath.Base(exePath)

	if slices.Contains(os.Args, "tasksch") {
		Env.FromTaskSch = true
	}

	if priv, err := IsPrivileged(); err == nil {
		Env.IsPrivileged = priv
	}

	app := NewApp()
	AppInstance = app

	loadConfig()

	return app
}

func (a *App) ExitApp() {
	log.Printf("ExitApp")
	Env.PreventExit = false
	os.Exit(0)
}

func (a *App) GetEnv(key string) any {
	log.Printf("GetEnv: %s", key)
	if key != "" {
		return os.Getenv(key)
	}
	return EnvResult{
		AppName:      Env.AppName,
		AppVersion:   Env.AppVersion,
		BasePath:     Env.BasePath,
		OS:           Env.OS,
		ARCH:         Env.ARCH,
		Libc:         Env.Libc,
		IsPrivileged: Env.IsPrivileged,
	}
}

func (a *App) GetInterfaces() FlagResult {
	log.Printf("GetInterfaces")

	interfaces, err := net.Interfaces()
	if err != nil {
		return FlagResult{false, err.Error()}
	}

	var interfaceNames []string

	for _, inter := range interfaces {
		interfaceNames = append(interfaceNames, inter.Name)
	}

	return FlagResult{true, strings.Join(interfaceNames, "|")}
}
