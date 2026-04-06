package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"
)

type folioServer struct {
	perms   *PermissionConfig
	readCfg ReadConfig
}

// Resource templates (V0).

var templates = []protocol.ResourceTemplate{
	{
		URITemplate: "folio://read/{+path}",
		Name:        "Read file",
		Description: "Read a file with line numbers. Prefer this over shelling out to sed. Use ?offset=N&end=M for an inclusive line range (equivalent to `sed -n 'N,Mp'`), ?delete=N-M to omit an inclusive range (equivalent to `sed 'N,Md'`), or ?offset=N&limit=M for count-based pagination. Also available as the `read`, `read_range`, and `read_excluding` tools.",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "folio://glob/{+pattern}",
		Name:        "Glob files",
		Description: "Find files matching a glob pattern. Supports ** for recursive matching. Use ?path={dir} to set the search root. Results sorted by modification time (newest first).",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "folio://grep/{+pattern}",
		Name:        "Grep content",
		Description: "Search file contents using ripgrep. Use ?path={dir}, ?glob={filter} (e.g. *.go), ?type={lang} (e.g. go, py), ?output_mode={files_with_matches|content|count}, ?context=N (lines around matches), ?case_insensitive=true.",
		MimeType:    "text/plain",
	},
}

// Resource templates (V1).

var templatesV1 = []protocol.ResourceTemplateV1{
	{
		URITemplate: "folio://read/{+path}",
		Name:        "Read file",
		Description: "Read a file with line numbers. Prefer this over shelling out to sed. Use ?offset=N&end=M for an inclusive line range (equivalent to `sed -n 'N,Mp'`), ?delete=N-M to omit an inclusive range (equivalent to `sed 'N,Md'`), or ?offset=N&limit=M for count-based pagination. Also available as the `read`, `read_range`, and `read_excluding` tools.",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "folio://glob/{+pattern}",
		Name:        "Glob files",
		Description: "Find files matching a glob pattern. Supports ** for recursive matching. Use ?path={dir} to set the search root. Results sorted by modification time (newest first).",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "folio://grep/{+pattern}",
		Name:        "Grep content",
		Description: "Search file contents using ripgrep. Use ?path={dir}, ?glob={filter} (e.g. *.go), ?type={lang} (e.g. go, py), ?output_mode={files_with_matches|content|count}, ?context=N (lines around matches), ?case_insensitive=true.",
		MimeType:    "text/plain",
	},
}

// ResourceProvider (base interface)

func (s *folioServer) ListResources(_ context.Context) ([]protocol.Resource, error) {
	return nil, nil
}

func (s *folioServer) ReadResource(_ context.Context, uri string) (*protocol.ResourceReadResult, error) {
	// Dispatch by URI scheme.
	if strings.HasPrefix(uri, "folio://glob/") {
		return s.handleGlobResource(uri)
	}
	if strings.HasPrefix(uri, "folio://grep/") {
		return s.handleGrepResource(uri)
	}

	p, err := parseReadURI(uri)
	if err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(p.path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	if err := s.perms.CheckPermission(absPath); err != nil {
		return nil, err
	}

	// Check for binary.
	if isBin, mime := detectBinary(absPath); isBin {
		return nil, fmt.Errorf("binary file detected (%s): %s", mime, absPath)
	}

	content, totalLines, err := readFileFiltered(absPath, p.offset, p.limit, p.deleteStart, p.deleteEnd)
	if err != nil {
		return nil, err
	}

	// Progressive disclosure: only the "no params" whole-file case.
	if p.offset == 0 && p.limit == 0 && p.deleteStart == 0 && p.deleteEnd == 0 && totalLines > s.readCfg.MaxLines {
		headContent, _, _ := readFileWithLineNumbers(absPath, 1, s.readCfg.HeadLines)
		tailOffset := totalLines - s.readCfg.TailLines + 1
		if tailOffset < 1 {
			tailOffset = 1
		}
		tailContent, _, _ := readFileWithLineNumbers(absPath, tailOffset, s.readCfg.TailLines)
		resourceURI := "folio://read/" + absPath
		content = formatReadSummary(absPath, totalLines, headContent, tailContent, resourceURI)
	}

	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{
			{URI: uri, MimeType: "text/plain", Text: content},
		},
	}, nil
}

func (s *folioServer) ListResourceTemplates(_ context.Context) ([]protocol.ResourceTemplate, error) {
	return templates, nil
}

// ResourceProviderV1 (V1 extensions)

func (s *folioServer) ListResourcesV1(_ context.Context, _ string) (*protocol.ResourcesListResultV1, error) {
	return &protocol.ResourcesListResultV1{}, nil
}

func (s *folioServer) ListResourceTemplatesV1(_ context.Context, _ string) (*protocol.ResourceTemplatesListResultV1, error) {
	return &protocol.ResourceTemplatesListResultV1{ResourceTemplates: templatesV1}, nil
}

// ToolProvider (base interface)

var readToolSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"file_path": {"type": "string", "description": "Absolute path to the file to read"}
	},
	"required": ["file_path"]
}`)

var readRangeToolSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"file_path": {"type": "string", "description": "Absolute path to the file to read"},
		"start": {"type": "integer", "description": "Inclusive start line (1-indexed)", "minimum": 1},
		"end": {"type": "integer", "description": "Inclusive end line (1-indexed)", "minimum": 1}
	},
	"required": ["file_path", "start", "end"]
}`)

var readExcludingToolSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"file_path": {"type": "string", "description": "Absolute path to the file to read"},
		"delete_start": {"type": "integer", "description": "Inclusive first line to omit (1-indexed)", "minimum": 1},
		"delete_end": {"type": "integer", "description": "Inclusive last line to omit (1-indexed)", "minimum": 1}
	},
	"required": ["file_path", "delete_start", "delete_end"]
}`)

var writeToolSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"file_path": {"type": "string", "description": "Absolute path to the file to write"},
		"content": {"type": "string", "description": "The content to write to the file"}
	},
	"required": ["file_path", "content"]
}`)

var editToolSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"file_path": {"type": "string", "description": "Absolute path to the file to edit"},
		"old_string": {"type": "string", "description": "The text to replace"},
		"new_string": {"type": "string", "description": "The replacement text"},
		"replace_all": {"type": "boolean", "description": "Replace all occurrences (default false)", "default": false}
	},
	"required": ["file_path", "old_string", "new_string"]
}`)

const (
	readToolDescription          = "Read an entire file with line numbers. Output: first line is the folio://read/<path> resource URI; subsequent lines are `     N\\tcontent`. Large files return a head+tail summary instead — use read_range for specific sections."
	readRangeToolDescription     = "Read an inclusive line range from a file, equivalent to `sed -n 'start,end p' <file>`. Prefer this over shelling out to sed. Output: first line is the folio://read/<path>?offset=start&end=end resource URI; subsequent lines are `     N\\tcontent`."
	readExcludingToolDescription = "Read a file with an inclusive line range omitted, equivalent to `sed 'delete_start,delete_end d' <file>`. Prefer this over shelling out to sed. Output: first line is the folio://read/<path>?delete=start-end resource URI; subsequent lines are `     N\\tcontent`."
)

func (s *folioServer) ListTools(_ context.Context) ([]protocol.Tool, error) {
	return []protocol.Tool{
		{
			Name:        "read",
			Description: readToolDescription,
			InputSchema: readToolSchema,
		},
		{
			Name:        "read_range",
			Description: readRangeToolDescription,
			InputSchema: readRangeToolSchema,
		},
		{
			Name:        "read_excluding",
			Description: readExcludingToolDescription,
			InputSchema: readExcludingToolSchema,
		},
		{
			Name:        "write",
			Description: "Create or overwrite a file. Creates parent directories if needed.",
			InputSchema: writeToolSchema,
		},
		{
			Name:        "edit",
			Description: "Perform exact string replacement in a file. Fails if old_string is not found or matches multiple locations (unless replace_all is true).",
			InputSchema: editToolSchema,
		},
	}, nil
}

func (s *folioServer) CallTool(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResult, error) {
	resultV1, err := s.CallToolV1(ctx, name, args)
	if err != nil {
		return nil, err
	}
	// Downgrade V1 result to V0.
	result := &protocol.ToolCallResult{IsError: resultV1.IsError}
	for _, block := range resultV1.Content {
		result.Content = append(result.Content, protocol.ContentBlock{
			Type: block.Type,
			Text: block.Text,
		})
	}
	return result, nil
}

// ToolProviderV1 (V1 extensions)

func (s *folioServer) ListToolsV1(_ context.Context, _ string) (*protocol.ToolsListResultV1, error) {
	return &protocol.ToolsListResultV1{
		Tools: []protocol.ToolV1{
			{
				Name:        "read",
				Description: readToolDescription,
				InputSchema: readToolSchema,
				Annotations: &protocol.ToolAnnotations{
					ReadOnlyHint:   boolPtr(true),
					IdempotentHint: boolPtr(true),
				},
			},
			{
				Name:        "read_range",
				Description: readRangeToolDescription,
				InputSchema: readRangeToolSchema,
				Annotations: &protocol.ToolAnnotations{
					ReadOnlyHint:   boolPtr(true),
					IdempotentHint: boolPtr(true),
				},
			},
			{
				Name:        "read_excluding",
				Description: readExcludingToolDescription,
				InputSchema: readExcludingToolSchema,
				Annotations: &protocol.ToolAnnotations{
					ReadOnlyHint:   boolPtr(true),
					IdempotentHint: boolPtr(true),
				},
			},
			{
				Name:        "write",
				Description: "Create or overwrite a file. Creates parent directories if needed.",
				InputSchema: writeToolSchema,
				Annotations: &protocol.ToolAnnotations{
					DestructiveHint: boolPtr(true),
					IdempotentHint:  boolPtr(true),
				},
			},
			{
				Name:        "edit",
				Description: "Perform exact string replacement in a file. Fails if old_string is not found or matches multiple locations (unless replace_all is true).",
				InputSchema: editToolSchema,
			},
		},
	}, nil
}

func (s *folioServer) CallToolV1(_ context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
	switch name {
	case "read":
		return s.handleRead(args)
	case "read_range":
		return s.handleReadRange(args)
	case "read_excluding":
		return s.handleReadExcluding(args)
	case "write":
		return s.handleWrite(args)
	case "edit":
		return s.handleEdit(args)
	default:
		return protocol.ErrorResultV1(fmt.Sprintf("unknown tool %q", name)), nil
	}
}

// preflightRead resolves the path, checks permissions, and rejects binary
// files. It returns the absolute path on success, or a ready-to-return error
// result otherwise.
func (s *folioServer) preflightRead(filePath string) (string, *protocol.ToolCallResultV1) {
	if filePath == "" {
		return "", protocol.ErrorResultV1("file_path is required")
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", protocol.ErrorResultV1(fmt.Sprintf("resolving path: %v", err))
	}
	if err := s.perms.CheckPermission(absPath); err != nil {
		return "", protocol.ErrorResultV1(err.Error())
	}
	if isBin, mime := detectBinary(absPath); isBin {
		return "", protocol.ErrorResultV1(fmt.Sprintf("binary file detected (%s): %s", mime, absPath))
	}
	return absPath, nil
}

// formatReadToolOutput is the custom tool-response format: the resource URI
// on the first line, followed by the line-numbered content.
func formatReadToolOutput(uri, content string) string {
	return uri + "\n" + content
}

func (s *folioServer) handleRead(args json.RawMessage) (*protocol.ToolCallResultV1, error) {
	var params struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(fmt.Sprintf("invalid read args: %v", err)), nil
	}
	absPath, errResult := s.preflightRead(params.FilePath)
	if errResult != nil {
		return errResult, nil
	}

	content, totalLines, err := readFileFiltered(absPath, 0, 0, 0, 0)
	if err != nil {
		return protocol.ErrorResultV1(err.Error()), nil
	}

	uri := "folio://read/" + absPath

	// Progressive disclosure for whole-file reads of large files.
	if totalLines > s.readCfg.MaxLines {
		headContent, _, _ := readFileFiltered(absPath, 1, s.readCfg.HeadLines, 0, 0)
		tailOffset := totalLines - s.readCfg.TailLines + 1
		if tailOffset < 1 {
			tailOffset = 1
		}
		tailContent, _, _ := readFileFiltered(absPath, tailOffset, s.readCfg.TailLines, 0, 0)
		content = formatReadSummary(absPath, totalLines, headContent, tailContent, uri)
	}

	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			protocol.TextContentV1(formatReadToolOutput(uri, content)),
		},
	}, nil
}

func (s *folioServer) handleReadRange(args json.RawMessage) (*protocol.ToolCallResultV1, error) {
	var params struct {
		FilePath string `json:"file_path"`
		Start    int    `json:"start"`
		End      int    `json:"end"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(fmt.Sprintf("invalid read_range args: %v", err)), nil
	}
	if params.Start < 1 {
		return protocol.ErrorResultV1("start must be >= 1"), nil
	}
	if params.End < params.Start {
		return protocol.ErrorResultV1(fmt.Sprintf("end (%d) must be >= start (%d)", params.End, params.Start)), nil
	}
	absPath, errResult := s.preflightRead(params.FilePath)
	if errResult != nil {
		return errResult, nil
	}

	limit := params.End - params.Start + 1
	content, _, err := readFileFiltered(absPath, params.Start, limit, 0, 0)
	if err != nil {
		return protocol.ErrorResultV1(err.Error()), nil
	}

	uri := fmt.Sprintf("folio://read/%s?offset=%d&end=%d", absPath, params.Start, params.End)
	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			protocol.TextContentV1(formatReadToolOutput(uri, content)),
		},
	}, nil
}

func (s *folioServer) handleReadExcluding(args json.RawMessage) (*protocol.ToolCallResultV1, error) {
	var params struct {
		FilePath    string `json:"file_path"`
		DeleteStart int    `json:"delete_start"`
		DeleteEnd   int    `json:"delete_end"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(fmt.Sprintf("invalid read_excluding args: %v", err)), nil
	}
	if params.DeleteStart < 1 {
		return protocol.ErrorResultV1("delete_start must be >= 1"), nil
	}
	if params.DeleteEnd < params.DeleteStart {
		return protocol.ErrorResultV1(fmt.Sprintf("delete_end (%d) must be >= delete_start (%d)", params.DeleteEnd, params.DeleteStart)), nil
	}
	absPath, errResult := s.preflightRead(params.FilePath)
	if errResult != nil {
		return errResult, nil
	}

	content, _, err := readFileFiltered(absPath, 0, 0, params.DeleteStart, params.DeleteEnd)
	if err != nil {
		return protocol.ErrorResultV1(err.Error()), nil
	}

	uri := fmt.Sprintf("folio://read/%s?delete=%d-%d", absPath, params.DeleteStart, params.DeleteEnd)
	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			protocol.TextContentV1(formatReadToolOutput(uri, content)),
		},
	}, nil
}

func (s *folioServer) handleWrite(args json.RawMessage) (*protocol.ToolCallResultV1, error) {
	var params struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(fmt.Sprintf("invalid write args: %v", err)), nil
	}
	if params.FilePath == "" {
		return protocol.ErrorResultV1("file_path is required"), nil
	}

	absPath, err := filepath.Abs(params.FilePath)
	if err != nil {
		return protocol.ErrorResultV1(fmt.Sprintf("resolving path: %v", err)), nil
	}

	if err := s.perms.CheckPermission(absPath); err != nil {
		return protocol.ErrorResultV1(err.Error()), nil
	}

	if err := atomicWrite(absPath, []byte(params.Content)); err != nil {
		return protocol.ErrorResultV1(fmt.Sprintf("write failed: %v", err)), nil
	}

	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			protocol.TextContentV1(fmt.Sprintf("Wrote %d bytes to %s", len(params.Content), absPath)),
		},
	}, nil
}

func (s *folioServer) handleEdit(args json.RawMessage) (*protocol.ToolCallResultV1, error) {
	var params struct {
		FilePath   string `json:"file_path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(fmt.Sprintf("invalid edit args: %v", err)), nil
	}
	if params.FilePath == "" {
		return protocol.ErrorResultV1("file_path is required"), nil
	}
	if params.OldString == "" {
		return protocol.ErrorResultV1("old_string is required"), nil
	}

	absPath, err := filepath.Abs(params.FilePath)
	if err != nil {
		return protocol.ErrorResultV1(fmt.Sprintf("resolving path: %v", err)), nil
	}

	if err := s.perms.CheckPermission(absPath); err != nil {
		return protocol.ErrorResultV1(err.Error()), nil
	}

	count, err := editFile(absPath, params.OldString, params.NewString, params.ReplaceAll)
	if err != nil {
		return protocol.ErrorResultV1(err.Error()), nil
	}

	msg := fmt.Sprintf("Made %d replacement(s) in %s", count, absPath)
	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			protocol.TextContentV1(msg),
		},
	}, nil
}

// URI parsing

// readURIParams holds the parsed query parameters of a folio://read URI.
type readURIParams struct {
	path        string
	offset      int
	limit       int
	deleteStart int
	deleteEnd   int
}

func parseReadURI(uri string) (readURIParams, error) {
	// Expected: folio://read/<path>?offset=N&limit=M[&end=M][&delete=N-M]
	var p readURIParams
	if !strings.HasPrefix(uri, "folio://read/") {
		return p, fmt.Errorf("invalid folio URI: %s", uri)
	}

	rest := strings.TrimPrefix(uri, "folio://read/")

	// Split path from query params.
	pathPart := rest
	queryPart := ""
	if idx := strings.Index(rest, "?"); idx >= 0 {
		pathPart = rest[:idx]
		queryPart = rest[idx+1:]
	}

	if pathPart == "" {
		return p, fmt.Errorf("empty path in URI: %s", uri)
	}
	p.path = pathPart

	if queryPart == "" {
		return p, nil
	}

	values, err := url.ParseQuery(queryPart)
	if err != nil {
		return readURIParams{}, fmt.Errorf("invalid query params: %w", err)
	}
	if v := values.Get("offset"); v != "" {
		p.offset, err = strconv.Atoi(v)
		if err != nil {
			return readURIParams{}, fmt.Errorf("invalid offset: %w", err)
		}
	}
	if v := values.Get("limit"); v != "" {
		p.limit, err = strconv.Atoi(v)
		if err != nil {
			return readURIParams{}, fmt.Errorf("invalid limit: %w", err)
		}
	}
	// ?end=N specifies an inclusive end line, equivalent to
	// `sed -n 'offset,end p'`. If offset is unset it defaults to 1.
	// end takes precedence over limit when both are supplied.
	if v := values.Get("end"); v != "" {
		end, endErr := strconv.Atoi(v)
		if endErr != nil {
			return readURIParams{}, fmt.Errorf("invalid end: %w", endErr)
		}
		start := p.offset
		if start < 1 {
			start = 1
		}
		if end < start {
			return readURIParams{}, fmt.Errorf("end (%d) must be >= offset (%d)", end, start)
		}
		p.offset = start
		p.limit = end - start + 1
	}
	// ?delete=N-M omits an inclusive range of lines from the output,
	// equivalent to `sed 'N,Md'`.
	if v := values.Get("delete"); v != "" {
		parts := strings.SplitN(v, "-", 2)
		if len(parts) != 2 {
			return readURIParams{}, fmt.Errorf("invalid delete range (want N-M): %s", v)
		}
		ds, dsErr := strconv.Atoi(parts[0])
		if dsErr != nil {
			return readURIParams{}, fmt.Errorf("invalid delete start: %w", dsErr)
		}
		de, deErr := strconv.Atoi(parts[1])
		if deErr != nil {
			return readURIParams{}, fmt.Errorf("invalid delete end: %w", deErr)
		}
		if ds < 1 {
			return readURIParams{}, fmt.Errorf("delete start must be >= 1")
		}
		if de < ds {
			return readURIParams{}, fmt.Errorf("delete end (%d) must be >= delete start (%d)", de, ds)
		}
		p.deleteStart = ds
		p.deleteEnd = de
	}

	return p, nil
}

// Glob resource handler

func (s *folioServer) handleGlobResource(uri string) (*protocol.ResourceReadResult, error) {
	pattern, dir, err := parseGlobURI(uri)
	if err != nil {
		return nil, err
	}

	if dir == "" {
		dir, _ = os.Getwd()
	}

	matches, err := globFiles(pattern, dir)
	if err != nil {
		return nil, err
	}

	text := formatGlobResults(matches)
	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{
			{URI: uri, MimeType: "text/plain", Text: text},
		},
	}, nil
}

func parseGlobURI(uri string) (pattern, dir string, err error) {
	rest := strings.TrimPrefix(uri, "folio://glob/")

	pathPart := rest
	queryPart := ""
	if idx := strings.Index(rest, "?"); idx >= 0 {
		pathPart = rest[:idx]
		queryPart = rest[idx+1:]
	}

	if pathPart == "" {
		return "", "", fmt.Errorf("empty pattern in glob URI: %s", uri)
	}

	if queryPart != "" {
		values, parseErr := url.ParseQuery(queryPart)
		if parseErr != nil {
			return "", "", fmt.Errorf("invalid query params: %w", parseErr)
		}
		dir = values.Get("path")
	}

	return pathPart, dir, nil
}

// Grep resource handler

func (s *folioServer) handleGrepResource(uri string) (*protocol.ResourceReadResult, error) {
	params, err := parseGrepURI(uri)
	if err != nil {
		return nil, err
	}

	if params.Path == "" {
		params.Path, _ = os.Getwd()
	}

	text, err := runGrep(params)
	if err != nil {
		return nil, err
	}

	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{
			{URI: uri, MimeType: "text/plain", Text: text},
		},
	}, nil
}

func parseGrepURI(uri string) (grepParams, error) {
	rest := strings.TrimPrefix(uri, "folio://grep/")

	pathPart := rest
	queryPart := ""
	if idx := strings.Index(rest, "?"); idx >= 0 {
		pathPart = rest[:idx]
		queryPart = rest[idx+1:]
	}

	if pathPart == "" {
		return grepParams{}, fmt.Errorf("empty pattern in grep URI: %s", uri)
	}

	params := grepParams{Pattern: pathPart}

	if queryPart != "" {
		values, parseErr := url.ParseQuery(queryPart)
		if parseErr != nil {
			return grepParams{}, fmt.Errorf("invalid query params: %w", parseErr)
		}
		params.Path = values.Get("path")
		params.Glob = values.Get("glob")
		params.FileType = values.Get("type")
		params.OutputMode = values.Get("output_mode")
		if v := values.Get("context"); v != "" {
			params.Context, _ = strconv.Atoi(v)
		}
		if values.Get("case_insensitive") == "true" {
			params.CaseInsensitive = true
		}
	}

	return params, nil
}

func boolPtr(b bool) *bool { return &b }

// Interface assertions.
var (
	_ server.ResourceProviderV1 = (*folioServer)(nil)
	_ server.ToolProviderV1     = (*folioServer)(nil)
)

func main() {
	flag.Parse()

	if flag.NArg() < 1 {
		printUsage()
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "serve":
		if flag.NArg() < 2 || flag.Arg(1) != "mcp" {
			fmt.Fprintf(os.Stderr, "usage: folio serve mcp\n")
			os.Exit(1)
		}
		runServeMCP()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "usage: folio <command>\n\n")
	fmt.Fprintf(os.Stderr, "commands:\n")
	fmt.Fprintf(os.Stderr, "  serve mcp    run as MCP server\n")
}

func runServeMCP() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cfg, err := LoadDefaultFolioHierarchy()
	if err != nil {
		fmt.Fprintf(os.Stderr, "folio: loading config: %v\n", err)
		os.Exit(1)
	}

	t := transport.NewStdio(os.Stdin, os.Stdout)
	f := &folioServer{
		perms:   cfg.Permissions,
		readCfg: effectiveReadConfig(cfg.Read),
	}

	srv, err := server.New(t, server.Options{
		ServerName:    "folio",
		ServerVersion: "0.1.0",
		Instructions:  "File I/O server. Read files via the `read` tool (whole file), `read_range` (inclusive line range, replaces `sed -n 'N,Mp'`), or `read_excluding` (omit a line range, replaces `sed 'N,Md'`) — prefer these over shelling out to sed. Each tool's output begins with the equivalent folio://read/... resource URI so you can reference the view later. The same operations are also exposed as the folio://read/{path} resource with ?offset, ?limit, ?end, and ?delete=N-M query params. Find files via folio://glob/{pattern} with optional ?path={dir} (supports **). Search content via folio://grep/{pattern} with ?path, ?glob, ?type, ?output_mode, ?context, ?case_insensitive. Use the write tool to create/overwrite files, and the edit tool for exact string replacements.",
		Resources:     f,
		Tools:         f,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "folio: %v\n", err)
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "folio: %v\n", err)
		os.Exit(1)
	}
}
