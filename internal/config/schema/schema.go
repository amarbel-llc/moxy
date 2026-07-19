// Package schema holds the moxyfile TOML data model and its tommy-generated
// codec. It deliberately contains no callers of the generated symbols
// (DecodeConfig/ConfigDocument/Encode) so that deleting or regenerating
// schema_tommy.go always leaves this package type-checkable — which is what
// `tommy generate` requires to analyze the package. The orchestration that
// consumes the codec lives in the parent config package.
package schema

import (
	"fmt"
	"os"
	"strings"

	"code.linenisgreat.com/moxy/internal/credentials"
)

//go:generate tommy generate
type Config struct {
	Ephemeral             *bool                      `toml:"ephemeral"`
	ProgressiveDisclosure *bool                      `toml:"progressive-disclosure"`
	BuiltinNative         *bool                      `toml:"builtin-native"`
	DisableMoxins         []string                   `toml:"disable-moxins,omitempty"`
	DisableServers        []string                   `toml:"disable-servers,omitempty"`
	Include               []string                   `toml:"include,omitempty"`
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

// DisableMoxinSet provides O(1) lookups for disabled moxins and moxin tools.
type DisableMoxinSet struct {
	servers map[string]bool // bare names like "chix"
	tools   map[string]bool // dotted names like "folio.write"
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

// DisableServerSet provides O(1) lookups for disabled [[servers]] entries.
// Whole-server only — per-tool granularity is rejected at parse time.
type DisableServerSet struct {
	servers map[string]bool // bare names like "dodder"
}

// BuildDisableServerSet returns a set keyed by server name for fast lookup.
func (c Config) BuildDisableServerSet() DisableServerSet {
	s := DisableServerSet{servers: make(map[string]bool, len(c.DisableServers))}
	for _, entry := range c.DisableServers {
		s.servers[entry] = true
	}
	return s
}

// ServerDisabled reports whether a configured [[servers]] entry is disabled.
func (s DisableServerSet) ServerDisabled(name string) bool {
	return s.servers[name]
}
