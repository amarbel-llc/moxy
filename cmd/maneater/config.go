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
type ManeaterConfig struct {
	Default string                 `toml:"default"`
	Models  map[string]ModelConfig `toml:"models"`
	Exec    *ExecConfig            `toml:"exec"`
}

type ModelConfig struct {
	Path           string `toml:"path"`
	QueryPrefix    string `toml:"query-prefix"`
	DocumentPrefix string `toml:"document-prefix"`
}

func globalConfigDir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "maneater")
}

func loadManeaterFile(path string) (ManeaterConfig, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ManeaterConfig{}, false, nil
		}
		return ManeaterConfig{}, false, fmt.Errorf("reading %s: %w", path, err)
	}

	doc, err := DecodeManeaterConfig(data)
	if err != nil {
		return ManeaterConfig{}, false, fmt.Errorf("parsing %s: %w", path, err)
	}

	return *doc.Data(), true, nil
}

// MergeConfig combines base and overlay configs. Models are merged by name
// (overlay wins per key). Exec rules accumulate (both allow and deny lists
// are appended). Scalar fields (Default) are overwritten by overlay if set.
func MergeConfig(base, overlay ManeaterConfig) ManeaterConfig {
	merged := base

	if overlay.Default != "" {
		merged.Default = overlay.Default
	}

	// Merge models: overlay wins per key.
	if len(overlay.Models) > 0 {
		if merged.Models == nil {
			merged.Models = make(map[string]ModelConfig)
		}
		for k, v := range overlay.Models {
			merged.Models[k] = v
		}
	}

	// Accumulate exec rules.
	if overlay.Exec != nil {
		if merged.Exec == nil {
			cp := *overlay.Exec
			merged.Exec = &cp
		} else {
			mergedExec := *merged.Exec
			mergedExec.Allow = append(mergedExec.Allow, overlay.Exec.Allow...)
			mergedExec.Deny = append(mergedExec.Deny, overlay.Exec.Deny...)
			merged.Exec = &mergedExec
		}
	}

	return merged
}

// LoadManeaterHierarchy loads and merges maneater.toml files from:
//  1. ~/.config/maneater/maneater.toml (global)
//  2. Each parent directory between home and dir
//  3. ./maneater.toml (project-local)
//
// Falls back to ~/.config/maneater/models.toml at the global level if
// maneater.toml doesn't exist there (backward compatibility).
func LoadManeaterHierarchy(home, dir string) (ManeaterConfig, error) {
	merged := ManeaterConfig{}

	loadAndMerge := func(path string) error {
		cfg, found, err := loadManeaterFile(path)
		if err != nil {
			return err
		}
		if found {
			merged = MergeConfig(merged, cfg)
		}
		return nil
	}

	// 1. Global config: try maneater.toml first, fall back to models.toml.
	globalDir := filepath.Join(home, ".config", "maneater")
	globalPath := filepath.Join(globalDir, "maneater.toml")
	cfg, found, err := loadManeaterFile(globalPath)
	if err != nil {
		return ManeaterConfig{}, err
	}
	if found {
		merged = MergeConfig(merged, cfg)
	} else {
		// Fallback: models.toml for backward compatibility.
		fallbackPath := filepath.Join(globalDir, "models.toml")
		if err := loadAndMerge(fallbackPath); err != nil {
			return ManeaterConfig{}, err
		}
	}

	// 2. Intermediate parent directories walking down from home to dir.
	cleanHome := filepath.Clean(home)
	cleanDir := filepath.Clean(dir)

	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for i := 1; i < len(parts); i++ {
			parentDir := filepath.Join(cleanHome, filepath.Join(parts[:i]...))
			parentPath := filepath.Join(parentDir, "maneater.toml")
			if err := loadAndMerge(parentPath); err != nil {
				return ManeaterConfig{}, err
			}
		}
	}

	// 3. Target directory maneater.toml.
	dirPath := filepath.Join(cleanDir, "maneater.toml")
	if err := loadAndMerge(dirPath); err != nil {
		return ManeaterConfig{}, err
	}

	expandEnvInModels(&merged)

	return merged, nil
}

// expandEnvInModels expands $VAR and ${VAR} references in model path fields.
func expandEnvInModels(cfg *ManeaterConfig) {
	for k, m := range cfg.Models {
		if m.Path != "" {
			m.Path = os.ExpandEnv(m.Path)
			cfg.Models[k] = m
		}
	}
}

// LoadDefaultManeaterHierarchy is a convenience wrapper using the real home
// directory and working directory.
func LoadDefaultManeaterHierarchy() (ManeaterConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ManeaterConfig{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ManeaterConfig{}, err
	}

	return LoadManeaterHierarchy(home, cwd)
}

func configPath() string {
	if v := os.Getenv("MANEATER_CONFIG"); v != "" {
		return v
	}
	return filepath.Join(globalConfigDir(), "models.toml")
}

func loadActiveModel() (name string, model ModelConfig, err error) {
	path := configPath()

	data, err := os.ReadFile(path)
	if err != nil {
		return "", ModelConfig{}, fmt.Errorf(
			"reading config %s: %w\n\nCreate a models.toml with at least one [models.<name>] entry",
			path, err,
		)
	}

	doc, err := DecodeManeaterConfig(data)
	if err != nil {
		return "", ModelConfig{}, fmt.Errorf("parsing config %s: %w", path, err)
	}

	cfg := doc.Data()

	if len(cfg.Models) == 0 {
		return "", ModelConfig{}, fmt.Errorf("config %s has no [models.*] entries", path)
	}

	name = cfg.Default
	if name == "" {
		if len(cfg.Models) == 1 {
			for k := range cfg.Models {
				name = k
			}
		} else {
			return "", ModelConfig{}, fmt.Errorf(
				"config %s has multiple models but no 'default' key", path,
			)
		}
	}

	model, ok := cfg.Models[name]
	if !ok {
		return "", ModelConfig{}, fmt.Errorf(
			"config %s: default model %q not found in [models]", path, name,
		)
	}

	if model.Path == "" {
		return "", ModelConfig{}, fmt.Errorf(
			"config %s: model %q has no 'path'", path, name,
		)
	}

	return name, model, nil
}
