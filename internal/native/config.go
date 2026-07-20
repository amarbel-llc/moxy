package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"code.linenisgreat.com/tommy/pkg/cst"
	"code.linenisgreat.com/tommy/pkg/document"
	"github.com/BurntSushi/toml"
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
	// PermsDynamic runs the [dynamic-perms] script per call to derive a decision.
	// Added for moxy POC dynamic-perms
	PermsDynamic PermsRequest = "dynamic"
)

// DynamicPermsSpec describes how to invoke the per-call permission predicate.
// Mirrors the main tool's argv/stdin shaping so the existing input pipeline
// can be reused at execution time.
// Added for moxy POC dynamic-perms
type DynamicPermsSpec struct {
	Command    string   `toml:"command"           json:"command"`
	Args       []string `toml:"args"              json:"args,omitempty"`
	ArgOrder   []string `toml:"arg-order"         json:"arg-order,omitempty"`
	StdinParam string   `toml:"stdin-param"       json:"stdin-param,omitempty"`
	TimeoutMS  int      `toml:"timeout-ms"        json:"timeout-ms,omitempty"`
}

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
	DynamicPerms         *DynamicPermsSpec // Added for moxy POC dynamic-perms
	PermitAsync          *bool             // nil = eligible (see PermitsAsync); #317
	ContentType          string
	ResultType           ResultType
	CacheResults         CacheResults // resolved; never empty (#319)
	NoTruncate           bool
	SubstituteResultURIs *bool
	Annotations          *ToolAnnotations
	Input                json.RawMessage
	InputParsed          *InputSchema
}

// ShouldSubstituteURIs reports whether madder://blobs/<digest> URIs in
// this tool's arguments should be rewritten to /dev/fd/N pipes.
func (s *ToolSpec) ShouldSubstituteURIs() bool {
	return s.SubstituteResultURIs == nil || *s.SubstituteResultURIs
}

// PermitsAsync reports whether this tool may be dispatched asynchronously
// (FDR 0004, #317). Omitted defaults to eligible; only an explicit
// `permit-async = false` forbids backgrounding — the permission tier is a
// separate, additional gate enforced by the async preflight. *bool keeps
// omitted distinguishable from explicit-false for the future tools/list
// execution.taskSupport surfacing.
func (s *ToolSpec) PermitsAsync() bool {
	return s.PermitAsync == nil || *s.PermitAsync
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
	Schema               int               `toml:"schema"`
	Name                 string            `toml:"name"`
	Description          string            `toml:"description"`
	Command              string            `toml:"command"`
	Args                 []string          `toml:"args"`
	ArgOrder             []string          `toml:"arg-order"`
	StdinParam           string            `toml:"stdin-param"`
	PermsRequest         PermsRequest      `toml:"perms-request"`
	DynamicPerms         *DynamicPermsSpec `toml:"dynamic-perms"` // Added for moxy POC dynamic-perms
	PermitAsync          *bool             `toml:"permit-async"`  // #317
	CacheResults         string            `toml:"cache-results"` // #319
	ContentType          string            `toml:"content-type"`
	ResultType           string            `toml:"result-type"`
	NoTruncate           bool              `toml:"no-truncate"`
	SubstituteResultURIs *bool             `toml:"substitute-result-uris"`
	Annotations          *ToolAnnotations  `toml:"annotations"`
	Input                *InputSchema      `toml:"input"`
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
					"%s: arg_order is deprecated, use arg-order (schema 3)", filename,
				))
			}
			if raw.StdinParam == "" && legacy.StdinParam != "" {
				raw.StdinParam = legacy.StdinParam
				allWarnings = append(allWarnings, fmt.Sprintf(
					"%s: stdin_param is deprecated, use stdin-param (schema 3)", filename,
				))
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
		// Added for moxy POC dynamic-perms
		if err := validateDynamicPerms(raw.PermsRequest, raw.DynamicPerms); err != nil {
			return nil, fmt.Errorf("tool file %s: %w", filename, err)
		}

		resultType, err := resolveResultType(raw.Schema, raw.ResultType)
		if err != nil {
			return nil, fmt.Errorf("tool file %s: %w", filename, err)
		}

		cacheResults, err := resolveCacheResults(raw.CacheResults)
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
			DynamicPerms:         raw.DynamicPerms, // Added for moxy POC dynamic-perms
			PermitAsync:          raw.PermitAsync,  // #317
			ContentType:          raw.ContentType,
			ResultType:           resultType,
			CacheResults:         cacheResults, // #319
			NoTruncate:           raw.NoTruncate,
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

// CacheResults controls when a tool's output is written to the madder blob
// store and surfaced as a resource URI (#319). Decoupled from content-type:
// the mime is a label stamped onto whatever resource block caching produces,
// never itself a reason to cache.
type CacheResults string

const (
	// CacheAlways writes every non-empty output to the blob store; small
	// outputs come back as resource blocks (URI + text + mime if declared).
	// For tools whose small outputs are composable artifacts (re-piped
	// into jq, folio.read, etc.).
	CacheAlways CacheResults = "always"
	// CacheThreshold (the default) writes only outputs above the token
	// threshold; small outputs are plain text blocks with no blob write
	// and no mime.
	CacheThreshold CacheResults = "threshold"
	// CacheNever never writes the blob store — even oversized outputs are
	// returned as plain full text. The tool author owns the context cost.
	CacheNever CacheResults = "never"
)

func resolveCacheResults(raw string) (CacheResults, error) {
	if raw == "" {
		return CacheThreshold, nil
	}
	cr := CacheResults(raw)
	switch cr {
	case CacheAlways, CacheThreshold, CacheNever:
		return cr, nil
	default:
		return "", fmt.Errorf("invalid cache-results %q (want always, threshold, or never)", raw)
	}
}

func validatePermsRequest(pr PermsRequest) error {
	switch pr {
	case "", PermsDelegateToClient, PermsAlwaysAllow, PermsEachUse, PermsDynamic:
		return nil
	default:
		return fmt.Errorf("invalid perms-request %q (want always-allow, each-use, dynamic, or delegate-to-client)", pr)
	}
}

// Added for moxy POC dynamic-perms
//
// validateDynamicPerms enforces the contract between perms-request = "dynamic"
// and the [dynamic-perms] block: the block is required when dynamic is set,
// must declare a command, and is otherwise meaningless.
func validateDynamicPerms(pr PermsRequest, spec *DynamicPermsSpec) error {
	if pr == PermsDynamic {
		if spec == nil {
			return fmt.Errorf(`perms-request = "dynamic" requires a [dynamic-perms] block`)
		}
		if spec.Command == "" {
			return fmt.Errorf("[dynamic-perms]: command is required")
		}
		if spec.TimeoutMS < 0 {
			return fmt.Errorf("[dynamic-perms]: timeout-ms must be >= 0")
		}
		return nil
	}
	if spec != nil {
		return fmt.Errorf(`[dynamic-perms] is only valid with perms-request = "dynamic"`)
	}
	return nil
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

	return undecodedKeys(doc.Root(), consumed)
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
		"dynamic-perms", // Added for moxy POC dynamic-perms
		"permit-async",  // #317
		"cache-results", // #319
		"content-type", "result-type", "no-truncate", "substitute-result-uris",
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
		markAllConsumed(inputNode, "input", consumed)
	}

	// annotations is a known table with typed fields.
	if annNode := doc.FindTableInContainer(doc.Root(), "annotations"); annNode != nil {
		consumed["annotations"] = true
		markAllConsumed(annNode, "annotations", consumed)
	}

	// dynamic-perms is a known table with typed fields.
	// Added for moxy POC dynamic-perms
	if dpNode := doc.FindTableInContainer(doc.Root(), "dynamic-perms"); dpNode != nil {
		consumed["dynamic-perms"] = true
		markAllConsumed(dpNode, "dynamic-perms", consumed)
	}

	for _, child := range doc.Root().Children {
		if child.Kind == cst.NodeTable {
			key := document.SubTableKey(child, "")
			if strings.HasPrefix(key, "input") {
				consumed[key] = true
				markAllConsumed(child, key, consumed)
			}
		}
	}

	raw := undecodedKeys(doc.Root(), consumed)
	if len(raw) == 0 {
		return nil
	}

	prefixed := make([]string, len(raw))
	for i, k := range raw {
		prefixed[i] = filename + ": " + k
	}
	return prefixed
}

// undecodedKeys walks the CST and returns all key paths not present in the
// consumed set. Table headers are prefixed to their children (e.g.
// "input.foo"); keys under a consumed table are still checked individually.
// Localized from tommy's document.UndecodedKeys, removed in tommy 0.4.x where
// undecoded detection moved onto the cst.Value model (which native does not
// use — it decodes via BurntSushi and only borrows tommy for this scan).
func undecodedKeys(root *cst.Node, consumed map[string]bool) []string {
	var result []string
	for _, child := range root.Children {
		switch child.Kind {
		case cst.NodeKeyValue:
			if k := cst.KeyValueName(child); !consumed[k] {
				result = append(result, k)
			}
		case cst.NodeTable:
			name := cst.TableHeaderKey(child)
			if consumed[name] {
				// Table was consumed (e.g. a map field) — check inner keys.
				for _, inner := range child.Children {
					if inner.Kind != cst.NodeKeyValue {
						continue
					}
					if q := name + "." + cst.KeyValueName(inner); !consumed[q] {
						result = append(result, q)
					}
				}
			} else {
				result = append(result, name) // whole table unknown
			}
		}
	}
	return result
}

// markAllConsumed marks every key-value child of a table as consumed, using
// the given prefix to build qualified keys (e.g. "input.foo"). Localized from
// tommy's document.MarkAllConsumed, removed in tommy 0.4.x.
func markAllConsumed(table *cst.Node, prefix string, consumed map[string]bool) {
	for _, child := range table.Children {
		if child.Kind == cst.NodeKeyValue {
			consumed[prefix+"."+cst.KeyValueName(child)] = true
		}
	}
}
