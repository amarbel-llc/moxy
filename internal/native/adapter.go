package native

import (
	"context"
	"encoding/json"
	"fmt"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
	"code.linenisgreat.com/purse-first/libs/go-mcp/server"
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
	return &protocol.ToolCallResult{
		Content: DowngradeContentBlocks(v1.Content),
		IsError: v1.IsError,
	}, nil
}

// DowngradeContentBlocks maps V1 content blocks to V0 ones. V1 resource
// blocks (what moxy's result caching produces) have no V0 equivalent, so they
// are flattened to text blocks carrying the resource's text. The proxy and the
// standalone serve-moxin adapter share it (moxy#507).
func DowngradeContentBlocks(blocks []protocol.ContentBlockV1) []protocol.ContentBlock {
	out := make([]protocol.ContentBlock, len(blocks))
	for i, b := range blocks {
		if b.Type == "resource" && b.Resource != nil {
			text := ""
			if b.Resource.Text != nil {
				text = *b.Resource.Text
			}
			out[i] = protocol.ContentBlock{Type: "text", Text: text}
			continue
		}
		out[i] = protocol.ContentBlock{
			Type:     b.Type,
			Text:     b.Text,
			MimeType: b.MimeType,
			Data:     b.Data,
		}
	}
	return out
}
