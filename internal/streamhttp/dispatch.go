package streamhttp

import (
	"context"
	"encoding/json"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
)

type dispatcher struct {
	tools     server.ToolProviderV1
	resources server.ResourceProviderV1
	prompts   server.PromptProviderV1

	serverName    string
	serverVersion string
	instructions  string
}

func (d *dispatcher) dispatch(ctx context.Context, msg *jsonrpc.Message) (*jsonrpc.Message, error) {
	switch msg.Method {
	case protocol.MethodInitialize:
		return d.handleInitialize(msg)

	case protocol.MethodInitialized:
		return nil, nil

	case protocol.MethodPing:
		return jsonrpc.NewResponse(*msg.ID, protocol.PingResult{})

	case protocol.MethodToolsList:
		return d.handleToolsList(ctx, msg)
	case protocol.MethodToolsCall:
		return d.handleToolsCall(ctx, msg)

	case protocol.MethodResourcesList:
		return d.handleResourcesList(ctx, msg)
	case protocol.MethodResourcesRead:
		return d.handleResourcesRead(ctx, msg)
	case protocol.MethodResourcesTemplates:
		return d.handleResourcesTemplates(ctx, msg)

	case protocol.MethodPromptsList:
		return d.handlePromptsList(ctx, msg)
	case protocol.MethodPromptsGet:
		return d.handlePromptsGet(ctx, msg)

	case protocol.MethodNotificationsProgress,
		protocol.MethodNotificationsCancelled,
		protocol.MethodNotificationsToolsListChanged,
		protocol.MethodNotificationsResourcesListChanged,
		protocol.MethodNotificationsResourceUpdated,
		protocol.MethodNotificationsPromptsListChanged,
		protocol.MethodNotificationsRootsListChanged:
		return nil, nil

	default:
		if msg.IsRequest() {
			return jsonrpc.NewErrorResponse(*msg.ID, jsonrpc.MethodNotFound,
				"method not found: "+msg.Method, nil)
		}
		return nil, nil
	}
}

func (d *dispatcher) handleInitialize(msg *jsonrpc.Message) (*jsonrpc.Message, error) {
	capabilities := protocol.ServerCapabilitiesV1{
		Tools:     &protocol.ToolsCapability{ListChanged: true},
		Resources: &protocol.ResourcesCapability{},
		Prompts:   &protocol.PromptsCapability{},
	}

	result := protocol.InitializeResultV1{
		ProtocolVersion: protocol.ProtocolVersionV1,
		Capabilities:    capabilities,
		ServerInfo: protocol.ImplementationV1{
			Name:    d.serverName,
			Version: d.serverVersion,
		},
		Instructions: d.instructions,
	}

	return jsonrpc.NewResponse(*msg.ID, result)
}

func (d *dispatcher) handleToolsList(ctx context.Context, msg *jsonrpc.Message) (*jsonrpc.Message, error) {
	cursor := parseCursor(msg.Params)
	result, err := d.tools.ListToolsV1(ctx, cursor)
	if err != nil {
		return jsonrpc.NewErrorResponse(*msg.ID, jsonrpc.InternalError, err.Error(), nil)
	}
	return jsonrpc.NewResponse(*msg.ID, result)
}

func (d *dispatcher) handleToolsCall(ctx context.Context, msg *jsonrpc.Message) (*jsonrpc.Message, error) {
	var params protocol.ToolCallParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return jsonrpc.NewErrorResponse(*msg.ID, jsonrpc.InvalidParams, "invalid params", nil)
	}
	result, err := d.tools.CallToolV1(ctx, params.Name, params.Arguments)
	if err != nil {
		return jsonrpc.NewErrorResponse(*msg.ID, jsonrpc.InternalError, err.Error(), nil)
	}
	return jsonrpc.NewResponse(*msg.ID, result)
}

func (d *dispatcher) handleResourcesList(ctx context.Context, msg *jsonrpc.Message) (*jsonrpc.Message, error) {
	cursor := parseCursor(msg.Params)
	result, err := d.resources.ListResourcesV1(ctx, cursor)
	if err != nil {
		return jsonrpc.NewErrorResponse(*msg.ID, jsonrpc.InternalError, err.Error(), nil)
	}
	return jsonrpc.NewResponse(*msg.ID, result)
}

func (d *dispatcher) handleResourcesRead(ctx context.Context, msg *jsonrpc.Message) (*jsonrpc.Message, error) {
	var params protocol.ResourceReadParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return jsonrpc.NewErrorResponse(*msg.ID, jsonrpc.InvalidParams, "invalid params", nil)
	}
	result, err := d.resources.ReadResource(ctx, params.URI)
	if err != nil {
		return jsonrpc.NewErrorResponse(*msg.ID, jsonrpc.InternalError, err.Error(), nil)
	}
	return jsonrpc.NewResponse(*msg.ID, result)
}

func (d *dispatcher) handleResourcesTemplates(ctx context.Context, msg *jsonrpc.Message) (*jsonrpc.Message, error) {
	cursor := parseCursor(msg.Params)
	result, err := d.resources.ListResourceTemplatesV1(ctx, cursor)
	if err != nil {
		return jsonrpc.NewErrorResponse(*msg.ID, jsonrpc.InternalError, err.Error(), nil)
	}
	return jsonrpc.NewResponse(*msg.ID, result)
}

func (d *dispatcher) handlePromptsList(ctx context.Context, msg *jsonrpc.Message) (*jsonrpc.Message, error) {
	cursor := parseCursor(msg.Params)
	result, err := d.prompts.ListPromptsV1(ctx, cursor)
	if err != nil {
		return jsonrpc.NewErrorResponse(*msg.ID, jsonrpc.InternalError, err.Error(), nil)
	}
	return jsonrpc.NewResponse(*msg.ID, result)
}

func (d *dispatcher) handlePromptsGet(ctx context.Context, msg *jsonrpc.Message) (*jsonrpc.Message, error) {
	var params protocol.PromptGetParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return jsonrpc.NewErrorResponse(*msg.ID, jsonrpc.InvalidParams, "invalid params", nil)
	}
	result, err := d.prompts.GetPromptV1(ctx, params.Name, params.Arguments)
	if err != nil {
		return jsonrpc.NewErrorResponse(*msg.ID, jsonrpc.InternalError, err.Error(), nil)
	}
	return jsonrpc.NewResponse(*msg.ID, result)
}

func parseCursor(params json.RawMessage) string {
	if params == nil {
		return ""
	}
	var pagination protocol.PaginationParams
	if err := json.Unmarshal(params, &pagination); err == nil {
		return pagination.Cursor
	}
	return ""
}
