package native

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

type NativeConfig struct {
	Name        string     `toml:"name"`
	Description string     `toml:"description"`
	Tools       []ToolSpec `toml:"tools"`
}

type ToolSpec struct {
	Name        string          `toml:"name"`
	Description string          `toml:"description"`
	Command     string          `toml:"command"`
	Args        []string        `toml:"args"`
	ArgOrder    []string        `toml:"arg_order"`
	Input       json.RawMessage `toml:"-"`
}

// rawConfig mirrors NativeConfig but uses an intermediate type for Input
// so that TOML tables decode into map[string]any before JSON marshaling.
type rawConfig struct {
	Name        string        `toml:"name"`
	Description string        `toml:"description"`
	Tools       []rawToolSpec `toml:"tools"`
}

type rawToolSpec struct {
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	Command     string   `toml:"command"`
	Args        []string `toml:"args"`
	ArgOrder    []string `toml:"arg_order"`
	Input       any      `toml:"input"`
}

func ParseConfig(data []byte) (*NativeConfig, error) {
	var raw rawConfig
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing native config: %w", err)
	}

	if raw.Name == "" {
		return nil, fmt.Errorf("native config: name is required")
	}
	if strings.Contains(raw.Name, ".") {
		return nil, fmt.Errorf("native config: name %q must not contain '.'", raw.Name)
	}

	cfg := &NativeConfig{
		Name:        raw.Name,
		Description: raw.Description,
		Tools:       make([]ToolSpec, len(raw.Tools)),
	}

	for i, rt := range raw.Tools {
		if rt.Name == "" {
			return nil, fmt.Errorf("native config %q: tool[%d] missing name", cfg.Name, i)
		}
		if rt.Command == "" {
			return nil, fmt.Errorf("native config %q: tool %q missing command", cfg.Name, rt.Name)
		}

		ts := ToolSpec{
			Name:        rt.Name,
			Description: rt.Description,
			Command:     rt.Command,
			Args:        rt.Args,
			ArgOrder:    rt.ArgOrder,
		}

		if rt.Input != nil {
			jsonBytes, err := json.Marshal(rt.Input)
			if err != nil {
				return nil, fmt.Errorf("native config %q: tool %q: marshaling input schema: %w", cfg.Name, rt.Name, err)
			}
			ts.Input = jsonBytes
		}

		cfg.Tools[i] = ts
	}

	return cfg, nil
}
