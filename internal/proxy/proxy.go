package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/moxy/internal/mcpclient"
)

type ChildEntry struct {
	Client       *mcpclient.Client
	Config       config.ServerConfig
	Capabilities protocol.ServerCapabilitiesV1
}

type Proxy struct {
	children []ChildEntry
}

func New(children []ChildEntry) *Proxy {
	return &Proxy{children: children}
}

// --- ToolProvider (V0) ---

func (p *Proxy) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	v1, err := p.ListToolsV1(ctx, "")
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

func (p *Proxy) CallTool(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResult, error) {
	v1, err := p.CallToolV1(ctx, name, args)
	if err != nil {
		return nil, err
	}
	return &protocol.ToolCallResult{
		Content: downgradeContentBlocks(v1.Content),
		IsError: v1.IsError,
	}, nil
}

// --- ToolProviderV1 ---

func (p *Proxy) ListToolsV1(ctx context.Context, cursor string) (*protocol.ToolsListResultV1, error) {
	allTools := make([]protocol.ToolV1, 0)

	for _, child := range p.children {
		if child.Capabilities.Tools == nil {
			continue
		}

		raw, err := child.Client.Call(ctx, protocol.MethodToolsList, cursorParams(cursor))
		if err != nil {
			return nil, fmt.Errorf("listing tools from %s: %w", child.Client.Name(), err)
		}

		tools, err := decodeToolsList(raw)
		if err != nil {
			return nil, fmt.Errorf("decoding tools from %s: %w", child.Client.Name(), err)
		}

		for _, tool := range tools {
			if !matchesAnnotationFilter(tool.Annotations, child.Config.Annotations) {
				continue
			}
			tool.Name = child.Client.Name() + "-" + tool.Name
			allTools = append(allTools, tool)
		}
	}

	return &protocol.ToolsListResultV1{Tools: allTools}, nil
}

func (p *Proxy) CallToolV1(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
	serverName, toolName, ok := splitPrefix(name, "-")
	if !ok {
		return protocol.ErrorResultV1(fmt.Sprintf("invalid tool name %q: missing server prefix", name)), nil
	}

	child, ok := p.findChild(serverName)
	if !ok {
		return protocol.ErrorResultV1(fmt.Sprintf("unknown server %q", serverName)), nil
	}

	params := protocol.ToolCallParams{
		Name:      toolName,
		Arguments: args,
	}

	raw, err := child.Client.Call(ctx, protocol.MethodToolsCall, params)
	if err != nil {
		return nil, fmt.Errorf("calling tool %s on %s: %w", toolName, serverName, err)
	}

	result, err := decodeToolCallResult(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding tool call result from %s: %w", serverName, err)
	}

	return result, nil
}

// --- ResourceProvider (V0) ---

func (p *Proxy) ListResources(ctx context.Context) ([]protocol.Resource, error) {
	v1, err := p.ListResourcesV1(ctx, "")
	if err != nil {
		return nil, err
	}
	resources := make([]protocol.Resource, len(v1.Resources))
	for i, r := range v1.Resources {
		resources[i] = protocol.Resource{
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MimeType:    r.MimeType,
		}
	}
	return resources, nil
}

func (p *Proxy) ReadResource(ctx context.Context, uri string) (*protocol.ResourceReadResult, error) {
	serverName, originalURI, ok := splitPrefix(uri, "/")
	if !ok {
		return nil, fmt.Errorf("invalid resource URI %q: missing server prefix", uri)
	}

	child, ok := p.findChild(serverName)
	if !ok {
		return nil, fmt.Errorf("unknown server %q", serverName)
	}

	params := protocol.ResourceReadParams{URI: originalURI}

	raw, err := child.Client.Call(ctx, protocol.MethodResourcesRead, params)
	if err != nil {
		return nil, fmt.Errorf("reading resource %s from %s: %w", originalURI, serverName, err)
	}

	var result protocol.ResourceReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding resource read result from %s: %w", serverName, err)
	}

	return &result, nil
}

func (p *Proxy) ListResourceTemplates(ctx context.Context) ([]protocol.ResourceTemplate, error) {
	v1, err := p.ListResourceTemplatesV1(ctx, "")
	if err != nil {
		return nil, err
	}
	templates := make([]protocol.ResourceTemplate, len(v1.ResourceTemplates))
	for i, t := range v1.ResourceTemplates {
		templates[i] = protocol.ResourceTemplate{
			URITemplate: t.URITemplate,
			Name:        t.Name,
			Description: t.Description,
			MimeType:    t.MimeType,
		}
	}
	return templates, nil
}

// --- ResourceProviderV1 ---

func (p *Proxy) ListResourcesV1(ctx context.Context, cursor string) (*protocol.ResourcesListResultV1, error) {
	allResources := make([]protocol.ResourceV1, 0)

	for _, child := range p.children {
		if child.Capabilities.Resources == nil {
			continue
		}

		raw, err := child.Client.Call(ctx, protocol.MethodResourcesList, cursorParams(cursor))
		if err != nil {
			return nil, fmt.Errorf("listing resources from %s: %w", child.Client.Name(), err)
		}

		resources, err := decodeResourcesList(raw)
		if err != nil {
			return nil, fmt.Errorf("decoding resources from %s: %w", child.Client.Name(), err)
		}

		for _, r := range resources {
			r.URI = child.Client.Name() + "/" + r.URI
			allResources = append(allResources, r)
		}
	}

	return &protocol.ResourcesListResultV1{Resources: allResources}, nil
}

func (p *Proxy) ListResourceTemplatesV1(ctx context.Context, cursor string) (*protocol.ResourceTemplatesListResultV1, error) {
	allTemplates := make([]protocol.ResourceTemplateV1, 0)

	for _, child := range p.children {
		if child.Capabilities.Resources == nil {
			continue
		}

		raw, err := child.Client.Call(ctx, protocol.MethodResourcesTemplates, cursorParams(cursor))
		if err != nil {
			return nil, fmt.Errorf("listing resource templates from %s: %w", child.Client.Name(), err)
		}

		templates, err := decodeResourceTemplatesList(raw)
		if err != nil {
			return nil, fmt.Errorf("decoding resource templates from %s: %w", child.Client.Name(), err)
		}

		for _, t := range templates {
			t.URITemplate = child.Client.Name() + "/" + t.URITemplate
			allTemplates = append(allTemplates, t)
		}
	}

	return &protocol.ResourceTemplatesListResultV1{ResourceTemplates: allTemplates}, nil
}

// --- helpers ---

func (p *Proxy) findChild(name string) (ChildEntry, bool) {
	for _, c := range p.children {
		if c.Client.Name() == name {
			return c, true
		}
	}
	return ChildEntry{}, false
}

func splitPrefix(s, sep string) (prefix, rest string, ok bool) {
	i := strings.Index(s, sep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+len(sep):], true
}

func matchesAnnotationFilter(annotations *protocol.ToolAnnotations, filter *config.AnnotationFilter) bool {
	if filter == nil {
		return true
	}
	if filter.ReadOnlyHint != nil {
		if annotations == nil || annotations.ReadOnlyHint == nil || *annotations.ReadOnlyHint != *filter.ReadOnlyHint {
			return false
		}
	}
	if filter.DestructiveHint != nil {
		if annotations == nil || annotations.DestructiveHint == nil || *annotations.DestructiveHint != *filter.DestructiveHint {
			return false
		}
	}
	if filter.IdempotentHint != nil {
		if annotations == nil || annotations.IdempotentHint == nil || *annotations.IdempotentHint != *filter.IdempotentHint {
			return false
		}
	}
	if filter.OpenWorldHint != nil {
		if annotations == nil || annotations.OpenWorldHint == nil || *annotations.OpenWorldHint != *filter.OpenWorldHint {
			return false
		}
	}
	return true
}

type cursorParam struct {
	Cursor string `json:"cursor,omitempty"`
}

func cursorParams(cursor string) *cursorParam {
	if cursor == "" {
		return nil
	}
	return &cursorParam{Cursor: cursor}
}

// decodeToolsList tries V1 first, falls back to V0 and upgrades.
func decodeToolsList(raw json.RawMessage) ([]protocol.ToolV1, error) {
	var v1 protocol.ToolsListResultV1
	if err := json.Unmarshal(raw, &v1); err == nil && len(v1.Tools) > 0 {
		return v1.Tools, nil
	}

	var v0 protocol.ToolsListResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		tools := make([]protocol.ToolV1, len(v0.Tools))
		for i, t := range v0.Tools {
			tools[i] = protocol.ToolV1{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			}
		}
		return tools, nil
	}

	return nil, fmt.Errorf("unable to decode tools list response")
}

// decodeToolCallResult tries V1 first, falls back to V0 and upgrades.
func decodeToolCallResult(raw json.RawMessage) (*protocol.ToolCallResultV1, error) {
	var v1 protocol.ToolCallResultV1
	if err := json.Unmarshal(raw, &v1); err == nil {
		return &v1, nil
	}

	var v0 protocol.ToolCallResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		return &protocol.ToolCallResultV1{
			Content: upgradeContentBlocks(v0.Content),
			IsError: v0.IsError,
		}, nil
	}

	return nil, fmt.Errorf("unable to decode tool call result")
}

// decodeResourcesList tries V1 first, falls back to V0 and upgrades.
func decodeResourcesList(raw json.RawMessage) ([]protocol.ResourceV1, error) {
	var v1 protocol.ResourcesListResultV1
	if err := json.Unmarshal(raw, &v1); err == nil && len(v1.Resources) > 0 {
		return v1.Resources, nil
	}

	var v0 protocol.ResourcesListResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		resources := make([]protocol.ResourceV1, len(v0.Resources))
		for i, r := range v0.Resources {
			resources[i] = protocol.ResourceV1{
				URI:         r.URI,
				Name:        r.Name,
				Description: r.Description,
				MimeType:    r.MimeType,
			}
		}
		return resources, nil
	}

	return nil, fmt.Errorf("unable to decode resources list response")
}

// decodeResourceTemplatesList tries V1 first, falls back to V0 and upgrades.
func decodeResourceTemplatesList(raw json.RawMessage) ([]protocol.ResourceTemplateV1, error) {
	var v1 protocol.ResourceTemplatesListResultV1
	if err := json.Unmarshal(raw, &v1); err == nil && len(v1.ResourceTemplates) > 0 {
		return v1.ResourceTemplates, nil
	}

	var v0 protocol.ResourceTemplatesListResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		templates := make([]protocol.ResourceTemplateV1, len(v0.ResourceTemplates))
		for i, t := range v0.ResourceTemplates {
			templates[i] = protocol.ResourceTemplateV1{
				URITemplate: t.URITemplate,
				Name:        t.Name,
				Description: t.Description,
				MimeType:    t.MimeType,
			}
		}
		return templates, nil
	}

	return nil, fmt.Errorf("unable to decode resource templates list response")
}

func downgradeContentBlocks(blocks []protocol.ContentBlockV1) []protocol.ContentBlock {
	out := make([]protocol.ContentBlock, len(blocks))
	for i, b := range blocks {
		out[i] = protocol.ContentBlock{
			Type:     b.Type,
			Text:     b.Text,
			MimeType: b.MimeType,
			Data:     b.Data,
		}
	}
	return out
}

func upgradeContentBlocks(blocks []protocol.ContentBlock) []protocol.ContentBlockV1 {
	out := make([]protocol.ContentBlockV1, len(blocks))
	for i, b := range blocks {
		out[i] = protocol.ContentBlockV1{
			Type:     b.Type,
			Text:     b.Text,
			MimeType: b.MimeType,
			Data:     b.Data,
		}
	}
	return out
}
