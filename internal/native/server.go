package native

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// Server implements proxy.ServerBackend for native (config-declared) tools.
// It dispatches MCP method calls locally without spawning a child MCP server.
type Server struct {
	config  *NativeConfig
	toolIdx map[string]*ToolSpec
}

// NewServer constructs a Server from a parsed NativeConfig.
func NewServer(cfg *NativeConfig) *Server {
	idx := make(map[string]*ToolSpec, len(cfg.Tools))
	for i := range cfg.Tools {
		idx[cfg.Tools[i].Name] = &cfg.Tools[i]
	}
	return &Server{config: cfg, toolIdx: idx}
}

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
			tool.InputSchema = spec.Input
		} else {
			tool.InputSchema = json.RawMessage(`{"type":"object"}`)
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

	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	result := &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{protocol.TextContentV1(output)},
	}
	if err != nil {
		result.IsError = true
	}

	return marshalResult(result)
}

func marshalResult(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}
	return json.RawMessage(data), nil
}
