package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:generate tommy generate
type FolioConfig struct {
	Permissions *PermissionConfig `toml:"permissions"`
	Read        *ReadConfig       `toml:"read"`
}

type PermissionConfig struct {
	Allow []PathRule `toml:"allow"`
	Deny  []PathRule `toml:"deny"`
}

type PathRule struct {
	Path []string `toml:"path"`
}

type ReadConfig struct {
	MaxLines  int `toml:"max-lines"`
	HeadLines int `toml:"head-lines"`
	TailLines int `toml:"tail-lines"`
}

func defaultReadConfig() ReadConfig {
	return ReadConfig{
		MaxLines:  2000,
		HeadLines: 50,
		TailLines: 20,
	}
}

func effectiveReadConfig(cfg *ReadConfig) ReadConfig {
	d := defaultReadConfig()
	if cfg == nil {
		return d
	}
	if cfg.MaxLines > 0 {
		d.MaxLines = cfg.MaxLines
	}
	if cfg.HeadLines > 0 {
		d.HeadLines = cfg.HeadLines
	}
	if cfg.TailLines > 0 {
		d.TailLines = cfg.TailLines
	}
	return d
}

func loadFolioFile(path string) (FolioConfig, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return FolioConfig{}, false, nil
		}
		return FolioConfig{}, false, fmt.Errorf("reading %s: %w", path, err)
	}

	doc, err := DecodeFolioConfig(data)
	if err != nil {
		return FolioConfig{}, false, fmt.Errorf("parsing %s: %w", path, err)
	}

	return *doc.Data(), true, nil
}

// MergeConfig combines base and overlay configs. Permission rules accumulate
// (both allow and deny lists are appended). Read config scalars are overwritten
// by overlay if set.
func MergeConfig(base, overlay FolioConfig) FolioConfig {
	merged := base

	// Accumulate permission rules.
	if overlay.Permissions != nil {
		if merged.Permissions == nil {
			cp := *overlay.Permissions
			merged.Permissions = &cp
		} else {
			mergedPerms := *merged.Permissions
			mergedPerms.Allow = append(mergedPerms.Allow, overlay.Permissions.Allow...)
			mergedPerms.Deny = append(mergedPerms.Deny, overlay.Permissions.Deny...)
			merged.Permissions = &mergedPerms
		}
	}

	// Read config: overlay scalars win.
	if overlay.Read != nil {
		if merged.Read == nil {
			cp := *overlay.Read
			merged.Read = &cp
		} else {
			mergedRead := *merged.Read
			if overlay.Read.MaxLines > 0 {
				mergedRead.MaxLines = overlay.Read.MaxLines
			}
			if overlay.Read.HeadLines > 0 {
				mergedRead.HeadLines = overlay.Read.HeadLines
			}
			if overlay.Read.TailLines > 0 {
				mergedRead.TailLines = overlay.Read.TailLines
			}
			merged.Read = &mergedRead
		}
	}

	return merged
}

// LoadFolioHierarchy loads and merges folio.toml files from:
//  1. ~/.config/folio/folio.toml (global)
//  2. Each parent directory between home and dir
//  3. ./folio.toml (project-local)
func LoadFolioHierarchy(home, dir string) (FolioConfig, error) {
	merged := FolioConfig{}

	loadAndMerge := func(path string) error {
		cfg, found, err := loadFolioFile(path)
		if err != nil {
			return err
		}
		if found {
			merged = MergeConfig(merged, cfg)
		}
		return nil
	}

	// 1. Global config.
	globalPath := filepath.Join(home, ".config", "folio", "folio.toml")
	if err := loadAndMerge(globalPath); err != nil {
		return FolioConfig{}, err
	}

	// 2. Intermediate parent directories walking down from home to dir.
	cleanHome := filepath.Clean(home)
	cleanDir := filepath.Clean(dir)

	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for i := 1; i < len(parts); i++ {
			parentDir := filepath.Join(cleanHome, filepath.Join(parts[:i]...))
			parentPath := filepath.Join(parentDir, "folio.toml")
			if err := loadAndMerge(parentPath); err != nil {
				return FolioConfig{}, err
			}
		}
	}

	// 3. Target directory folio.toml.
	dirPath := filepath.Join(cleanDir, "folio.toml")
	if err := loadAndMerge(dirPath); err != nil {
		return FolioConfig{}, err
	}

	return merged, nil
}

// LoadDefaultFolioHierarchy is a convenience wrapper using the real home
// directory and working directory.
func LoadDefaultFolioHierarchy() (FolioConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return FolioConfig{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return FolioConfig{}, err
	}

	return LoadFolioHierarchy(home, cwd)
}
