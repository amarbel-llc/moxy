package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Servers map[string]ServerConfig `toml:"servers"`
}

type ServerConfig struct {
	Command     string            `toml:"command"`
	Args        []string          `toml:"args"`
	Annotations *AnnotationFilter `toml:"annotations"`
}

type AnnotationFilter struct {
	ReadOnlyHint    *bool `toml:"readOnlyHint"`
	DestructiveHint *bool `toml:"destructiveHint"`
	IdempotentHint  *bool `toml:"idempotentHint"`
	OpenWorldHint   *bool `toml:"openWorldHint"`
}

type LoadSource struct {
	Path  string
	Found bool
	File  Config
}

type Hierarchy struct {
	Sources []LoadSource
	Merged  Config
}

func Parse(data []byte) (Config, error) {
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing moxyfile: %w", err)
	}
	return cfg, nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("reading moxyfile: %w", err)
	}
	return Parse(data)
}

func Merge(base, overlay Config) Config {
	merged := base

	if overlay.Servers != nil {
		if merged.Servers == nil {
			merged.Servers = make(map[string]ServerConfig)
		}
		for name, srv := range overlay.Servers {
			merged.Servers[name] = srv
		}
	}

	return merged
}

func LoadDefaultHierarchy() (Hierarchy, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Hierarchy{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return Hierarchy{}, err
	}

	return LoadHierarchy(home, cwd)
}

func LoadHierarchy(home, dir string) (Hierarchy, error) {
	var sources []LoadSource
	merged := Config{}

	loadAndMerge := func(path string) error {
		cfg, err := Load(path)
		if err != nil {
			return err
		}
		_, found := fileExists(path)
		sources = append(sources, LoadSource{Path: path, Found: found, File: cfg})
		if found {
			merged = Merge(merged, cfg)
		}
		return nil
	}

	// 1. Global config
	globalPath := filepath.Join(home, ".config", "moxy", "moxyfile")
	if err := loadAndMerge(globalPath); err != nil {
		return Hierarchy{}, err
	}

	// 2. Intermediate parent directories walking down from home to dir
	cleanHome := filepath.Clean(home)
	cleanDir := filepath.Clean(dir)

	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for i := 1; i < len(parts); i++ {
			parentDir := filepath.Join(cleanHome, filepath.Join(parts[:i]...))
			parentPath := filepath.Join(parentDir, "moxyfile")
			if err := loadAndMerge(parentPath); err != nil {
				return Hierarchy{}, err
			}
		}
	}

	// 3. Target directory moxyfile
	dirPath := filepath.Join(cleanDir, "moxyfile")
	if err := loadAndMerge(dirPath); err != nil {
		return Hierarchy{}, err
	}

	return Hierarchy{Sources: sources, Merged: merged}, nil
}

func fileExists(path string) (os.FileInfo, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	return info, true
}
