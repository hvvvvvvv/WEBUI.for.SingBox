package main

import (
	"embed"
	"os"
)

//go:embed all:frontend/dist
var assets embed.FS

var version = "unknown"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
