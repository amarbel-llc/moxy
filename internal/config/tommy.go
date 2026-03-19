package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/amarbel-llc/tommy/pkg/marshal"
)

// moxyfileConfig is the tommy-compatible representation of a moxyfile.
// Uses plain types that tommy's reflection-based marshal supports.
type moxyfileConfig struct {
	Servers []moxyfileServer `toml:"servers"`
}

// moxyfileServer is the tommy-compatible representation of a server entry.
// Command is a plain string (tommy can't handle custom UnmarshalTOML).
// Annotations are excluded because tommy writes all struct fields including
// zero-value bools, which would pollute the moxyfile. Annotations are handled
// separately via the document API in WriteServer.
type moxyfileServer struct {
	Name    string `toml:"name"`
	Command string `toml:"command"`
}

func toMoxyfileServer(srv ServerConfig) moxyfileServer {
	return moxyfileServer{
		Name:    srv.Name,
		Command: srv.Command.String(),
	}
}

func WriteServer(path string, srv ServerConfig) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var mf moxyfileConfig
	handle, err := marshal.UnmarshalDocument(data, &mf)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	newEntry := toMoxyfileServer(srv)

	found := false
	for i, s := range mf.Servers {
		if s.Name == srv.Name {
			mf.Servers[i] = newEntry
			found = true
			break
		}
	}
	if !found {
		mf.Servers = append(mf.Servers, newEntry)
	}

	out, err := marshal.MarshalDocument(handle, &mf)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}

	return os.WriteFile(path, out, 0o644)
}
