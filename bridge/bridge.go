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

	"gopkg.in/yaml.v3"
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
	IsStartup:    true,
	PreventExit:  true,
	FromTaskSch:  false,
	WebviewPath:  "",
	AppName:      "",
	AppVersion:   "v1.22.0",
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

	extractEmbeddedFiles(fs)

	loadConfig()

	return app
}

func (a *App) IsStartup() bool {
	if Env.IsStartup {
		Env.IsStartup = false
		return true
	}
	return false
}

func (a *App) ExitApp() {
	log.Printf("ExitApp")
	Env.PreventExit = false
	os.Exit(0)
}

func (a *App) RestartApp() FlagResult {
	log.Printf("RestartApp")
	exePath := Env.BasePath + "/" + Env.AppName

	cmd := exec.Command(exePath)
	SetCmdWindowHidden(cmd)

	if err := cmd.Start(); err != nil {
		return FlagResult{false, err.Error()}
	}

	a.ExitApp()

	return FlagResult{true, "Success"}
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

func (a *App) ShowMainWindow() {
	log.Printf("ShowMainWindow: no-op in server mode")
}

func extractEmbeddedFiles(fs embed.FS) {
	iconSrc := "frontend/dist/icons"
	iconDst := "data/.cache/icons"
	imgSrc := "frontend/dist/imgs"
	imgDst := "data/.cache/imgs"

	os.MkdirAll(GetPath(iconDst), os.ModePerm)
	os.MkdirAll(GetPath(imgDst), os.ModePerm)

	extractFiles(fs, iconSrc, iconDst)
	extractFiles(fs, imgSrc, imgDst)
}

func extractFiles(fs embed.FS, srcDir, dstDir string) {
	files, _ := fs.ReadDir(srcDir)
	for _, file := range files {
		fileName := file.Name()
		dstPath := GetPath(dstDir + "/" + fileName)
		if _, err := os.Stat(dstPath); os.IsNotExist(err) {
			log.Printf("InitResources [%s]: %s", dstDir, fileName)
			data, _ := fs.ReadFile(srcDir + "/" + fileName)
			if err := os.WriteFile(dstPath, data, os.ModePerm); err != nil {
				log.Printf("Error writing file %s: %v", dstPath, err)
			}
		}
	}
}

func loadConfig() {
	b, err := os.ReadFile(Env.BasePath + "/data/user.yaml")
	if err == nil {
		yaml.Unmarshal(b, &Config)
	}

	if Config.Width == 0 {
		Config.Width = 800
	}

	if Config.Height == 0 {
		Config.Height = 540
	}

	Config.StartHidden = Env.FromTaskSch && Config.WindowStartState == 2 // Minimised

	if !Env.FromTaskSch {
		Config.WindowStartState = 0 // Normal
	}
}

func SaveConfig() error {
	path := Env.BasePath + "/data/user.yaml"

	// Read existing file and merge to preserve frontend-managed fields
	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		yaml.Unmarshal(data, &existing)
	}

	// Marshal Go-managed fields into a map
	goData, err := yaml.Marshal(Config)
	if err != nil {
		return err
	}
	goFields := make(map[string]any)
	if err := yaml.Unmarshal(goData, &goFields); err != nil {
		return err
	}

	// Merge Go fields into existing (Go fields take precedence)
	for k, v := range goFields {
		existing[k] = v
	}

	// If authSecret is empty, remove it from the file
	if Config.AuthSecret == "" {
		delete(existing, "authSecret")
	}

	b, err := yaml.Marshal(existing)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
