package native

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/amarbel-llc/tommy/pkg/cst"
	"github.com/amarbel-llc/tommy/pkg/document"
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
	StdinParam  string          `toml:"stdin_param"`
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
	StdinParam  string   `toml:"stdin_param"`
	Input       any      `toml:"input"`
}

// ParseResult holds the parsed config and any undecoded keys found in the TOML.
type ParseResult struct {
	Config    *NativeConfig
	Undecoded []string
}

func ParseConfig(data []byte) (*NativeConfig, error) {
	result, err := ParseConfigFull(data)
	if err != nil {
		return nil, err
	}
	return result.Config, nil
}

func ParseConfigFull(data []byte) (*ParseResult, error) {
	// BurntSushi/toml for data extraction (handles arbitrary tools.input).
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
			StdinParam:  rt.StdinParam,
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

	// Tommy parse for undecoded key detection.
	undecoded := detectUndecoded(data)

	return &ParseResult{Config: cfg, Undecoded: undecoded}, nil
}

// detectUndecoded parses with tommy and returns any keys not part of the
// native config schema. Returns nil on parse error (BurntSushi already
// reported it).
func detectUndecoded(data []byte) []string {
	doc, err := document.Parse(data)
	if err != nil {
		return nil
	}

	consumed := make(map[string]bool)

	// Top-level known keys.
	if _, err := document.GetFromContainer[string](doc, doc.Root(), "name"); err == nil {
		consumed["name"] = true
	}
	if _, err := document.GetFromContainer[string](doc, doc.Root(), "description"); err == nil {
		consumed["description"] = true
	}

	// [[tools]] array tables.
	toolNodes := doc.FindArrayTableNodes("tools")
	consumed["tools"] = true
	for _, node := range toolNodes {
		for _, key := range []string{"name", "description", "command", "args", "arg_order"} {
			if doc.HasInContainer(node, key) {
				consumed["tools."+key] = true
			}
		}
		// tools.input is an arbitrary JSON Schema table — consume it and
		// all subtables (e.g. tools.input.properties.recipe).
		if inputNode := doc.FindTableInContainer(node, "input"); inputNode != nil {
			consumed["tools.input"] = true
			document.MarkAllConsumed(inputNode, "tools.input", consumed)
		}
	}

	// Consume all tools.input.* subtables (deeply nested JSON Schema
	// sections like [tools.input.properties.recipe] appear as separate
	// NodeTable entries at the document root).
	for _, child := range doc.Root().Children {
		if child.Kind == cst.NodeTable {
			key := document.SubTableKey(child, "")
			if strings.HasPrefix(key, "tools.input") {
				consumed[key] = true
				document.MarkAllConsumed(child, key, consumed)
			}
		}
	}

	return document.UndecodedKeys(doc.Root(), consumed)
}
