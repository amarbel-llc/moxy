package main

import (
	"fmt"
	"os"
	"path/filepath"
)

//go:generate tommy generate
type ManeaterConfig struct {
	Default string                 `toml:"default"`
	Models  map[string]ModelConfig `toml:"models"`
}

type ModelConfig struct {
	Path           string `toml:"path"`
	QueryPrefix    string `toml:"query-prefix"`
	DocumentPrefix string `toml:"document-prefix"`
}

func configPath() string {
	if v := os.Getenv("MANEATER_CONFIG"); v != "" {
		return v
	}

	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}

	return filepath.Join(dir, "maneater", "models.toml")
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
