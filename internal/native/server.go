package native

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/amarbel-llc/moxy/internal/lifecyclelog"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// moxinArgvPreview truncates an argv slice to a single log-friendly string
// of at most maxLen characters. Used so lifecycle.log lines don't blow up
// on long nix develop / bash -c invocations.
func moxinArgvPreview(argv []string, maxLen int) string {
	s := strings.Join(argv, " ")
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}

// moxinSignalName returns the signal name if the process was killed by a
// signal, or "none" otherwise. Safe to call with a nil ProcessState.
func moxinSignalName(ps *os.ProcessState) string {
	if ps == nil {
		return "unknown"
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return ws.Signal().String()
	}
	return "none"
}

// resolveBinPlaceholder replaces the @BIN@ placeholder in a tool command
// with the moxin's bin directory. This is a runtime fallback for standalone
// (non-nix) installs where @BIN@ was not substituted at build time.
func resolveBinPlaceholder(command, sourceDir string) string {
	if !strings.Contains(command, "@BIN@") {
		return command
	}
	return strings.ReplaceAll(command, "@BIN@", filepath.Join(sourceDir, "bin"))
}

// MadderBackend is the subset of *MadderClient that the proxy and
// native Server depend on. Defined as an interface so tests can stub
// the blob store without touching a real `madder` binary or
// initializing a store on disk.
//
// *MadderClient is the canonical implementation; a stub is in
// substitute_test.go for unit tests.
type MadderBackend interface {
	// Write streams content into the default blob store and returns
	// the resulting digest (markl-id).
	Write(ctx context.Context, content io.Reader) (string, error)
	// OpenBlob opens a pipe and prepares a writer that fills it with
	// the named blob's bytes. Used by substitution to stream into a
	// child process's fd without buffering the full payload.
	OpenBlob(ctx context.Context, digest string) (*os.File, BlobWriter, error)
	// CatBytes returns the raw bytes of a single blob synchronously.
	// Used by the resource provider for client reads.
	CatBytes(ctx context.Context, digest string) ([]byte, error)
}

// Server implements proxy.ServerBackend for native (config-declared) tools.
// It dispatches MCP method calls locally without spawning a child MCP server.
type Server struct {
	config  *NativeConfig
	toolIdx map[string]*ToolSpec
	madder  MadderBackend
	session string
}

// NewServer constructs a Server from a parsed NativeConfig. The madder
// backend may be nil for tests that don't exercise large-output
// caching; tools that produce >tokenThreshold output will then return
// inline content without a cache URI.
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
		session: session,
	}
}

// SetMadder wires the madder blob store into this Server. Tools that
// produce large outputs use it to stash content and return a
// madder://blobs/<digest> URI to the caller. Substitution
// (madder://blobs/... → /dev/fd/N) also goes through this backend.
func (s *Server) SetMadder(m MadderBackend) { s.madder = m }

// SetSession overrides the session identifier used for diagnostics.
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
	debugMoxin("InitializeResult: server=%q numTools=%d", s.config.Name, len(s.config.Tools))
	if len(s.config.Tools) > 0 {
		result.Capabilities.Tools = &protocol.ToolsCapability{}
		debugMoxin("InitializeResult: server=%q → setting Tools capability", s.config.Name)
	} else {
		debugMoxin("InitializeResult: server=%q → NO tools, Capabilities.Tools will be nil", s.config.Name)
	}
	return result
}

func (s *Server) handleToolsList() (json.RawMessage, error) {
	debugMoxin("handleToolsList: server=%q numTools=%d", s.config.Name, len(s.config.Tools))
	tools := make([]protocol.ToolV1, len(s.config.Tools))
	for i, spec := range s.config.Tools {
		debugMoxin("handleToolsList: server=%q tool[%d]=%q cmd=%q", s.config.Name, i, spec.Name, spec.Command)
		desc := spec.Description
		if spec.PermsRequest != "" {
			desc = fmt.Sprintf("%s [perms: %s]", desc, spec.PermsRequest)
		}
		tool := protocol.ToolV1{
			Name:        spec.Name,
			Description: desc,
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
	callParams, err := parseToolCallParams(params)
	if err != nil {
		return nil, err
	}

	spec, ok := s.toolIdx[callParams.Name]
	if !ok {
		debugMoxin("toolCall %s.%s: unknown tool", s.config.Name, callParams.Name)
		return errResult("unknown tool %q", callParams.Name)
	}
	debugMoxin("toolCall %s.%s: command=%s args=%v", s.config.Name, spec.Name, spec.Command, spec.Args)

	stdinContent, arguments, err := s.prepareStdin(ctx, spec, callParams.Arguments)
	if err != nil {
		return errResult("%v", err)
	}

	if err := validateEnumConstraints(arguments, spec.InputParsed); err != nil {
		return errResult("%v", err)
	}

	extraArgs, err := buildExtraArgs(arguments, spec.Input, spec.ArgOrder)
	if err != nil {
		return errResult("parsing arguments: %v", err)
	}

	allArgs := append(append(make([]string, 0, len(spec.Args)+len(extraArgs)), spec.Args...), extraArgs...)

	allArgs, sub, err := s.substituteArgvBlobURIs(ctx, spec, allArgs)
	if err != nil {
		return errResult("resolving blob references: %v", err)
	}
	defer sub.Cleanup()

	output, runErr := s.runMoxinProcess(ctx, spec, allArgs, stdinContent, sub)
	if runErr != nil {
		if output == "" {
			output = runErr.Error()
		}
		debugMoxin("toolCall %s.%s: exec error: %v args=%v", s.config.Name, spec.Name, runErr, allArgs)
		return marshalResult(&protocol.ToolCallResultV1{
			Content: []protocol.ContentBlockV1{protocol.TextContentV1(output)},
			IsError: true,
		})
	}

	if spec.ResultType == ResultTypeMCPResult {
		return s.buildMCPResult(ctx, spec, output)
	}
	return s.buildTextResult(ctx, spec, output)
}

// errResult marshals an MCP isError text result with a printf-formatted
// message. Most error paths in handleToolsCall use this shape.
func errResult(format string, args ...any) (json.RawMessage, error) {
	return marshalResult(protocol.ErrorResultV1(fmt.Sprintf(format, args...)))
}

// parseToolCallParams normalizes the params payload (which may arrive
// as a struct, map, or json.RawMessage) into a typed ToolCallParams.
func parseToolCallParams(params any) (protocol.ToolCallParams, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return protocol.ToolCallParams{}, fmt.Errorf("marshaling tool call params: %w", err)
	}
	var callParams protocol.ToolCallParams
	if err := json.Unmarshal(raw, &callParams); err != nil {
		return protocol.ToolCallParams{}, fmt.Errorf("unmarshaling tool call params: %w", err)
	}
	return callParams, nil
}

// prepareStdin pulls spec.StdinParam out of arguments (if configured),
// resolves any madder://blobs/<digest> reference into bytes, and
// returns the stdin content plus arguments with the stdin key removed.
func (s *Server) prepareStdin(
	ctx context.Context,
	spec *ToolSpec,
	arguments json.RawMessage,
) (string, json.RawMessage, error) {
	if spec.StdinParam == "" {
		return "", arguments, nil
	}

	stdinContent, remaining := extractStdinParam(arguments, spec.StdinParam)
	if stdinContent == "" || !spec.ShouldSubstituteURIs() {
		return stdinContent, remaining, nil
	}

	digest, ok := parseBlobURI(stdinContent)
	if !ok {
		return stdinContent, remaining, nil
	}
	if s.madder == nil {
		return "", remaining, fmt.Errorf("resolving stdin blob URI: no madder backend configured")
	}
	body, err := openBlobBuffered(ctx, s.madder, digest)
	if err != nil {
		return "", remaining, fmt.Errorf("resolving stdin blob URI: %w", err)
	}
	return string(body), remaining, nil
}

// extractStdinParam pops the named string field out of arguments and
// returns it alongside arguments with that field removed. Missing or
// non-string values yield an empty stdin and pass arguments through
// unchanged.
func extractStdinParam(arguments json.RawMessage, name string) (string, json.RawMessage) {
	if len(arguments) == 0 {
		return "", arguments
	}
	var argMap map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &argMap); err != nil {
		return "", arguments
	}
	raw, ok := argMap[name]
	if !ok {
		return "", arguments
	}
	var val string
	_ = json.Unmarshal(raw, &val)
	delete(argMap, name)
	remaining, _ := json.Marshal(argMap)
	return val, remaining
}

// substituteArgvBlobURIs rewrites madder://blobs/<digest> URIs in
// caller-supplied positional arguments to /dev/fd/N references. The
// returned resultSubstitution owns the pipes and writer subprocesses;
// callers must defer its Cleanup. When substitution is disabled or no
// madder backend is wired, an empty substitution is returned and
// argv passes through unchanged.
func (s *Server) substituteArgvBlobURIs(
	ctx context.Context,
	spec *ToolSpec,
	allArgs []string,
) ([]string, *resultSubstitution, error) {
	if !spec.ShouldSubstituteURIs() || s.madder == nil {
		return allArgs, &resultSubstitution{}, nil
	}

	var sub *resultSubstitution
	specArgCount := len(spec.Args)
	for i, arg := range allArgs[specArgCount:] {
		argSub, err := substituteMadderURIs(ctx, arg, s.madder)
		if err != nil {
			if sub != nil {
				sub.Cleanup()
			}
			return nil, nil, err
		}
		allArgs[specArgCount+i] = argSub.Command
		if sub == nil {
			sub = argSub
			continue
		}
		// Merge bookkeeping from this arg into the aggregate substitution.
		sub.ExtraFiles = append(sub.ExtraFiles, argSub.ExtraFiles...)
		sub.pipeReads = append(sub.pipeReads, argSub.pipeReads...)
		sub.writers = append(sub.writers, argSub.writers...)
	}
	if sub == nil {
		sub = &resultSubstitution{}
	}
	return allArgs, sub, nil
}

// runMoxinProcess starts the moxin tool's subprocess, kicks off any
// blob-streaming writers, waits for both, and returns the combined
// stdout+stderr plus any error. Lifecycle and debug logging happen
// here.
func (s *Server) runMoxinProcess(
	ctx context.Context,
	spec *ToolSpec,
	allArgs []string,
	stdinContent string,
	sub *resultSubstitution,
) (string, error) {
	command := resolveBinPlaceholder(spec.Command, s.config.SourceDir)
	if !filepath.IsAbs(command) && s.config.SourceDir != "" && strings.Contains(command, string(filepath.Separator)) {
		command = filepath.Join(s.config.SourceDir, command)
	}
	cmd := exec.CommandContext(ctx, command, allArgs...)
	cmd.ExtraFiles = sub.ExtraFiles
	if stdinContent != "" {
		cmd.Stdin = strings.NewReader(stdinContent)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	moxinStart := time.Now()
	if err := cmd.Start(); err != nil {
		debugMoxin("toolCall %s.%s: start error: %v (command=%s args=%v)", s.config.Name, spec.Name, err, spec.Command, allArgs)
		lifecyclelog.Log("moxin START_FAIL %s.%s err=%v", s.config.Name, spec.Name, err)
		return "", fmt.Errorf("starting command: %w", err)
	}
	moxinPid := cmd.Process.Pid
	lifecyclelog.Log(
		"moxin START %s.%s pid=%d argv=%q",
		s.config.Name, spec.Name, moxinPid,
		moxinArgvPreview(append([]string{command}, allArgs...), 120),
	)

	if err := sub.StartWriters(); err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		return "", fmt.Errorf("starting blob streaming: %w", err)
	}
	// Close the parent's read-end copies now that the child has them, so
	// the child sees EOF when the writer subprocesses finish.
	for _, r := range sub.pipeReads {
		_ = r.Close()
	}
	sub.pipeReads = nil

	waitErr := cmd.Wait()
	if writerErr := sub.WaitWriters(); writerErr != nil && waitErr == nil {
		waitErr = writerErr
	}
	lifecyclelog.Log(
		"moxin DONE %s.%s pid=%d dur=%s exit=%d signal=%s stdout=%d stderr=%d",
		s.config.Name, spec.Name, moxinPid, time.Since(moxinStart), exitCode(cmd.ProcessState),
		moxinSignalName(cmd.ProcessState), stdout.Len(), stderr.Len(),
	)

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}
	return output, waitErr
}

func exitCode(ps *os.ProcessState) int {
	if ps == nil {
		return -1
	}
	return ps.ExitCode()
}

func (s *Server) buildMCPResult(ctx context.Context, spec *ToolSpec, output string) (json.RawMessage, error) {
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

	// Rewrite text blocks that carry mimeType into resource blocks with
	// cache URIs — the MCP spec only allows mimeType on resource blocks.
	// Skip empty text — EmbeddedResourceContents requires non-empty text or blob.
	cleaned := result.Content[:0]
	for _, block := range result.Content {
		if block.Type == "text" && block.MimeType != "" {
			if block.Text != "" && s.madder != nil {
				uri, cacheErr := s.cacheAndGetURI(ctx, block.Text)
				if cacheErr == nil {
					text := block.Text
					cleaned = append(cleaned, protocol.ContentBlockV1{
						Type: "resource",
						Resource: &protocol.EmbeddedResourceContents{
							URI:      uri,
							Text:     &text,
							MimeType: block.MimeType,
						},
					})
					continue
				}
			}
			// Strip mimeType — the MCP spec doesn't allow it on text blocks.
			// Drop empty text blocks entirely: V1's omitempty on the Text
			// field would produce {"type":"text"} with no text property,
			// which fails Claude Code's Zod validator (invalid_union).
			if block.Text == "" {
				continue
			}
			block.MimeType = ""
		}
		cleaned = append(cleaned, block)
	}
	result.Content = cleaned

	return marshalResult(&result)
}

func (s *Server) buildTextResult(ctx context.Context, spec *ToolSpec, output string) (json.RawMessage, error) {
	if output == "" {
		return marshalResult(&protocol.ToolCallResultV1{})
	}

	tokens := estimateTokens(output)
	if tokens > tokenThreshold && s.madder != nil {
		digest, storeErr := s.madder.Write(ctx, strings.NewReader(output))
		if storeErr == nil {
			if spec.NoTruncate {
				inline := fmt.Sprintf(
					"Full output: %s\nLines: %d\n\n%s",
					blobURI(digest), countLines(output), output,
				)
				return marshalResult(&protocol.ToolCallResultV1{
					Content: []protocol.ContentBlockV1{protocol.TextContentV1(inline)},
				})
			}
			summary := formatSummary(output, digest)
			return marshalResult(&protocol.ToolCallResultV1{
				Content: []protocol.ContentBlockV1{protocol.TextContentV1(summary)},
			})
		}
	}

	if spec.ContentType != "" && s.madder != nil {
		uri, cacheErr := s.cacheAndGetURI(ctx, output)
		if cacheErr == nil {
			block := protocol.ContentBlockV1{
				Type: "resource",
				Resource: &protocol.EmbeddedResourceContents{
					URI:      uri,
					Text:     &output,
					MimeType: spec.ContentType,
				},
			}
			return marshalResult(&protocol.ToolCallResultV1{
				Content: []protocol.ContentBlockV1{block},
			})
		}
	}

	return marshalResult(&protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{protocol.TextContentV1(output)},
	})
}

func (s *Server) cacheAndGetURI(ctx context.Context, output string) (string, error) {
	if s.madder == nil {
		return "", fmt.Errorf("no madder backend configured")
	}
	digest, err := s.madder.Write(ctx, strings.NewReader(output))
	if err != nil {
		return "", err
	}
	return blobURI(digest), nil
}

// validateEnumConstraints checks that any argument with an enum constraint
// in the input schema has a value that is one of the allowed options.
func validateEnumConstraints(arguments json.RawMessage, schema *InputSchema) error {
	if schema == nil || len(arguments) == 0 {
		return nil
	}

	var argMap map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &argMap); err != nil {
		return nil // let buildExtraArgs handle parse errors
	}

	for name, prop := range schema.Properties {
		if len(prop.Enum) == 0 {
			continue
		}
		raw, ok := argMap[name]
		if !ok {
			continue
		}
		var val string
		if err := json.Unmarshal(raw, &val); err != nil {
			continue // non-string value; skip enum check
		}
		found := false
		for _, allowed := range prop.Enum {
			if val == allowed {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("invalid value %q for parameter %q: must be one of %v", val, name, prop.Enum)
		}
	}
	return nil
}

// BuildExtraArgs is the exported alias of buildExtraArgs, used by the
// dynamic-perms executor in dynperms.go so it shares argv-shaping with the
// main tool dispatcher.
// Added for moxy POC dynamic-perms
func BuildExtraArgs(arguments json.RawMessage, inputSchema json.RawMessage, argOrder []string) ([]string, error) {
	return buildExtraArgs(arguments, inputSchema, argOrder)
}

// BuildPermsArgs is BuildExtraArgs's strict cousin. When argOrder is
// non-empty, only the listed keys are emitted as positional argv;
// unlisted JSON keys are silently dropped. When argOrder is empty, the
// behavior is identical to BuildExtraArgs (schema-required + sorted
// remainder).
//
// This is the right shape for the dynamic-perms gate: the perms script
// declares which inputs it actually wants to see via its own
// [dynamic-perms].arg-order. Trailing keys like a file's content have
// no business in a path check and may even contain path-shaped tokens
// that trigger spurious permission prompts.
func BuildPermsArgs(arguments json.RawMessage, inputSchema json.RawMessage, argOrder []string) ([]string, error) {
	if len(argOrder) == 0 {
		return buildExtraArgs(arguments, inputSchema, nil)
	}

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

	extra := make([]string, len(argOrder))
	for i, key := range argOrder {
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
	return extra, nil
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
