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
type FreudConfig struct {
	ProjectsDir string      `toml:"projects-dir"`
	List        *ListConfig `toml:"list"`
}

type ListConfig struct {
	MaxRows  int `toml:"max-rows"`
	HeadRows int `toml:"head-rows"`
	TailRows int `toml:"tail-rows"`
}

func defaultListConfig() ListConfig {
	return ListConfig{
		MaxRows:  500,
		HeadRows: 50,
		TailRows: 20,
	}
}

func effectiveListConfig(cfg *ListConfig) ListConfig {
	d := defaultListConfig()
	if cfg == nil {
		return d
	}
	if cfg.MaxRows > 0 {
		d.MaxRows = cfg.MaxRows
	}
	if cfg.HeadRows > 0 {
		d.HeadRows = cfg.HeadRows
	}
	if cfg.TailRows > 0 {
		d.TailRows = cfg.TailRows
	}
	return d
}

// effectiveProjectsDir resolves the configured projects directory, expanding
// a leading "~" to the user's home and falling back to ~/.claude/projects.
func effectiveProjectsDir(cfg FreudConfig, home string) string {
	p := cfg.ProjectsDir
	if p == "" {
		return filepath.Join(home, ".claude", "projects")
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	if p == "~" {
		return home
	}
	return p
}

func loadFreudFile(path string) (FreudConfig, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return FreudConfig{}, false, nil
		}
		return FreudConfig{}, false, fmt.Errorf("reading %s: %w", path, err)
	}

	doc, err := DecodeFreudConfig(data)
	if err != nil {
		return FreudConfig{}, false, fmt.Errorf("parsing %s: %w", path, err)
	}

	return *doc.Data(), true, nil
}

// MergeConfig combines base and overlay configs. ProjectsDir is overwritten by
// overlay if set. List config scalars are overwritten individually by overlay.
func MergeConfig(base, overlay FreudConfig) FreudConfig {
	merged := base

	if overlay.ProjectsDir != "" {
		merged.ProjectsDir = overlay.ProjectsDir
	}

	if overlay.List != nil {
		if merged.List == nil {
			cp := *overlay.List
			merged.List = &cp
		} else {
			mergedList := *merged.List
			if overlay.List.MaxRows > 0 {
				mergedList.MaxRows = overlay.List.MaxRows
			}
			if overlay.List.HeadRows > 0 {
				mergedList.HeadRows = overlay.List.HeadRows
			}
			if overlay.List.TailRows > 0 {
				mergedList.TailRows = overlay.List.TailRows
			}
			merged.List = &mergedList
		}
	}

	return merged
}

// LoadFreudHierarchy loads and merges freud.toml files from:
//  1. ~/.config/freud/freud.toml (global)
//  2. Each parent directory between home and dir
//  3. ./freud.toml (project-local)
func LoadFreudHierarchy(home, dir string) (FreudConfig, error) {
	merged := FreudConfig{}

	loadAndMerge := func(path string) error {
		cfg, found, err := loadFreudFile(path)
		if err != nil {
			return err
		}
		if found {
			merged = MergeConfig(merged, cfg)
		}
		return nil
	}

	globalPath := filepath.Join(home, ".config", "freud", "freud.toml")
	if err := loadAndMerge(globalPath); err != nil {
		return FreudConfig{}, err
	}

	cleanHome := filepath.Clean(home)
	cleanDir := filepath.Clean(dir)

	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for i := 1; i < len(parts); i++ {
			parentDir := filepath.Join(cleanHome, filepath.Join(parts[:i]...))
			parentPath := filepath.Join(parentDir, "freud.toml")
			if err := loadAndMerge(parentPath); err != nil {
				return FreudConfig{}, err
			}
		}
	}

	dirPath := filepath.Join(cleanDir, "freud.toml")
	if err := loadAndMerge(dirPath); err != nil {
		return FreudConfig{}, err
	}

	return merged, nil
}

// LoadDefaultFreudHierarchy is a convenience wrapper using the real home
// directory and working directory.
func LoadDefaultFreudHierarchy() (FreudConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return FreudConfig{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return FreudConfig{}, err
	}

	return LoadFreudHierarchy(home, cwd)
}
