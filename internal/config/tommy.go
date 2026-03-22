package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

func WriteServer(path string, srv ServerConfig) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	doc, err := DecodeConfig(data)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	cfg := doc.Data()

	found := false
	for i, s := range cfg.Servers {
		if s.Name == srv.Name {
			cfg.Servers[i] = srv
			found = true
			break
		}
	}
	if !found {
		cfg.Servers = append(cfg.Servers, srv)
	}

	out, err := doc.Encode()
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}

	return os.WriteFile(path, out, 0o644)
}
