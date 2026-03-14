package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Servers map[string]ServerConfig `toml:"servers"`
}

type ServerConfig struct {
	Command     string           `toml:"command"`
	Args        []string         `toml:"args"`
	Annotations *AnnotationFilter `toml:"annotations"`
}

type AnnotationFilter struct {
	ReadOnlyHint    *bool `toml:"readOnlyHint"`
	DestructiveHint *bool `toml:"destructiveHint"`
	IdempotentHint  *bool `toml:"idempotentHint"`
	OpenWorldHint   *bool `toml:"openWorldHint"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading moxyfile: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing moxyfile: %w", err)
	}

	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("moxyfile has no servers configured")
	}

	for name, srv := range cfg.Servers {
		if srv.Command == "" {
			return nil, fmt.Errorf("server %q has no command", name)
		}
	}

	return &cfg, nil
}
