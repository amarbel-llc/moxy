package native

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
)

// ToolAdapter wraps a native Server to implement server.ToolProviderV1,
// allowing it to be served directly over stdio without the proxy layer.
type ToolAdapter struct {
	Srv *Server
}

var _ server.ToolProviderV1 = (*ToolAdapter)(nil)

func (a *ToolAdapter) ListToolsV1(ctx context.Context, _ string) (*protocol.ToolsListResultV1, error) {
	raw, err := a.Srv.Call(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("listing tools: %w", err)
	}
	var result protocol.ToolsListResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling tools list: %w", err)
	}
	return &result, nil
}

func (a *ToolAdapter) CallToolV1(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
	raw, err := a.Srv.Call(ctx, "tools/call", protocol.ToolCallParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return nil, fmt.Errorf("calling tool %q: %w", name, err)
	}
	var result protocol.ToolCallResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling tool call result: %w", err)
	}
	return &result, nil
}

func (a *ToolAdapter) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	v1, err := a.ListToolsV1(ctx, "")
	if err != nil {
		return nil, err
	}
	tools := make([]protocol.Tool, len(v1.Tools))
	for i, t := range v1.Tools {
		tools[i] = protocol.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return tools, nil
}

func (a *ToolAdapter) CallTool(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResult, error) {
	v1, err := a.CallToolV1(ctx, name, args)
	if err != nil {
		return nil, err
	}
	blocks := make([]protocol.ContentBlock, len(v1.Content))
	for i, b := range v1.Content {
		blocks[i] = protocol.ContentBlock{
			Type: b.Type,
			Text: b.Text,
		}
	}
	return &protocol.ToolCallResult{
		Content: blocks,
		IsError: v1.IsError,
	}, nil
}
