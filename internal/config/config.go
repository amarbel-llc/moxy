package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/tommy/pkg/cst"
	"github.com/amarbel-llc/tommy/pkg/document"
)

type Config struct {
	Servers []ServerConfig `toml:"servers"`
}

type ServerConfig struct {
	Name          string            `toml:"name"`
	Command       Command           `toml:"command"`
	Annotations   *AnnotationFilter `toml:"annotations"`
	Paginate      bool              `toml:"paginate"`
	GenerateResourceTools *bool `toml:"generate-resource-tools"`
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

	doc, err := document.Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("parsing moxyfile: %w", err)
	}

	nodes := doc.FindArrayTableNodes("servers")
	if len(nodes) == 0 {
		return Config{}, nil
	}

	cfg := Config{
		Servers: make([]ServerConfig, len(nodes)),
	}
	for i, node := range nodes {
		name, _ := document.GetFromContainer[string](doc, node, "name")

		cfg.Servers[i] = ServerConfig{
			Name:    name,
			Command: parseCommandFromNode(doc, node),
		}

		cfg.Servers[i].Annotations = parseAnnotations(doc, node)

		paginate, _ := document.GetFromContainer[bool](doc, node, "paginate")
		cfg.Servers[i].Paginate = paginate

		if rt, err := document.GetFromContainer[bool](doc, node, "generate-resource-tools"); err == nil {
			cfg.Servers[i].GenerateResourceTools = &rt
		}
	}
	return cfg, nil
}

func parseCommandFromNode(doc *document.Document, node *cst.Node) Command {
	// Try array form first: command = ["lux", "--lsp-dir", "/path"]
	if parts, err := document.GetFromContainer[[]string](doc, node, "command"); err == nil {
		return MakeCommand(parts...)
	}
	// Fall back to string form: command = "grit mcp --verbose"
	if s, err := document.GetFromContainer[string](doc, node, "command"); err == nil {
		return MakeCommand(strings.Fields(s)...)
	}
	return Command{}
}

func parseAnnotations(
	doc *document.Document,
	node *cst.Node,
) *AnnotationFilter {
	var af AnnotationFilter
	var found bool

	if v, err := document.GetFromContainer[bool](doc, node, "readOnlyHint"); err == nil {
		af.ReadOnlyHint = &v
		found = true
	}
	if v, err := document.GetFromContainer[bool](doc, node, "destructiveHint"); err == nil {
		af.DestructiveHint = &v
		found = true
	}
	if v, err := document.GetFromContainer[bool](doc, node, "idempotentHint"); err == nil {
		af.IdempotentHint = &v
		found = true
	}
	if v, err := document.GetFromContainer[bool](doc, node, "openWorldHint"); err == nil {
		af.OpenWorldHint = &v
		found = true
	}

	if !found {
		return nil
	}
	return &af
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
