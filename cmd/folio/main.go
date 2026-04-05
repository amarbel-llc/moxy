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
		Description: "Read a file with line numbers. Use ?offset=N (1-indexed) and ?limit=M for pagination.",
		MimeType:    "text/plain",
	},
}

// Resource templates (V1).

var templatesV1 = []protocol.ResourceTemplateV1{
	{
		URITemplate: "folio://read/{+path}",
		Name:        "Read file",
		Description: "Read a file with line numbers. Use ?offset=N (1-indexed) and ?limit=M for pagination.",
		MimeType:    "text/plain",
	},
}

// ResourceProvider (base interface)

func (s *folioServer) ListResources(_ context.Context) ([]protocol.Resource, error) {
	return nil, nil
}

func (s *folioServer) ReadResource(_ context.Context, uri string) (*protocol.ResourceReadResult, error) {
	path, offset, limit, err := parseReadURI(uri)
	if err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(path)
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

	content, totalLines, err := readFileWithLineNumbers(absPath, offset, limit)
	if err != nil {
		return nil, err
	}

	// Progressive disclosure: if file is large and no pagination requested.
	if offset == 0 && limit == 0 && totalLines > s.readCfg.MaxLines {
		headContent, _, _ := readFileWithLineNumbers(absPath, 1, s.readCfg.HeadLines)
		tailOffset := totalLines - s.readCfg.TailLines + 1
		if tailOffset < 1 {
			tailOffset = 1
		}
		tailContent, _, _ := readFileWithLineNumbers(absPath, tailOffset, s.readCfg.TailLines)
		resourceURI := fmt.Sprintf("folio://read/%s", absPath)
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

func (s *folioServer) ListTools(_ context.Context) ([]protocol.Tool, error) {
	return []protocol.Tool{
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
	case "write":
		return s.handleWrite(args)
	case "edit":
		return s.handleEdit(args)
	default:
		return protocol.ErrorResultV1(fmt.Sprintf("unknown tool %q", name)), nil
	}
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

func parseReadURI(uri string) (path string, offset, limit int, err error) {
	// Expected: folio://read/<path>?offset=N&limit=M
	if !strings.HasPrefix(uri, "folio://read/") {
		return "", 0, 0, fmt.Errorf("invalid folio URI: %s", uri)
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
		return "", 0, 0, fmt.Errorf("empty path in URI: %s", uri)
	}

	if queryPart != "" {
		values, parseErr := url.ParseQuery(queryPart)
		if parseErr != nil {
			return "", 0, 0, fmt.Errorf("invalid query params: %w", parseErr)
		}
		if v := values.Get("offset"); v != "" {
			offset, err = strconv.Atoi(v)
			if err != nil {
				return "", 0, 0, fmt.Errorf("invalid offset: %w", err)
			}
		}
		if v := values.Get("limit"); v != "" {
			limit, err = strconv.Atoi(v)
			if err != nil {
				return "", 0, 0, fmt.Errorf("invalid limit: %w", err)
			}
		}
	}

	return pathPart, offset, limit, nil
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
		Instructions:  "File I/O server. Read files via folio://read/{path} resource with optional ?offset=N&limit=M for line-based pagination. Use the write tool to create or overwrite files, and the edit tool for exact string replacements within existing files.",
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
