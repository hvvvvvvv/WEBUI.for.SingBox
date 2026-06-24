package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func ReadYAML[T any](paths *Paths, path string) (T, error) {
	var value T
	data, err := os.ReadFile(paths.Resolve(path))
	if err != nil {
		if os.IsNotExist(err) {
			return value, nil
		}
		return value, err
	}
	if len(data) == 0 {
		return value, nil
	}
	if err := yaml.Unmarshal(data, &value); err != nil {
		return value, err
	}
	return value, nil
}

func WriteYAML(paths *Paths, path string, value any) error {
	fullPath := paths.Resolve(path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return fmt.Errorf("write yaml: %w", err)
	}
	return nil
}
