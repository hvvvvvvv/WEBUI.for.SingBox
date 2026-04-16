package main

import (
	"embed"
	"flag"
	"fmt"
	"guiforcores/bridge"
	"os"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	addr := flag.String("addr", "0.0.0.0:9090", "HTTP server listen address")
	resetAuth := flag.String("reset-auth", "", "Reset auth secret (provide new secret, or 'clear' to remove)")
	flag.Parse()

	app := bridge.CreateApp(assets)
	_ = app

	if *resetAuth != "" {
		if *resetAuth == "clear" {
			bridge.Config.AuthSecret = ""
			fmt.Println("Auth secret cleared.")
		} else {
			bridge.Config.AuthSecret = bridge.HashSecret(*resetAuth)
			fmt.Println("Auth secret updated.")
		}
		if err := bridge.SaveConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save config: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	bridge.ServerAddr = *addr

	bridge.StartHTTPServer(*addr, assets)
}
