package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/moxy/internal/credentials"
)

//go:generate tommy generate
type Config struct {
	Ephemeral             *bool                      `toml:"ephemeral"`
	ProgressiveDisclosure *bool                      `toml:"progressive-disclosure"`
	BuiltinNative         *bool                      `toml:"builtin-native"`
	DisableMoxins         []string                   `toml:"disable-moxins"`
	Credentials           *credentials.CommandConfig `toml:"credentials"`
	Servers               []ServerConfig             `toml:"servers"`
}

type ServerConfig struct {
	Name                  string            `toml:"name"`
	Command               Command           `toml:"command"`
	URL                   string            `toml:"url"`
	Headers               map[string]string `toml:"headers"`
	HeadersHelper         *string           `toml:"headers-helper"`
	OAuth                 *OAuthConfig      `toml:"oauth"`
	Annotations           *AnnotationFilter `toml:"annotations"`
	Paginate              bool              `toml:"paginate"`
	GenerateResourceTools *bool             `toml:"generate-resource-tools"`
	Ephemeral             *bool             `toml:"ephemeral"`
	ProgressiveDisclosure *bool             `toml:"progressive-disclosure"`
	NixDevshell           *string           `toml:"nix-devshell"`
}

// OAuthConfig holds OAuth 2.1 configuration for HTTP servers.
type OAuthConfig struct {
	ClientID     string `toml:"client-id"`
	CallbackPort int    `toml:"callback-port"`
}

// IsHTTP reports whether this server is an HTTP (URL-based) server.
func (s ServerConfig) IsHTTP() bool {
	return s.URL != ""
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

func expandPath(s string) string {
	s = os.ExpandEnv(s)
	if strings.HasPrefix(s, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			s = home + s[1:]
		}
	}
	return s
}

func (c Command) Executable() string {
	if len(c.parts) == 0 {
		return ""
	}
	return expandPath(c.parts[0])
}

func (c Command) Args() []string {
	if len(c.parts) <= 1 {
		return nil
	}
	expanded := make([]string, len(c.parts)-1)
	for i, p := range c.parts[1:] {
		expanded[i] = expandPath(p)
	}
	return expanded
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

// DisableMoxinSet provides O(1) lookups for disabled moxins and moxin tools.
type DisableMoxinSet struct {
	servers map[string]bool // bare names like "chix"
	tools   map[string]bool // dotted names like "man.semantic-search"
}

// BuildDisableMoxinSet partitions DisableMoxins into whole-server and
// per-tool sets for efficient lookup.
func (c Config) BuildDisableMoxinSet() DisableMoxinSet {
	s := DisableMoxinSet{
		servers: make(map[string]bool),
		tools:   make(map[string]bool),
	}
	for _, entry := range c.DisableMoxins {
		if strings.Contains(entry, ".") {
			s.tools[entry] = true
		} else {
			s.servers[entry] = true
		}
	}
	return s
}

// ServerDisabled reports whether an entire moxin server is disabled.
func (s DisableMoxinSet) ServerDisabled(name string) bool {
	return s.servers[name]
}

// ToolDisabled reports whether a specific tool within a moxin is disabled.
func (s DisableMoxinSet) ToolDisabled(serverName, toolName string) bool {
	return s.tools[serverName+"."+toolName]
}
