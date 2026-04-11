package native

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// Server implements proxy.ServerBackend for native (config-declared) tools.
// It dispatches MCP method calls locally without spawning a child MCP server.
type Server struct {
	config  *NativeConfig
	toolIdx map[string]*ToolSpec
	cache   *resultCache
	session string
}

// NewServer constructs a Server from a parsed NativeConfig.
func NewServer(cfg *NativeConfig) *Server {
	idx := make(map[string]*ToolSpec, len(cfg.Tools))
	for i := range cfg.Tools {
		idx[cfg.Tools[i].Name] = &cfg.Tools[i]
	}

	session := os.Getenv("CLAUDE_SESSION_ID")
	if session == "" {
		session = "no-session"
	}

	return &Server{
		config:  cfg,
		toolIdx: idx,
		cache:   newResultCache(""),
		session: session,
	}
}

// SetSession overrides the session identifier used in cached result URIs.
func (s *Server) SetSession(id string) { s.session = id }

// Name returns the server's configured name.
func (s *Server) Name() string { return s.config.Name }

// Call dispatches an MCP JSON-RPC method and returns the result as raw JSON.
func (s *Server) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	switch method {
	case "tools/list":
		return s.handleToolsList()
	case "tools/call":
		return s.handleToolsCall(ctx, params)
	case "resources/list":
		return marshalResult(protocol.ResourcesListResultV1{})
	case "resources/templates/list":
		return marshalResult(protocol.ResourceTemplatesListResultV1{})
	case "prompts/list":
		return marshalResult(protocol.PromptsListResultV1{})
	default:
		return nil, fmt.Errorf("native server %q: unsupported method %q", s.config.Name, method)
	}
}

// Notify is a no-op; native servers do not process notifications.
func (s *Server) Notify(string, any) error { return nil }

// SetOnNotification is a no-op; native servers do not emit notifications.
func (s *Server) SetOnNotification(func(*jsonrpc.Message)) {}

// Close is a no-op; native servers hold no external resources.
func (s *Server) Close() error { return nil }

// InitializeResult synthesizes an MCP initialize result from the config.
func (s *Server) InitializeResult() *protocol.InitializeResultV1 {
	result := &protocol.InitializeResultV1{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ImplementationV1{
			Name: s.config.Name,
		},
	}
	if len(s.config.Tools) > 0 {
		result.Capabilities.Tools = &protocol.ToolsCapability{}
	}
	return result
}

func (s *Server) handleToolsList() (json.RawMessage, error) {
	tools := make([]protocol.ToolV1, len(s.config.Tools))
	for i, spec := range s.config.Tools {
		tool := protocol.ToolV1{
			Name:        spec.Name,
			Description: spec.Description,
		}
		if spec.Input != nil {
			tool.InputSchema = ensureObjectType(spec.Input)
		} else {
			tool.InputSchema = json.RawMessage(`{"type":"object"}`)
		}
		if spec.Annotations != nil {
			tool.Annotations = &protocol.ToolAnnotations{
				Title:           spec.Annotations.Title,
				ReadOnlyHint:    spec.Annotations.ReadOnlyHint,
				DestructiveHint: spec.Annotations.DestructiveHint,
				IdempotentHint:  spec.Annotations.IdempotentHint,
				OpenWorldHint:   spec.Annotations.OpenWorldHint,
			}
		}
		tools[i] = tool
	}
	return marshalResult(protocol.ToolsListResultV1{Tools: tools})
}

func (s *Server) handleToolsCall(ctx context.Context, params any) (json.RawMessage, error) {
	// params may arrive as a struct, map, or json.RawMessage — normalize via JSON round-trip.
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshaling tool call params: %w", err)
	}

	var callParams protocol.ToolCallParams
	if err := json.Unmarshal(raw, &callParams); err != nil {
		return nil, fmt.Errorf("unmarshaling tool call params: %w", err)
	}

	spec, ok := s.toolIdx[callParams.Name]
	if !ok {
		return marshalResult(protocol.ErrorResultV1(
			fmt.Sprintf("unknown tool %q", callParams.Name),
		))
	}

	// If stdin_param is configured, extract that key from the arguments
	// before positional arg building.  The framework will pipe its value
	// (literal or resolved from a result-cache URI) to the child's stdin.
	var stdinContent string
	arguments := callParams.Arguments
	if spec.StdinParam != "" {
		var argMap map[string]json.RawMessage
		if len(arguments) > 0 {
			if err := json.Unmarshal(arguments, &argMap); err == nil {
				if raw, ok := argMap[spec.StdinParam]; ok {
					var val string
					if err := json.Unmarshal(raw, &val); err == nil {
						stdinContent = val
					}
					delete(argMap, spec.StdinParam)
					arguments, _ = json.Marshal(argMap)
				}
			}
		}
		// Resolve result-cache URIs to their cached content.
		if stdinContent != "" {
			if session, id, ok := parseResultURI(stdinContent); ok {
				cached, loadErr := s.cache.load(session, id)
				if loadErr != nil {
					return marshalResult(protocol.ErrorResultV1(
						fmt.Sprintf("resolving stdin result URI: %v", loadErr),
					))
				}
				stdinContent = cached.Output
			}
		}
	}

	// Extract caller-supplied arguments and append them (as strings) after
	// spec.Args.  Ordering follows the input schema's "required" array so
	// positional semantics are deterministic; any remaining keys are appended
	// in sorted order.
	extraArgs, err := buildExtraArgs(arguments, spec.Input, spec.ArgOrder)
	if err != nil {
		return marshalResult(protocol.ErrorResultV1(
			fmt.Sprintf("parsing arguments: %v", err),
		))
	}

	allArgs := make([]string, 0, len(spec.Args)+len(extraArgs))
	allArgs = append(allArgs, spec.Args...)
	allArgs = append(allArgs, extraArgs...)

	// Apply URI substitution to each extra arg individually so that
	// moxy.native://results/{session}/{id} references are rewritten to
	// /dev/fd/N with pipes backed by cached output.
	var sub *resultSubstitution
	specArgCount := len(spec.Args)
	for i, arg := range allArgs[specArgCount:] {
		argSub, subErr := substituteResultURIs(arg, s.cache)
		if subErr != nil {
			if sub != nil {
				sub.Cleanup()
			}
			return marshalResult(protocol.ErrorResultV1(
				fmt.Sprintf("resolving result references: %v", subErr),
			))
		}
		allArgs[specArgCount+i] = argSub.Command
		if sub == nil {
			sub = argSub
		} else {
			// Merge extra files and pipe bookkeeping from this arg into
			// the aggregate substitution.
			sub.ExtraFiles = append(sub.ExtraFiles, argSub.ExtraFiles...)
			sub.pipeReads = append(sub.pipeReads, argSub.pipeReads...)
			sub.pipeWrites = append(sub.pipeWrites, argSub.pipeWrites...)
		}
	}
	if sub == nil {
		sub = &resultSubstitution{}
	}
	defer sub.Cleanup()

	cmd := exec.CommandContext(ctx, spec.Command, allArgs...)
	cmd.ExtraFiles = sub.ExtraFiles
	if stdinContent != "" {
		cmd.Stdin = strings.NewReader(stdinContent)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if startErr := cmd.Start(); startErr != nil {
		return marshalResult(protocol.ErrorResultV1(
			fmt.Sprintf("starting command: %v", startErr),
		))
	}
	sub.StartWriters()
	// Close the parent's read-end copies now that the child has them, so
	// the child sees EOF when the writer goroutines finish.
	for _, r := range sub.pipeReads {
		_ = r.Close()
	}
	sub.pipeReads = nil

	err = cmd.Wait()

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if err != nil {
		if output == "" {
			output = err.Error()
		}
		result := &protocol.ToolCallResultV1{
			Content: []protocol.ContentBlockV1{protocol.TextContentV1(output)},
			IsError: true,
		}
		return marshalResult(result)
	}

	if spec.ResultType == ResultTypeMCPResult {
		return s.buildMCPResult(spec, output)
	}

	return s.buildTextResult(spec, output)
}

func (s *Server) buildMCPResult(spec *ToolSpec, output string) (json.RawMessage, error) {
	if output == "" {
		return marshalResult(&protocol.ToolCallResultV1{})
	}

	var result protocol.ToolCallResultV1
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return marshalResult(protocol.ErrorResultV1(
			fmt.Sprintf("tool %q: invalid MCP result JSON: %v\nraw output:\n%s",
				spec.Name, err, output),
		))
	}
	return marshalResult(&result)
}

func (s *Server) buildTextResult(spec *ToolSpec, output string) (json.RawMessage, error) {
	if output == "" {
		return marshalResult(&protocol.ToolCallResultV1{})
	}

	tokens := estimateTokens(output)
	if tokens > tokenThreshold {
		id, idErr := newResultID()
		if idErr == nil {
			cached := cachedResult{
				ID:         id,
				Session:    s.session,
				Output:     output,
				LineCount:  countLines(output),
				TokenCount: tokens,
			}
			if storeErr := s.cache.store(cached); storeErr == nil {
				summary := formatSummary(cached)
				return marshalResult(&protocol.ToolCallResultV1{
					Content: []protocol.ContentBlockV1{protocol.TextContentV1(summary)},
				})
			}
		}
	}

	block := protocol.ContentBlockV1{Type: "text", Text: output}
	if spec.ContentType != "" {
		block.MimeType = spec.ContentType
	}
	return marshalResult(&protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{block},
	})
}

// buildExtraArgs extracts string argument values from the caller's JSON
// arguments and returns them in a deterministic order: first the keys listed
// in the input schema's "required" array, then any remaining keys sorted
// lexicographically. When argOrder is non-empty, it takes precedence over
// all other ordering heuristics — and absent keys emit empty strings so
// that positional indices remain stable for shell scripts using $1, $2, etc.
func buildExtraArgs(arguments json.RawMessage, inputSchema json.RawMessage, argOrder []string) ([]string, error) {
	if len(arguments) == 0 {
		return nil, nil
	}

	var argMap map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &argMap); err != nil {
		return nil, fmt.Errorf("unmarshaling arguments: %w", err)
	}
	if len(argMap) == 0 {
		return nil, nil
	}

	// When arg_order is set, emit a value for every slot (empty string for
	// absent keys) so positional indices are stable, then trim trailing
	// empty slots.
	if len(argOrder) > 0 {
		extra := make([]string, len(argOrder))
		seen := make(map[string]bool, len(argOrder))
		for i, key := range argOrder {
			seen[key] = true
			val, ok := argMap[key]
			if !ok {
				extra[i] = ""
				continue
			}
			var s string
			if err := json.Unmarshal(val, &s); err == nil {
				extra[i] = s
			} else {
				extra[i] = string(val)
			}
		}

		// Trim trailing empty slots so scripts can detect argc.
		for len(extra) > 0 && extra[len(extra)-1] == "" {
			extra = extra[:len(extra)-1]
		}

		// Append any unlisted keys in sorted order.
		var remaining []string
		for k := range argMap {
			if !seen[k] {
				remaining = append(remaining, k)
			}
		}
		sort.Strings(remaining)
		for _, key := range remaining {
			val := argMap[key]
			var s string
			if err := json.Unmarshal(val, &s); err == nil {
				extra = append(extra, s)
			} else {
				extra = append(extra, string(val))
			}
		}
		return extra, nil
	}

	order := argumentOrder(argMap, inputSchema, argOrder)

	var extra []string
	for _, key := range order {
		val, ok := argMap[key]
		if !ok {
			continue
		}
		// Try to unquote as a JSON string; fall back to raw representation
		// for non-string types (numbers, booleans, etc.).
		var s string
		if err := json.Unmarshal(val, &s); err == nil {
			extra = append(extra, s)
		} else {
			extra = append(extra, string(val))
		}
	}
	return extra, nil
}

// argumentOrder returns argument keys in the order they should be appended.
// When argOrder is non-empty, it defines the exact order — only keys present
// in both argOrder and argMap are included, and keys not listed in argOrder
// are appended in sorted order. Otherwise, falls back to the input schema's
// "required" array then lexicographic ordering.
func argumentOrder(argMap map[string]json.RawMessage, inputSchema json.RawMessage, argOrder []string) []string {
	// Explicit arg_order takes precedence over everything.
	priorityKeys := argOrder

	// Fall back to the schema's required array.
	if len(priorityKeys) == 0 && len(inputSchema) > 0 {
		var schema struct {
			Required []string `json:"required"`
		}
		if json.Unmarshal(inputSchema, &schema) == nil {
			priorityKeys = schema.Required
		}
	}

	seen := make(map[string]bool, len(priorityKeys))
	var order []string
	for _, k := range priorityKeys {
		if _, exists := argMap[k]; exists {
			order = append(order, k)
			seen[k] = true
		}
	}

	// Collect remaining keys in sorted order.
	var remaining []string
	for k := range argMap {
		if !seen[k] {
			remaining = append(remaining, k)
		}
	}
	sort.Strings(remaining)
	order = append(order, remaining...)

	return order
}

// ensureObjectType injects "type":"object" into a JSON Schema if missing.
// MCP clients (including Claude Code) require this field to generate tool bindings.
func ensureObjectType(schema json.RawMessage) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(schema, &m); err != nil {
		return schema
	}
	if _, ok := m["type"]; ok {
		return schema
	}
	m["type"] = "object"
	data, err := json.Marshal(m)
	if err != nil {
		return schema
	}
	return data
}

func marshalResult(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}
	return json.RawMessage(data), nil
}
