package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/amarbel-llc/tommy/pkg/cst"
	"github.com/amarbel-llc/tommy/pkg/document"
)

// PermsRequest controls how the hook system handles permission for a tool.
type PermsRequest string

const (
	// PermsDelegateToClient lets the MCP client decide (default if omitted).
	PermsDelegateToClient PermsRequest = "delegate-to-client"
	// PermsAlwaysAllow skips the permission prompt.
	PermsAlwaysAllow PermsRequest = "always-allow"
	// PermsEachUse forces a user confirmation prompt every time.
	PermsEachUse PermsRequest = "each-use"
)

// ResultType controls how a tool's stdout is interpreted.
type ResultType string

const (
	// ResultTypeText wraps stdout as a text content block (default for schema 1).
	ResultTypeText ResultType = "text"
	// ResultTypeMCPResult parses stdout as a ToolCallResultV1 JSON object.
	ResultTypeMCPResult ResultType = "mcp-result"
)

// NativeConfig is the assembled output consumed by Server, proxy, and hooks.
type NativeConfig struct {
	Name        string
	Description string
	SourceDir   string
	Tools       []ToolSpec
}

// ToolAnnotations holds optional behavior hints for a tool.
type ToolAnnotations struct {
	Title           string `toml:"title"`
	ReadOnlyHint    *bool  `toml:"read-only-hint"`
	DestructiveHint *bool  `toml:"destructive-hint"`
	IdempotentHint  *bool  `toml:"idempotent-hint"`
	OpenWorldHint   *bool  `toml:"open-world-hint"`
}

// InputSchema is the typed representation of a moxin tool's [input] section.
// Only the JSON Schema keywords actually used by moxin TOML files are modeled.
type InputSchema struct {
	Type        string                    `toml:"type"        json:"type"`
	Description string                    `toml:"description" json:"description,omitempty"`
	Required    []string                  `toml:"required"    json:"required,omitempty"`
	Properties  map[string]PropertySchema `toml:"properties"  json:"properties,omitempty"`
}

// PropertySchema describes a single property within an input schema.
type PropertySchema struct {
	Type                 string     `toml:"type"                  json:"type"`
	Description          string     `toml:"description"           json:"description,omitempty"`
	Enum                 []string   `toml:"enum"                  json:"enum,omitempty"`
	Items                *SchemaRef `toml:"items"                 json:"items,omitempty"`
	AdditionalProperties *SchemaRef `toml:"additionalProperties"  json:"additionalProperties,omitempty"`
}

// SchemaRef is a leaf schema reference (for array items or map values).
type SchemaRef struct {
	Type        string `toml:"type"        json:"type"`
	Description string `toml:"description" json:"description,omitempty"`
}

// ToolSpec describes a single tool within a moxin.
type ToolSpec struct {
	Name                 string
	Description          string
	Command              string
	Args                 []string
	ArgOrder             []string
	StdinParam           string
	PermsRequest         PermsRequest
	ContentType          string
	ResultType           ResultType
	SubstituteResultURIs *bool
	Annotations          *ToolAnnotations
	Input                json.RawMessage
	InputParsed          *InputSchema
}

// ShouldSubstituteURIs reports whether moxy.native:// result URIs in this
// tool's arguments should be rewritten to /dev/fd/N pipes.
func (s *ToolSpec) ShouldSubstituteURIs() bool {
	return s.SubstituteResultURIs == nil || *s.SubstituteResultURIs
}

// MoxinMeta is the parsed content of _moxin.toml.
type MoxinMeta struct {
	Schema      int    `toml:"schema"`
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

// rawToolFile mirrors the per-tool TOML file for initial decode.
// Struct tags use kebab-case (schema 3). For schema 1/2 backward
// compatibility, legacyToolKeys handles the old snake_case spellings.
type rawToolFile struct {
	Schema               int              `toml:"schema"`
	Name                 string           `toml:"name"`
	Description          string           `toml:"description"`
	Command              string           `toml:"command"`
	Args                 []string         `toml:"args"`
	ArgOrder             []string         `toml:"arg-order"`
	StdinParam           string           `toml:"stdin-param"`
	PermsRequest         PermsRequest     `toml:"perms-request"`
	ContentType          string           `toml:"content-type"`
	ResultType           string           `toml:"result-type"`
	SubstituteResultURIs *bool            `toml:"substitute-result-uris"`
	Annotations          *ToolAnnotations `toml:"annotations"`
	Input                *InputSchema     `toml:"input"`
}

// legacyToolKeys captures the old snake_case key spellings from schema 1/2.
type legacyToolKeys struct {
	ArgOrder   []string `toml:"arg_order"`
	StdinParam string   `toml:"stdin_param"`
}

// ParseResult holds the parsed config and any undecoded keys found in the TOML files.
type ParseResult struct {
	Config    *NativeConfig
	Undecoded []string
	Warnings  []string
}

// ParseMoxinDir parses a moxin directory containing _moxin.toml + per-tool files.
func ParseMoxinDir(dirPath string) (*NativeConfig, error) {
	result, err := ParseMoxinDirFull(dirPath)
	if err != nil {
		return nil, err
	}
	return result.Config, nil
}

// ParseMoxinDirFull parses a moxin directory and reports undecoded keys.
func ParseMoxinDirFull(dirPath string) (*ParseResult, error) {
	debugMoxin("ParseMoxinDirFull: parsing %s", dirPath)
	// Parse _moxin.toml metadata.
	metaPath := filepath.Join(dirPath, "_moxin.toml")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("reading _moxin.toml: %w", err)
	}

	var meta MoxinMeta
	if err := toml.Unmarshal(metaData, &meta); err != nil {
		return nil, fmt.Errorf("parsing _moxin.toml: %w", err)
	}
	if meta.Schema != 1 {
		return nil, fmt.Errorf("_moxin.toml: unsupported schema %d (want 1)", meta.Schema)
	}
	if meta.Name == "" {
		return nil, fmt.Errorf("_moxin.toml: name is required")
	}
	if strings.Contains(meta.Name, ".") {
		return nil, fmt.Errorf("_moxin.toml: name %q must not contain '.'", meta.Name)
	}

	var allUndecoded []string
	var allWarnings []string
	allUndecoded = append(allUndecoded, detectUndecodedMeta(metaData)...)

	// Scan for tool files (*.toml excluding _moxin.toml).
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("reading moxin directory: %w", err)
	}

	var toolFiles []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") || e.Name() == "_moxin.toml" {
			continue
		}
		toolFiles = append(toolFiles, e.Name())
	}
	sort.Strings(toolFiles)
	debugMoxin("ParseMoxinDirFull: %s: found %d tool files: %v", meta.Name, len(toolFiles), toolFiles)

	cfg := &NativeConfig{
		Name:        meta.Name,
		Description: meta.Description,
		Tools:       make([]ToolSpec, 0, len(toolFiles)),
	}

	for _, filename := range toolFiles {
		toolPath := filepath.Join(dirPath, filename)
		data, err := os.ReadFile(toolPath)
		if err != nil {
			return nil, fmt.Errorf("reading tool file %s: %w", filename, err)
		}

		var raw rawToolFile
		if err := toml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parsing tool file %s: %w", filename, err)
		}

		if raw.Schema < 1 || raw.Schema > 3 {
			return nil, fmt.Errorf("tool file %s: unsupported schema %d (want 1, 2, or 3)", filename, raw.Schema)
		}
		if raw.Command == "" {
			return nil, fmt.Errorf("tool file %s: command is required", filename)
		}

		// Schema 1/2 backward compatibility: try the old snake_case keys.
		if raw.Schema <= 2 {
			var legacy legacyToolKeys
			_ = toml.Unmarshal(data, &legacy)
			if len(raw.ArgOrder) == 0 && len(legacy.ArgOrder) > 0 {
				raw.ArgOrder = legacy.ArgOrder
				allWarnings = append(allWarnings, fmt.Sprintf(
					"%s: arg_order is deprecated, use arg-order (schema 3)", filename))
			}
			if raw.StdinParam == "" && legacy.StdinParam != "" {
				raw.StdinParam = legacy.StdinParam
				allWarnings = append(allWarnings, fmt.Sprintf(
					"%s: stdin_param is deprecated, use stdin-param (schema 3)", filename))
			}
		}

		// Tool name: file stem, overridden by explicit name field.
		toolName := strings.TrimSuffix(filename, ".toml")
		if raw.Name != "" {
			toolName = raw.Name
		}

		if err := validatePermsRequest(raw.PermsRequest); err != nil {
			return nil, fmt.Errorf("tool file %s: %w", filename, err)
		}

		resultType, err := resolveResultType(raw.Schema, raw.ResultType)
		if err != nil {
			return nil, fmt.Errorf("tool file %s: %w", filename, err)
		}

		ts := ToolSpec{
			Name:                 toolName,
			Description:          raw.Description,
			Command:              raw.Command,
			Args:                 raw.Args,
			ArgOrder:             raw.ArgOrder,
			StdinParam:           raw.StdinParam,
			PermsRequest:         raw.PermsRequest,
			ContentType:          raw.ContentType,
			ResultType:           resultType,
			SubstituteResultURIs: raw.SubstituteResultURIs,
			Annotations:          raw.Annotations,
		}

		if raw.Input != nil {
			if raw.Input.Type == "" && (len(raw.Input.Properties) > 0 || len(raw.Input.Required) > 0) {
				raw.Input.Type = "object"
			}
			jsonBytes, err := json.Marshal(raw.Input)
			if err != nil {
				return nil, fmt.Errorf("tool file %s: marshaling input schema: %w", filename, err)
			}
			ts.Input = jsonBytes
			ts.InputParsed = raw.Input
		}

		cfg.Tools = append(cfg.Tools, ts)
		debugMoxin("ParseMoxinDirFull: %s: parsed tool %q (cmd=%q, args=%v)", meta.Name, toolName, raw.Command, raw.Args)
		allUndecoded = append(allUndecoded, detectUndecodedTool(data, filename, raw.Schema)...)
	}

	debugMoxin("ParseMoxinDirFull: %s: completed with %d tools", meta.Name, len(cfg.Tools))
	return &ParseResult{Config: cfg, Undecoded: allUndecoded, Warnings: allWarnings}, nil
}

func resolveResultType(schema int, raw string) (ResultType, error) {
	switch schema {
	case 1:
		// Schema 1 tools are always text mode; result-type field is ignored.
		return ResultTypeText, nil
	case 2, 3:
		if raw == "" {
			return ResultTypeMCPResult, nil
		}
		rt := ResultType(raw)
		switch rt {
		case ResultTypeText, ResultTypeMCPResult:
			return rt, nil
		default:
			return "", fmt.Errorf("invalid result-type %q (want text or mcp-result)", raw)
		}
	default:
		return "", fmt.Errorf("unsupported schema %d", schema)
	}
}

func validatePermsRequest(pr PermsRequest) error {
	switch pr {
	case "", PermsDelegateToClient, PermsAlwaysAllow, PermsEachUse:
		return nil
	default:
		return fmt.Errorf("invalid perms-request %q (want always-allow, each-use, or delegate-to-client)", pr)
	}
}

// detectUndecodedMeta returns undecoded keys in a _moxin.toml file.
func detectUndecodedMeta(data []byte) []string {
	doc, err := document.Parse(data)
	if err != nil {
		return nil
	}

	consumed := make(map[string]bool)
	for _, key := range []string{"schema", "name", "description"} {
		if doc.HasInContainer(doc.Root(), key) {
			consumed[key] = true
		}
	}

	return document.UndecodedKeys(doc.Root(), consumed)
}

// detectUndecodedTool returns undecoded keys in a per-tool TOML file,
// prefixed with the filename for context. The schema parameter controls
// which key spellings are recognized: schema 3 only accepts kebab-case,
// while schema 1/2 accept both spellings.
func detectUndecodedTool(data []byte, filename string, schema int) []string {
	doc, err := document.Parse(data)
	if err != nil {
		return nil
	}

	consumed := make(map[string]bool)
	keys := []string{
		"schema", "name", "description", "command", "args",
		"arg-order", "stdin-param", "perms-request",
		"content-type", "result-type", "substitute-result-uris",
	}
	if schema <= 2 {
		keys = append(keys, "arg_order", "stdin_param")
	}
	for _, key := range keys {
		if doc.HasInContainer(doc.Root(), key) {
			consumed[key] = true
		}
	}

	// input is an arbitrary JSON Schema table — consume it and all subtables.
	if inputNode := doc.FindTableInContainer(doc.Root(), "input"); inputNode != nil {
		consumed["input"] = true
		document.MarkAllConsumed(inputNode, "input", consumed)
	}

	// annotations is a known table with typed fields.
	if annNode := doc.FindTableInContainer(doc.Root(), "annotations"); annNode != nil {
		consumed["annotations"] = true
		document.MarkAllConsumed(annNode, "annotations", consumed)
	}

	for _, child := range doc.Root().Children {
		if child.Kind == cst.NodeTable {
			key := document.SubTableKey(child, "")
			if strings.HasPrefix(key, "input") {
				consumed[key] = true
				document.MarkAllConsumed(child, key, consumed)
			}
		}
	}

	raw := document.UndecodedKeys(doc.Root(), consumed)
	if len(raw) == 0 {
		return nil
	}

	prefixed := make([]string, len(raw))
	for i, k := range raw {
		prefixed[i] = filename + ": " + k
	}
	return prefixed
}
