package main

import (
	"os"
	"path/filepath"
)

// atomicWrite writes content to path via a temp file and rename. This prevents
// partial writes if the process is killed mid-write. The temp file is created
// in the same directory to ensure same-filesystem rename.
func atomicWrite(path string, content []byte) error {
	if err := ensureParentDir(path); err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".folio-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Preserve permissions of existing file if it exists.
	if info, err := os.Stat(path); err == nil {
		os.Chmod(tmpPath, info.Mode())
	} else {
		os.Chmod(tmpPath, 0o644)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0o755)
}
