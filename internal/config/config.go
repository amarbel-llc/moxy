package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:generate tommy generate
type Config struct {
	Ephemeral              *bool          `toml:"ephemeral"`
	ProgressiveDisclosure  *bool          `toml:"progressive-disclosure"`
	Servers                []ServerConfig `toml:"servers"`
}

type ServerConfig struct {
	Name                  string            `toml:"name"`
	Command               Command           `toml:"command"`
	Annotations           *AnnotationFilter `toml:"annotations"`
	Paginate              bool              `toml:"paginate"`
	GenerateResourceTools *bool             `toml:"generate-resource-tools"`
	Ephemeral             *bool             `toml:"ephemeral"`
	ProgressiveDisclosure *bool             `toml:"progressive-disclosure"`
	NixDevshell           *string           `toml:"nix-devshell"`
}

func (s ServerConfig) EffectiveCommand() (executable string, args []string) {
	if s.NixDevshell != nil {
		a := []string{"develop", *s.NixDevshell, "--command", s.Command.Executable()}
		a = append(a, s.Command.Args()...)
		return "nix", a
	}
	return s.Command.Executable(), s.Command.Args()
}

func (s ServerConfig) IsEphemeral(globalEphemeral *bool) bool {
	if s.Ephemeral != nil {
		return *s.Ephemeral
	}
	if globalEphemeral != nil {
		return *globalEphemeral
	}
	return false
}

func (s ServerConfig) IsProgressiveDisclosure(global *bool) bool {
	if s.ProgressiveDisclosure != nil {
		return *s.ProgressiveDisclosure
	}
	if global != nil {
		return *global
	}
	return false
}

// Command holds a shell command as either a string or an array of strings.
// String form is split on whitespace; array form is used as-is.
type Command struct {
	parts []string
}

func (c *Command) UnmarshalTOML(data any) error {
	switch v := data.(type) {
	case string:
		c.parts = strings.Fields(v)
		if len(c.parts) == 0 {
			return fmt.Errorf("command string is empty")
		}
		return nil
	case []any:
		c.parts = make([]string, len(v))
		for i, elem := range v {
			s, ok := elem.(string)
			if !ok {
				return fmt.Errorf("command array element %d is not a string", i)
			}
			c.parts[i] = s
		}
		if len(c.parts) == 0 {
			return fmt.Errorf("command array is empty")
		}
		return nil
	default:
		return fmt.Errorf("command must be a string or array of strings")
	}
}

func (c Command) MarshalTOML() (string, error) {
	return c.String(), nil
}

func (c Command) Executable() string {
	if len(c.parts) == 0 {
		return ""
	}
	return c.parts[0]
}

func (c Command) Args() []string {
	if len(c.parts) <= 1 {
		return nil
	}
	return c.parts[1:]
}

func (c Command) IsEmpty() bool {
	return len(c.parts) == 0
}

func (c Command) String() string {
	return strings.Join(c.parts, " ")
}

func MakeCommand(parts ...string) Command {
	return Command{parts: parts}
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
	if len(data) == 0 {
		return Config{}, nil
	}

	doc, err := DecodeConfig(data)
	if err != nil {
		return Config{}, fmt.Errorf("parsing moxyfile: %w", err)
	}

	cfg := doc.Data()
	for _, srv := range cfg.Servers {
		if strings.Contains(srv.Name, ".") {
			return Config{}, fmt.Errorf(
				"server name %q must not contain '.' (dots are used as the namespace separator)",
				srv.Name,
			)
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
