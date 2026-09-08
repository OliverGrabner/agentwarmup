//go:build !windows

package config

import (
	"os"
	"path/filepath"
)

func replaceFile(source, destination string) error {
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	f, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
