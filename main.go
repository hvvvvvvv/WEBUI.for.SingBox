package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"guiforcores/bridge"
	"guiforcores/bridge/appupdate"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed all:frontend/dist
var assets embed.FS

var version = "1.0.0"

func main() {
	if appupdate.IsHelperMode(os.Args[1:]) {
		opts, err := appupdate.ParseHelperOptions(os.Args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid updater helper arguments: %v\n", err)
			os.Exit(1)
		}
		if err := appupdate.RunHelper(opts); err != nil {
			fmt.Fprintf(os.Stderr, "Updater helper failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	addr := flag.String("addr", "0.0.0.0:9090", "HTTP server listen address")
	resetAuth := flag.String("reset-auth", "", "Reset auth secret (provide new secret, or 'clear' to remove)")
	flag.Parse()

	app, err := bridge.New(bridge.Options{
		Address:    *addr,
		Assets:     assets,
		AppVersion: version,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize application: %v\n", err)
		os.Exit(1)
	}

	if *resetAuth != "" {
		if *resetAuth == "clear" {
			if err := app.SetAuthSecret(""); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to clear auth secret: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Auth secret cleared.")
		} else {
			if err := app.SetAuthSecret(*resetAuth); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to update auth secret: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Auth secret updated.")
		}
		os.Exit(0)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer func() {
		closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Close(closeContext)
	}()
	if err := app.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
		os.Exit(1)
	}
}
