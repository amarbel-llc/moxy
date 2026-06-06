package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/moxy/internal/config/schema"
)

// The moxyfile TOML data model and its tommy-generated codec live in the
// schema subpackage. They are isolated there so that regenerating the
// generated file never depends on (and never breaks) the code that consumes
// it. These aliases keep existing config.* references working unchanged.
type (
	Config           = schema.Config
	ServerConfig     = schema.ServerConfig
	OAuthConfig      = schema.OAuthConfig
	AnnotationFilter = schema.AnnotationFilter
	Command          = schema.Command
	DisableMoxinSet  = schema.DisableMoxinSet
	DisableServerSet = schema.DisableServerSet
	ConfigDocument   = schema.ConfigDocument
)

var (
	MakeCommand  = schema.MakeCommand
	DecodeConfig = schema.DecodeConfig
)

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
	if len(data) == 0 {
		return Config{}, nil
	}

	doc, err := DecodeConfig(data)
	if err != nil {
		return Config{}, fmt.Errorf("parsing moxyfile: %w", err)
	}

	cfg := doc.Data()

	for _, entry := range cfg.DisableMoxins {
		if entry == "" {
			return Config{}, fmt.Errorf("disable-moxins: entries must not be empty")
		}
		parts := strings.SplitN(entry, ".", 2)
		if strings.Contains(parts[0], ".") {
			return Config{}, fmt.Errorf("disable-moxins: invalid entry %q", entry)
		}
		if len(parts) == 2 && parts[1] == "" {
			return Config{}, fmt.Errorf("disable-moxins: invalid entry %q (tool name is empty)", entry)
		}
		if len(parts) == 2 && strings.Contains(parts[1], ".") {
			return Config{}, fmt.Errorf("disable-moxins: invalid entry %q (tool names must not contain dots)", entry)
		}
	}

	// disable-servers is whole-server only. Per-tool granularity is not
	// supported for [[servers]] entries because their tool lists come from
	// the initialize handshake at runtime rather than static config.
	for _, entry := range cfg.DisableServers {
		if entry == "" {
			return Config{}, fmt.Errorf("disable-servers: entries must not be empty")
		}
		if strings.Contains(entry, ".") {
			return Config{}, fmt.Errorf("disable-servers: invalid entry %q (per-tool disable not supported; use the bare server name)", entry)
		}
	}

	for _, srv := range cfg.Servers {
		if strings.Contains(srv.Name, ".") {
			return Config{}, fmt.Errorf(
				"server name %q must not contain '.' (dots are used as the namespace separator)",
				srv.Name,
			)
		}

		hasCommand := !srv.Command.IsEmpty()
		hasURL := srv.URL != ""

		if hasCommand && hasURL {
			return Config{}, fmt.Errorf(
				"server %q has both command and url (only one is allowed)",
				srv.Name,
			)
		}

		if hasURL {
			if _, err := url.ParseRequestURI(srv.URL); err != nil {
				return Config{}, fmt.Errorf(
					"server %q has invalid url %q: %w",
					srv.Name, srv.URL, err,
				)
			}
			if srv.NixDevshell != nil {
				return Config{}, fmt.Errorf(
					"server %q: nix-devshell is not valid for url servers",
					srv.Name,
				)
			}
			if srv.Ephemeral != nil && *srv.Ephemeral {
				return Config{}, fmt.Errorf(
					"server %q: ephemeral is not supported for url servers",
					srv.Name,
				)
			}
		}

		if !hasURL {
			if srv.Headers != nil {
				return Config{}, fmt.Errorf(
					"server %q: headers is only valid for url servers",
					srv.Name,
				)
			}
			if srv.HeadersHelper != nil {
				return Config{}, fmt.Errorf(
					"server %q: headers-helper is only valid for url servers",
					srv.Name,
				)
			}
			if srv.OAuth != nil {
				return Config{}, fmt.Errorf(
					"server %q: oauth is only valid for url servers",
					srv.Name,
				)
			}
		}

		// Expand environment variables in header values
		for k, v := range srv.Headers {
			srv.Headers[k] = os.ExpandEnv(v)
		}
	}

	return *cfg, nil
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

	if overlay.Ephemeral != nil {
		merged.Ephemeral = overlay.Ephemeral
	}

	if overlay.ProgressiveDisclosure != nil {
		merged.ProgressiveDisclosure = overlay.ProgressiveDisclosure
	}

	if overlay.BuiltinNative != nil {
		merged.BuiltinNative = overlay.BuiltinNative
	}

	if len(overlay.DisableMoxins) > 0 {
		seen := make(map[string]bool, len(merged.DisableMoxins))
		for _, d := range merged.DisableMoxins {
			seen[d] = true
		}
		for _, d := range overlay.DisableMoxins {
			if !seen[d] {
				merged.DisableMoxins = append(merged.DisableMoxins, d)
				seen[d] = true
			}
		}
	}

	if len(overlay.DisableServers) > 0 {
		seen := make(map[string]bool, len(merged.DisableServers))
		for _, d := range merged.DisableServers {
			seen[d] = true
		}
		for _, d := range overlay.DisableServers {
			if !seen[d] {
				merged.DisableServers = append(merged.DisableServers, d)
				seen[d] = true
			}
		}
	}

	for _, srv := range overlay.Servers {
		found := false
		for i, existing := range merged.Servers {
			if existing.Name == srv.Name {
				merged.Servers[i] = srv
				found = true
				break
			}
		}
		if !found {
			merged.Servers = append(merged.Servers, srv)
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
		sources = append(
			sources,
			LoadSource{Path: path, Found: found, File: cfg},
		)
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
