package storage

import (
	"path/filepath"
)

// Paths resolves application-relative paths against a fixed base directory.
type Paths struct {
	baseDir string
}

func NewPaths(baseDir string) *Paths {
	return &Paths{baseDir: filepath.ToSlash(filepath.Clean(baseDir))}
}

func (p *Paths) BaseDir() string {
	return p.baseDir
}

func (p *Paths) Resolve(path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.baseDir, path)
	}
	return filepath.ToSlash(filepath.Clean(path))
}
