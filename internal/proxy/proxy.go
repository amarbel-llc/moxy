package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"github.com/amarbel-llc/moxy/internal/config"
	"github.com/amarbel-llc/moxy/internal/mcpclient"
	"github.com/amarbel-llc/moxy/internal/paginate"
)

type ChildEntry struct {
	Client       *mcpclient.Client
	Config       config.ServerConfig
	Capabilities protocol.ServerCapabilitiesV1
	ServerInfo   protocol.ImplementationV1
	Instructions string
}

type FailedServer struct {
	Name  string
	Error string
}

type EphemeralMeta struct {
	Config       config.ServerConfig
	Capabilities protocol.ServerCapabilitiesV1
	ServerInfo   protocol.ImplementationV1
	Instructions string
	Tools        []protocol.ToolV1
	Resources    []protocol.ResourceV1
	Templates    []protocol.ResourceTemplateV1
	Prompts      []protocol.PromptV1
}

type Proxy struct {
	children                    []ChildEntry
	failed                      []FailedServer
	configs                     map[string]config.ServerConfig
	ephemeral                   map[string]*EphemeralMeta
	globalEphemeral             *bool
	globalProgressiveDisclosure *bool
	notifier                    func(*jsonrpc.Message) error
	mu                          sync.RWMutex
}

func (p *Proxy) SetNotifier(fn func(*jsonrpc.Message) error) {
	p.notifier = fn
}

func (p *Proxy) ForwardNotification(msg *jsonrpc.Message) {
	if p.notifier != nil {
		p.notifier(msg)
	}
}

func New(
	children []ChildEntry,
	failed []FailedServer,
	allConfigs []config.ServerConfig,
	globalEphemeral *bool,
	globalProgressiveDisclosure *bool,
) *Proxy {
	configs := make(map[string]config.ServerConfig, len(allConfigs))
	ephemeral := make(map[string]*EphemeralMeta)
	for _, cfg := range allConfigs {
		configs[cfg.Name] = cfg
		if cfg.IsEphemeral(globalEphemeral) {
			ephemeral[cfg.Name] = &EphemeralMeta{Config: cfg}
		}
	}
	return &Proxy{
		children:                    children,
		failed:                      failed,
		configs:                     configs,
		ephemeral:                   ephemeral,
		globalEphemeral:             globalEphemeral,
		globalProgressiveDisclosure: globalProgressiveDisclosure,
	}
}

func (p *Proxy) ProbeEphemeral(ctx context.Context) {
	for name, meta := range p.ephemeral {
		cfg := meta.Config
		exe, cmdArgs := cfg.EffectiveCommand()
		client, result, err := mcpclient.SpawnAndInitialize(
			ctx, cfg.Name, exe, cmdArgs,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "moxy: failed to probe ephemeral %s: %v\n", name, err)
			p.markFailed(name, err)
			continue
		}

		meta.Capabilities = result.Capabilities
		meta.ServerInfo = result.ServerInfo
		meta.Instructions = result.Instructions

		if result.Capabilities.Tools != nil {
			raw, err := client.Call(ctx, protocol.MethodToolsList, nil)
			if err == nil {
				if tools, err := decodeToolsList(raw); err == nil {
					meta.Tools = tools
				}
			}
		}

		if result.Capabilities.Resources != nil {
			raw, err := client.Call(ctx, protocol.MethodResourcesList, nil)
			if err == nil {
				if resources, err := decodeResourcesList(raw); err == nil {
					meta.Resources = resources
				}
			}
			raw, err = client.Call(ctx, protocol.MethodResourcesTemplates, nil)
			if err == nil {
				if templates, err := decodeResourceTemplatesList(raw); err == nil {
					meta.Templates = templates
				}
			}
		}

		if result.Capabilities.Prompts != nil {
			raw, err := client.Call(ctx, protocol.MethodPromptsList, nil)
			if err == nil {
				if prompts, err := decodePromptsList(raw); err == nil {
					meta.Prompts = prompts
				}
			}
		}

		client.Close()
		fmt.Fprintf(os.Stderr, "moxy: probed ephemeral %s (%s %s)\n",
			name, result.ServerInfo.Name, result.ServerInfo.Version)
	}
}

func (p *Proxy) reprobeEphemeral(ctx context.Context, meta *EphemeralMeta) error {
	cfg := meta.Config
	exe, cmdArgs := cfg.EffectiveCommand()
	client, result, err := mcpclient.SpawnAndInitialize(
		ctx, cfg.Name, exe, cmdArgs,
	)
	if err != nil {
		return fmt.Errorf("re-probing ephemeral %s: %w", cfg.Name, err)
	}
	defer client.Close()

	meta.Capabilities = result.Capabilities
	meta.ServerInfo = result.ServerInfo
	meta.Instructions = result.Instructions
	meta.Tools = nil
	meta.Resources = nil
	meta.Templates = nil
	meta.Prompts = nil

	if result.Capabilities.Tools != nil {
		raw, err := client.Call(ctx, protocol.MethodToolsList, nil)
		if err == nil {
			if tools, err := decodeToolsList(raw); err == nil {
				meta.Tools = tools
			}
		}
	}

	if result.Capabilities.Resources != nil {
		raw, err := client.Call(ctx, protocol.MethodResourcesList, nil)
		if err == nil {
			if resources, err := decodeResourcesList(raw); err == nil {
				meta.Resources = resources
			}
		}
		raw, err = client.Call(ctx, protocol.MethodResourcesTemplates, nil)
		if err == nil {
			if templates, err := decodeResourceTemplatesList(raw); err == nil {
				meta.Templates = templates
			}
		}
	}

	if result.Capabilities.Prompts != nil {
		raw, err := client.Call(ctx, protocol.MethodPromptsList, nil)
		if err == nil {
			if prompts, err := decodePromptsList(raw); err == nil {
				meta.Prompts = prompts
			}
		}
	}

	return nil
}

func (p *Proxy) spawnEphemeral(ctx context.Context, serverName string) (*mcpclient.Client, error) {
	cfg, ok := p.configs[serverName]
	if !ok {
		return nil, fmt.Errorf("unknown server %q", serverName)
	}
	exe, cmdArgs := cfg.EffectiveCommand()
	client, _, err := mcpclient.SpawnAndInitialize(
		ctx, cfg.Name, exe, cmdArgs,
	)
	if err != nil {
		return nil, fmt.Errorf("spawning ephemeral %s: %w", serverName, err)
	}
	client.SetOnNotification(func(msg *jsonrpc.Message) {
		p.ForwardNotification(msg)
	})
	return client, nil
}

func (p *Proxy) markFailed(name string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, f := range p.failed {
		if f.Name == name {
			return
		}
	}
	p.failed = append(p.failed, FailedServer{
		Name:  name,
		Error: err.Error(),
	})
}

func (p *Proxy) CollectServerSummaries(ctx context.Context) []ServerSummary {
	p.mu.RLock()
	children := p.children
	failed := p.failed
	p.mu.RUnlock()

	var summaries []ServerSummary

	for _, child := range children {
		s := ServerSummary{
			Name:         child.Config.Name,
			Version:      child.ServerInfo.Version,
			Instructions: child.Instructions,
			Status:       "running",
		}

		if child.Capabilities.Tools != nil {
			raw, err := child.Client.Call(ctx, protocol.MethodToolsList, nil)
			if err == nil {
				if tools, err := decodeToolsList(raw); err == nil {
					s.Tools = len(tools)
				}
			}
		}

		if child.Capabilities.Resources != nil {
			raw, err := child.Client.Call(ctx, protocol.MethodResourcesList, nil)
			if err == nil {
				if resources, err := decodeResourcesList(raw); err == nil {
					s.Resources = len(resources)
				}
			}
			raw, err = child.Client.Call(ctx, protocol.MethodResourcesTemplates, nil)
			if err == nil {
				if templates, err := decodeResourceTemplatesList(raw); err == nil {
					s.ResourceTemplates = len(templates)
				}
			}
		}

		if child.Capabilities.Prompts != nil {
			raw, err := child.Client.Call(ctx, protocol.MethodPromptsList, nil)
			if err == nil {
				if prompts, err := decodePromptsList(raw); err == nil {
					s.Prompts = len(prompts)
				}
			}
		}

		summaries = append(summaries, s)
	}

	for name, meta := range p.ephemeral {
		summaries = append(summaries, ServerSummary{
			Name:              name,
			Version:           meta.ServerInfo.Version,
			Instructions:      meta.Instructions,
			Status:            "running",
			Tools:             len(meta.Tools),
			Resources:         len(meta.Resources),
			ResourceTemplates: len(meta.Templates),
			Prompts:           len(meta.Prompts),
		})
	}

	for _, f := range failed {
		summaries = append(summaries, ServerSummary{
			Name:   f.Name,
			Status: "failed",
			Error:  f.Error,
		})
	}

	return summaries
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

func (p *Proxy) CallTool(
	ctx context.Context,
	name string,
	args json.RawMessage,
) (*protocol.ToolCallResult, error) {
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

func (p *Proxy) ListToolsV1(
	ctx context.Context,
	cursor string,
) (*protocol.ToolsListResultV1, error) {
	p.mu.RLock()
	children := p.children
	failed := p.failed
	p.mu.RUnlock()

	allTools := make([]protocol.ToolV1, 0)

	for _, child := range children {
		if child.Capabilities.Tools == nil {
			continue
		}
		if child.Config.IsProgressiveDisclosure(p.globalProgressiveDisclosure) {
			continue
		}

		raw, err := child.Client.Call(
			ctx,
			protocol.MethodToolsList,
			cursorParams(cursor),
		)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("listing tools: %w", err),
			)
			continue
		}

		tools, err := decodeToolsList(raw)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("decoding tools: %w", err),
			)
			continue
		}

		for _, tool := range tools {
			if !matchesAnnotationFilter(
				tool.Annotations,
				child.Config.Annotations,
			) {
				continue
			}
			tool.Name = child.Client.Name() + "." + tool.Name
			prefixToolTitle(&tool, child.Client.Name())
			allTools = append(allTools, tool)
		}
	}

	// Inject synthetic resource tools for resource-capable children
	for _, child := range children {
		if child.Capabilities.Resources == nil {
			continue
		}
		if child.Config.GenerateResourceTools != nil && !*child.Config.GenerateResourceTools {
			continue
		}
		if child.Config.IsProgressiveDisclosure(p.globalProgressiveDisclosure) {
			continue
		}

		serverName := child.Client.Name()

		// Check for collisions with child's own tools
		hasResourceRead := false
		hasResourceTemplates := false
		for _, t := range allTools {
			if t.Name == serverName+".resource-read" {
				hasResourceRead = true
			}
			if t.Name == serverName+".resource-templates" {
				hasResourceTemplates = true
			}
		}

		if !hasResourceRead {
			allTools = append(allTools, protocol.ToolV1{
				Name:        serverName + ".resource-read",
				Title:       serverName + ": Read Resource",
				Description: fmt.Sprintf("Read a resource from %s by URI", serverName),
				InputSchema: json.RawMessage(`{"type":"object","properties":{"uri":{"type":"string","description":"Resource URI"}},"required":["uri"]}`),
				Annotations: readOnlyAnnotations(),
			})
		}

		if !hasResourceTemplates {
			allTools = append(allTools, protocol.ToolV1{
				Name:        serverName + ".resource-templates",
				Title:       serverName + ": List Resource Templates",
				Description: fmt.Sprintf("List available resource templates for %s", serverName),
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Annotations: readOnlyAnnotations(),
			})
		}
	}

	// Append cached tools from ephemeral servers
	for serverName, meta := range p.ephemeral {
		if !meta.Config.IsProgressiveDisclosure(p.globalProgressiveDisclosure) {
			for _, tool := range meta.Tools {
				if !matchesAnnotationFilter(tool.Annotations, meta.Config.Annotations) {
					continue
				}
				tool.Name = serverName + "." + tool.Name
				prefixToolTitle(&tool, serverName)
				allTools = append(allTools, tool)
			}
			if meta.Capabilities.Resources != nil {
				grt := meta.Config.GenerateResourceTools
				if grt == nil || *grt {
					allTools = append(allTools, protocol.ToolV1{
						Name:        serverName + ".resource-read",
						Title:       serverName + ": Read Resource",
						Description: fmt.Sprintf("Read a resource from %s by URI", serverName),
						InputSchema: json.RawMessage(`{"type":"object","properties":{"uri":{"type":"string","description":"Resource URI"}},"required":["uri"]}`),
						Annotations: readOnlyAnnotations(),
					})
					allTools = append(allTools, protocol.ToolV1{
						Name:        serverName + ".resource-templates",
						Title:       serverName + ": List Resource Templates",
						Description: fmt.Sprintf("List available resource templates for %s", serverName),
						InputSchema: json.RawMessage(`{"type":"object"}`),
						Annotations: readOnlyAnnotations(),
					})
				}
			}
		}
	}

	for _, f := range failed {
		allTools = append(allTools, protocol.ToolV1{
			Name:  f.Name + ".status",
			Title: f.Name + ": Server Status",
			Description: fmt.Sprintf(
				"Server %q failed to start: %s",
				f.Name,
				f.Error,
			),
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Annotations: readOnlyAnnotations(),
		})
	}

	allTools = append(allTools, protocol.ToolV1{
		Name:        "restart",
		Title:       "Restart Server",
		Description: "Restart an MCP server by name. Closes and re-spawns the server process.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"server":{"type":"string","description":"Server name to restart"}},"required":["server"]}`),
		Annotations: &protocol.ToolAnnotations{
			Title:           "Restart Server",
			DestructiveHint: boolPtr(true),
			IdempotentHint:  boolPtr(true),
		},
	})

	allTools = append(allTools, protocol.ToolV1{
		Name:        "exec-mcp",
		Title:       "Execute Tool on Server",
		Description: "Execute a tool on a child server by name. Use moxy:// resources to discover available tools and schemas.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"server":{"type":"string","description":"Server name"},"tool":{"type":"string","description":"Tool name"},"arguments":{"type":"object","description":"Tool arguments"}},"required":["server","tool"]}`),
		Annotations: &protocol.ToolAnnotations{
			Title:         "Execute Tool on Server",
			OpenWorldHint: boolPtr(true),
		},
	})

	return &protocol.ToolsListResultV1{Tools: allTools}, nil
}

func (p *Proxy) CallToolV1(
	ctx context.Context,
	name string,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	if name == "restart" {
		return p.handleRestart(ctx, args)
	}

	if name == "exec-mcp" {
		return p.handleExecMCP(ctx, args)
	}

	p.mu.RLock()
	children := p.children
	failed := p.failed
	p.mu.RUnlock()

	serverName, toolName, ok := splitPrefix(name, ".")
	if !ok {
		return protocol.ErrorResultV1(
			fmt.Sprintf("invalid tool name %q: missing server prefix", name),
		), nil
	}

	if toolName == "status" {
		for _, f := range failed {
			if f.Name == serverName {
				return protocol.ErrorResultV1(
					fmt.Sprintf(
						"server %q failed to start: %s",
						f.Name,
						f.Error,
					),
				), nil
			}
		}
	}

	child, ok := findChildIn(children, serverName)
	if !ok {
		// Check if this is an ephemeral server
		if _, isEphemeral := p.ephemeral[serverName]; isEphemeral {
			return p.callToolEphemeral(ctx, serverName, toolName, args)
		}
		return protocol.ErrorResultV1(
			fmt.Sprintf("unknown server %q", serverName),
		), nil
	}

	if toolName == "resource-read" {
		return p.callResourceRead(ctx, child, args)
	}

	if toolName == "resource-templates" {
		return p.callResourceTemplates(ctx, child)
	}

	params := protocol.ToolCallParams{
		Name:      toolName,
		Arguments: args,
	}

	raw, err := child.Client.Call(ctx, protocol.MethodToolsCall, params)
	if err != nil {
		return nil, fmt.Errorf(
			"calling tool %s on %s: %w",
			toolName,
			serverName,
			err,
		)
	}

	result, err := decodeToolCallResult(raw)
	if err != nil {
		return nil, fmt.Errorf(
			"decoding tool call result from %s: %w",
			serverName,
			err,
		)
	}

	return result, nil
}

// --- ResourceProvider (V0) ---

func (p *Proxy) ListResources(
	ctx context.Context,
) ([]protocol.Resource, error) {
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

func (p *Proxy) ReadResource(
	ctx context.Context,
	uri string,
) (*protocol.ResourceReadResult, error) {
	if strings.HasPrefix(uri, "moxy://") {
		return p.readMoxyResource(ctx, uri)
	}

	serverName, originalURI, ok := splitPrefix(uri, "/")
	if !ok {
		return nil, fmt.Errorf(
			"invalid resource URI %q: missing server prefix",
			uri,
		)
	}

	child, ok := p.findChild(serverName)
	if !ok {
		if _, isEphemeral := p.ephemeral[serverName]; isEphemeral {
			return p.readResourceEphemeral(ctx, serverName, originalURI)
		}
		return p.resourceFallbackUnknownServer(uri, serverName)
	}

	if child.Capabilities.Resources == nil {
		return p.resourceFallbackNoResources(uri, serverName)
	}

	// Parse and strip pagination params if server has paginate enabled
	var pgParams paginate.Params
	if child.Config.Paginate {
		originalURI, pgParams = paginate.ParseParams(originalURI)
	}

	params := protocol.ResourceReadParams{URI: originalURI}

	raw, err := child.Client.Call(ctx, protocol.MethodResourcesRead, params)
	if err != nil {
		return nil, fmt.Errorf(
			"reading resource %s from %s: %w",
			originalURI,
			serverName,
			err,
		)
	}

	var result protocol.ResourceReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf(
			"decoding resource read result from %s: %w",
			serverName,
			err,
		)
	}

	if pgParams.Active {
		result = paginateResourceResult(result, pgParams)
	}

	return &result, nil
}

func (p *Proxy) ListResourceTemplates(
	ctx context.Context,
) ([]protocol.ResourceTemplate, error) {
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

func (p *Proxy) ListResourcesV1(
	ctx context.Context,
	cursor string,
) (*protocol.ResourcesListResultV1, error) {
	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()

	allResources := make([]protocol.ResourceV1, 0)

	for _, child := range children {
		if child.Capabilities.Resources == nil {
			continue
		}

		raw, err := child.Client.Call(
			ctx,
			protocol.MethodResourcesList,
			cursorParams(cursor),
		)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("listing resources: %w", err),
			)
			continue
		}

		resources, err := decodeResourcesList(raw)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("decoding resources: %w", err),
			)
			continue
		}

		for _, r := range resources {
			r.URI = child.Client.Name() + "/" + r.URI
			allResources = append(allResources, r)
		}
	}

	for serverName, meta := range p.ephemeral {
		for _, r := range meta.Resources {
			r.URI = serverName + "/" + r.URI
			allResources = append(allResources, r)
		}
	}

	// moxy:// tool schema resources for each server with tools
	for _, child := range children {
		if child.Capabilities.Tools != nil {
			allResources = append(allResources, protocol.ResourceV1{
				URI:         "moxy://tools/" + child.Client.Name(),
				Name:        child.Client.Name() + " tools",
				Description: fmt.Sprintf("List of tools available on %s", child.Client.Name()),
				MimeType:    "application/json",
			})
		}
	}
	for serverName, meta := range p.ephemeral {
		if len(meta.Tools) > 0 {
			allResources = append(allResources, protocol.ResourceV1{
				URI:         "moxy://tools/" + serverName,
				Name:        serverName + " tools",
				Description: fmt.Sprintf("List of tools available on %s", serverName),
				MimeType:    "application/json",
			})
		}
	}

	// moxy://servers static resource
	allResources = append(allResources, protocol.ResourceV1{
		URI:         "moxy://servers",
		Name:        "servers",
		Description: "List of all child servers with capability counts and status",
		MimeType:    "application/json",
	})

	return &protocol.ResourcesListResultV1{Resources: allResources}, nil
}

func (p *Proxy) ListResourceTemplatesV1(
	ctx context.Context,
	cursor string,
) (*protocol.ResourceTemplatesListResultV1, error) {
	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()

	allTemplates := make([]protocol.ResourceTemplateV1, 0)

	for _, child := range children {
		if child.Capabilities.Resources == nil {
			continue
		}

		raw, err := child.Client.Call(
			ctx,
			protocol.MethodResourcesTemplates,
			cursorParams(cursor),
		)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("listing resource templates: %w", err),
			)
			continue
		}

		templates, err := decodeResourceTemplatesList(raw)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("decoding resource templates: %w", err),
			)
			continue
		}

		for _, t := range templates {
			t.URITemplate = child.Client.Name() + "/" + t.URITemplate
			allTemplates = append(allTemplates, t)
		}
	}

	for serverName, meta := range p.ephemeral {
		for _, t := range meta.Templates {
			t.URITemplate = serverName + "/" + t.URITemplate
			allTemplates = append(allTemplates, t)
		}
	}

	// moxy:// resource templates
	allTemplates = append(allTemplates, protocol.ResourceTemplateV1{
		URITemplate: "moxy://servers/{server}",
		Name:        "Server details",
		Description: "Returns capability counts and status for a single child server",
		MimeType:    "application/json",
	})
	allTemplates = append(allTemplates, protocol.ResourceTemplateV1{
		URITemplate: "moxy://tools/{server}",
		Name:        "List tools for a server",
		Description: "Returns tool names and descriptions for a child server",
		MimeType:    "application/json",
	})
	allTemplates = append(allTemplates, protocol.ResourceTemplateV1{
		URITemplate: "moxy://tools/{server}/{tool}",
		Name:        "Tool schema",
		Description: "Returns the full JSON schema for a specific tool on a child server",
		MimeType:    "application/json",
	})

	return &protocol.ResourceTemplatesListResultV1{
		ResourceTemplates: allTemplates,
	}, nil
}

// --- PromptProvider (V0) ---

func (p *Proxy) ListPrompts(ctx context.Context) ([]protocol.Prompt, error) {
	v1, err := p.ListPromptsV1(ctx, "")
	if err != nil {
		return nil, err
	}
	prompts := make([]protocol.Prompt, len(v1.Prompts))
	for i, pr := range v1.Prompts {
		prompts[i] = protocol.Prompt{
			Name:        pr.Name,
			Description: pr.Description,
			Arguments:   pr.Arguments,
		}
	}
	return prompts, nil
}

func (p *Proxy) GetPrompt(
	ctx context.Context,
	name string,
	args map[string]string,
) (*protocol.PromptGetResult, error) {
	v1, err := p.GetPromptV1(ctx, name, args)
	if err != nil {
		return nil, err
	}
	messages := make([]protocol.PromptMessage, len(v1.Messages))
	for i, m := range v1.Messages {
		messages[i] = protocol.PromptMessage{
			Role: m.Role,
			Content: protocol.ContentBlock{
				Type:     m.Content.Type,
				Text:     m.Content.Text,
				MimeType: m.Content.MimeType,
				Data:     m.Content.Data,
			},
		}
	}
	return &protocol.PromptGetResult{
		Description: v1.Description,
		Messages:    messages,
	}, nil
}

// --- PromptProviderV1 ---

func (p *Proxy) ListPromptsV1(
	ctx context.Context,
	cursor string,
) (*protocol.PromptsListResultV1, error) {
	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()

	allPrompts := make([]protocol.PromptV1, 0)

	for _, child := range children {
		if child.Capabilities.Prompts == nil {
			continue
		}

		raw, err := child.Client.Call(
			ctx,
			protocol.MethodPromptsList,
			cursorParams(cursor),
		)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("listing prompts: %w", err),
			)
			continue
		}

		prompts, err := decodePromptsList(raw)
		if err != nil {
			p.markFailed(
				child.Client.Name(),
				fmt.Errorf("decoding prompts: %w", err),
			)
			continue
		}

		for _, pr := range prompts {
			pr.Name = child.Client.Name() + "." + pr.Name
			allPrompts = append(allPrompts, pr)
		}
	}

	for serverName, meta := range p.ephemeral {
		for _, pr := range meta.Prompts {
			pr.Name = serverName + "." + pr.Name
			allPrompts = append(allPrompts, pr)
		}
	}

	return &protocol.PromptsListResultV1{Prompts: allPrompts}, nil
}

func (p *Proxy) GetPromptV1(
	ctx context.Context,
	name string,
	args map[string]string,
) (*protocol.PromptGetResultV1, error) {
	serverName, promptName, ok := splitPrefix(name, ".")
	if !ok {
		return nil, fmt.Errorf(
			"invalid prompt name %q: missing server prefix",
			name,
		)
	}

	child, ok := p.findChild(serverName)
	if !ok {
		if _, isEphemeral := p.ephemeral[serverName]; isEphemeral {
			return p.getPromptEphemeral(ctx, serverName, promptName, args)
		}
		return nil, fmt.Errorf("unknown server %q", serverName)
	}

	params := protocol.PromptGetParams{
		Name:      promptName,
		Arguments: args,
	}

	raw, err := child.Client.Call(ctx, protocol.MethodPromptsGet, params)
	if err != nil {
		return nil, fmt.Errorf(
			"getting prompt %s from %s: %w",
			promptName,
			serverName,
			err,
		)
	}

	result, err := decodePromptGetResult(raw)
	if err != nil {
		return nil, fmt.Errorf(
			"decoding prompt get result from %s: %w",
			serverName,
			err,
		)
	}

	return result, nil
}

// --- helpers ---

func (p *Proxy) handleRestart(
	ctx context.Context,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	var params struct {
		Server string `json:"server"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("invalid restart args: %v", err),
		), nil
	}
	if params.Server == "" {
		return protocol.ErrorResultV1("server name is required"), nil
	}
	if err := p.restartServer(ctx, params.Server); err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("restart failed: %v", err),
		), nil
	}
	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			{Type: "text", Text: fmt.Sprintf("Server %q restarted successfully", params.Server)},
		},
	}, nil
}

func (p *Proxy) handleExecMCP(
	ctx context.Context,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	var params struct {
		Server    string          `json:"server"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("invalid exec-mcp args: %v", err),
		), nil
	}
	if params.Server == "" {
		return protocol.ErrorResultV1("server name is required"), nil
	}
	if params.Tool == "" {
		return protocol.ErrorResultV1("tool name is required"), nil
	}

	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()

	callParams := protocol.ToolCallParams{
		Name:      params.Tool,
		Arguments: params.Arguments,
	}

	child, ok := findChildIn(children, params.Server)
	if !ok {
		if meta, isEphemeral := p.ephemeral[params.Server]; isEphemeral {
			if err := validateToolExists(params.Tool, meta.Tools, meta.Config.Annotations); err != nil {
				return protocol.ErrorResultV1(err.Error()), nil
			}

			client, err := p.spawnEphemeral(ctx, params.Server)
			if err != nil {
				return nil, err
			}
			defer client.Close()

			raw, err := client.Call(ctx, protocol.MethodToolsCall, callParams)
			if err != nil {
				return nil, fmt.Errorf("calling tool %s on ephemeral %s: %w", params.Tool, params.Server, err)
			}
			return decodeToolCallResult(raw)
		}
		return protocol.ErrorResultV1(
			fmt.Sprintf("unknown server %q", params.Server),
		), nil
	}

	tools, err := p.getToolsForServer(ctx, params.Server)
	if err != nil {
		return nil, err
	}
	if err := validateToolExists(params.Tool, tools, child.Config.Annotations); err != nil {
		return protocol.ErrorResultV1(err.Error()), nil
	}

	raw, err := child.Client.Call(ctx, protocol.MethodToolsCall, callParams)
	if err != nil {
		return nil, fmt.Errorf("calling tool %s on %s: %w", params.Tool, params.Server, err)
	}
	return decodeToolCallResult(raw)
}

func (p *Proxy) getToolsForServer(ctx context.Context, serverName string) ([]protocol.ToolV1, error) {
	if meta, ok := p.ephemeral[serverName]; ok {
		return meta.Tools, nil
	}

	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()

	child, ok := findChildIn(children, serverName)
	if !ok {
		return nil, fmt.Errorf("unknown server %q", serverName)
	}

	if child.Capabilities.Tools == nil {
		return nil, nil
	}

	raw, err := child.Client.Call(ctx, protocol.MethodToolsList, nil)
	if err != nil {
		return nil, fmt.Errorf("listing tools from %s: %w", serverName, err)
	}

	tools, err := decodeToolsList(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding tools from %s: %w", serverName, err)
	}

	return tools, nil
}

func (p *Proxy) readMoxyResource(
	ctx context.Context,
	uri string,
) (*protocol.ResourceReadResult, error) {
	path := strings.TrimPrefix(uri, "moxy://")
	parts := strings.SplitN(path, "/", 3)

	if len(parts) == 0 {
		return nil, fmt.Errorf("unknown moxy resource URI %q", uri)
	}

	// moxy://servers — list all servers
	if parts[0] == "servers" {
		return p.readMoxyServersResource(ctx, uri, parts[1:])
	}

	if len(parts) < 2 || parts[0] != "tools" {
		return nil, fmt.Errorf("unknown moxy resource URI %q", uri)
	}

	serverName := parts[1]
	tools, err := p.getToolsForServer(ctx, serverName)
	if err != nil {
		return nil, err
	}

	if len(parts) == 2 {
		// moxy://tools/{server} — return tool list summary
		type toolSummary struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		summaries := make([]toolSummary, len(tools))
		for i, t := range tools {
			summaries[i] = toolSummary{Name: t.Name, Description: t.Description}
		}
		data, err := json.Marshal(summaries)
		if err != nil {
			return nil, fmt.Errorf("marshaling tool list: %w", err)
		}
		return &protocol.ResourceReadResult{
			Contents: []protocol.ResourceContent{
				{URI: uri, MimeType: "application/json", Text: string(data)},
			},
		}, nil
	}

	// moxy://tools/{server}/{tool} — return full tool schema
	toolName := parts[2]
	for _, t := range tools {
		if t.Name == toolName {
			data, err := json.Marshal(t)
			if err != nil {
				return nil, fmt.Errorf("marshaling tool schema: %w", err)
			}
			return &protocol.ResourceReadResult{
				Contents: []protocol.ResourceContent{
					{URI: uri, MimeType: "application/json", Text: string(data)},
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("tool %q not found on server %q", toolName, serverName)
}

func (p *Proxy) readMoxyServersResource(
	ctx context.Context,
	uri string,
	rest []string,
) (*protocol.ResourceReadResult, error) {
	summaries := p.CollectServerSummaries(ctx)

	if len(rest) == 0 {
		// moxy://servers — return all servers
		data, err := json.Marshal(summaries)
		if err != nil {
			return nil, fmt.Errorf("marshaling server summaries: %w", err)
		}
		return &protocol.ResourceReadResult{
			Contents: []protocol.ResourceContent{
				{URI: uri, MimeType: "application/json", Text: string(data)},
			},
		}, nil
	}

	// moxy://servers/{server} — return single server
	serverName := rest[0]
	for _, s := range summaries {
		if s.Name == serverName {
			data, err := json.Marshal(s)
			if err != nil {
				return nil, fmt.Errorf("marshaling server summary: %w", err)
			}
			return &protocol.ResourceReadResult{
				Contents: []protocol.ResourceContent{
					{URI: uri, MimeType: "application/json", Text: string(data)},
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("unknown server %q", serverName)
}

func (p *Proxy) resourceFallbackUnknownServer(
	uri string,
	serverName string,
) (*protocol.ResourceReadResult, error) {
	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()

	var names []string
	for _, c := range children {
		names = append(names, c.Client.Name())
	}
	for name := range p.ephemeral {
		names = append(names, name)
	}

	hint := fmt.Sprintf(
		"Available servers: %s. Use moxy://servers for details.",
		strings.Join(names, ", "),
	)

	msg := struct {
		Error string `json:"error"`
		Hint  string `json:"hint"`
	}{
		Error: fmt.Sprintf("Unknown server %q", serverName),
		Hint:  hint,
	}
	data, _ := json.Marshal(msg)

	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{
			{URI: uri, MimeType: "application/json", Text: string(data)},
		},
	}, nil
}

func (p *Proxy) resourceFallbackNoResources(
	uri string,
	serverName string,
) (*protocol.ResourceReadResult, error) {
	hint := fmt.Sprintf(
		"This server has no resources. Use moxy://tools/%s to list available tools, or moxy://servers for a full server overview.",
		serverName,
	)

	msg := struct {
		Error  string `json:"error"`
		Server string `json:"server"`
		Hint   string `json:"hint"`
	}{
		Error:  "No resources available",
		Server: serverName,
		Hint:   hint,
	}
	data, _ := json.Marshal(msg)

	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{
			{URI: uri, MimeType: "application/json", Text: string(data)},
		},
	}, nil
}

func (p *Proxy) restartServer(ctx context.Context, serverName string) error {
	cfg, ok := p.configs[serverName]
	if !ok {
		return fmt.Errorf("unknown server %q", serverName)
	}

	// Ephemeral servers: re-probe to refresh cached capabilities
	if meta, isEphemeral := p.ephemeral[serverName]; isEphemeral {
		return p.reprobeEphemeral(ctx, meta)
	}

	// Close existing child if running
	p.mu.Lock()
	for i, c := range p.children {
		if c.Client.Name() == serverName {
			c.Client.Close()
			p.children = append(p.children[:i], p.children[i+1:]...)
			break
		}
	}
	// Remove from failed list if present
	for i, f := range p.failed {
		if f.Name == serverName {
			p.failed = append(p.failed[:i], p.failed[i+1:]...)
			break
		}
	}
	p.mu.Unlock()

	// Spawn fresh (outside lock — this is slow)
	exe, cmdArgs := cfg.EffectiveCommand()
	client, result, err := mcpclient.SpawnAndInitialize(
		ctx, cfg.Name, exe, cmdArgs,
	)
	if err != nil {
		p.markFailed(serverName, err)
		return fmt.Errorf("spawning %s: %w", serverName, err)
	}

	client.SetOnNotification(func(msg *jsonrpc.Message) {
		p.ForwardNotification(msg)
	})

	p.mu.Lock()
	p.children = append(p.children, ChildEntry{
		Client:       client,
		Config:       cfg,
		Capabilities: result.Capabilities,
		ServerInfo:   result.ServerInfo,
		Instructions: result.Instructions,
	})
	p.mu.Unlock()

	return nil
}

func (p *Proxy) getPromptEphemeral(
	ctx context.Context,
	serverName string,
	promptName string,
	args map[string]string,
) (*protocol.PromptGetResultV1, error) {
	client, err := p.spawnEphemeral(ctx, serverName)
	if err != nil {
		return nil, fmt.Errorf("spawning ephemeral %s: %w", serverName, err)
	}
	defer client.Close()

	params := protocol.PromptGetParams{
		Name:      promptName,
		Arguments: args,
	}

	raw, err := client.Call(ctx, protocol.MethodPromptsGet, params)
	if err != nil {
		return nil, fmt.Errorf("getting prompt %s from ephemeral %s: %w", promptName, serverName, err)
	}

	result, err := decodePromptGetResult(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding prompt get result from ephemeral %s: %w", serverName, err)
	}

	return result, nil
}

func (p *Proxy) readResourceEphemeral(
	ctx context.Context,
	serverName string,
	uri string,
) (*protocol.ResourceReadResult, error) {
	client, err := p.spawnEphemeral(ctx, serverName)
	if err != nil {
		return nil, fmt.Errorf("spawning ephemeral %s: %w", serverName, err)
	}
	defer client.Close()

	meta := p.ephemeral[serverName]
	if meta.Config.Paginate {
		uri, _ = paginate.ParseParams(uri)
	}

	params := protocol.ResourceReadParams{URI: uri}
	raw, err := client.Call(ctx, protocol.MethodResourcesRead, params)
	if err != nil {
		return nil, fmt.Errorf("reading resource %s from ephemeral %s: %w", uri, serverName, err)
	}

	var result protocol.ResourceReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding resource read result from ephemeral %s: %w", serverName, err)
	}

	return &result, nil
}

func (p *Proxy) callToolEphemeral(
	ctx context.Context,
	serverName string,
	toolName string,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	client, err := p.spawnEphemeral(ctx, serverName)
	if err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("spawning ephemeral %s: %v", serverName, err),
		), nil
	}
	defer client.Close()

	if toolName == "resource-read" {
		return p.callResourceReadOn(ctx, client, serverName, args)
	}

	if toolName == "resource-templates" {
		return p.callResourceTemplatesOn(ctx, client, serverName)
	}

	params := protocol.ToolCallParams{
		Name:      toolName,
		Arguments: args,
	}

	raw, err := client.Call(ctx, protocol.MethodToolsCall, params)
	if err != nil {
		return nil, fmt.Errorf(
			"calling tool %s on ephemeral %s: %w",
			toolName, serverName, err,
		)
	}

	result, err := decodeToolCallResult(raw)
	if err != nil {
		return nil, fmt.Errorf(
			"decoding tool call result from ephemeral %s: %w",
			serverName, err,
		)
	}

	return result, nil
}

func (p *Proxy) callResourceReadOn(
	ctx context.Context,
	client *mcpclient.Client,
	serverName string,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("invalid resource-read args: %v", err),
		), nil
	}

	raw, err := client.Call(
		ctx,
		protocol.MethodResourcesRead,
		protocol.ResourceReadParams{URI: params.URI},
	)
	if err != nil {
		return nil, fmt.Errorf("reading resource %s from %s: %w", params.URI, serverName, err)
	}

	var result protocol.ResourceReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding resource read result from %s: %w", serverName, err)
	}

	return resourceContentsToToolResult(result.Contents)
}

// resourceContentsToToolResult converts MCP resource contents into a tool call
// result. When there is exactly one text content item, the text is returned
// directly as a plain text block (no JSON wrapping). Otherwise, the contents
// array is JSON-marshaled for structured access.
func resourceContentsToToolResult(contents []protocol.ResourceContent) (*protocol.ToolCallResultV1, error) {
	if len(contents) == 1 && contents[0].Text != "" {
		return &protocol.ToolCallResultV1{
			Content: []protocol.ContentBlockV1{
				protocol.TextContentV1(contents[0].Text),
			},
		}, nil
	}

	text, err := json.Marshal(contents)
	if err != nil {
		return nil, fmt.Errorf("marshaling resource contents: %w", err)
	}

	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			{Type: "text", Text: string(text)},
		},
	}, nil
}

func (p *Proxy) callResourceTemplatesOn(
	ctx context.Context,
	client *mcpclient.Client,
	serverName string,
) (*protocol.ToolCallResultV1, error) {
	raw, err := client.Call(ctx, protocol.MethodResourcesTemplates, nil)
	if err != nil {
		return nil, fmt.Errorf("listing resource templates from %s: %w", serverName, err)
	}

	templates, err := decodeResourceTemplatesList(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding resource templates from %s: %w", serverName, err)
	}

	text, err := json.Marshal(templates)
	if err != nil {
		return nil, fmt.Errorf("marshaling resource templates: %w", err)
	}

	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			{Type: "text", Text: string(text)},
		},
	}, nil
}

func (p *Proxy) callResourceRead(
	ctx context.Context,
	child ChildEntry,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("invalid resource-read args: %v", err),
		), nil
	}

	raw, err := child.Client.Call(
		ctx,
		protocol.MethodResourcesRead,
		protocol.ResourceReadParams{URI: params.URI},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"reading resource %s from %s: %w",
			params.URI,
			child.Client.Name(),
			err,
		)
	}

	var result protocol.ResourceReadResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf(
			"decoding resource read result from %s: %w",
			child.Client.Name(),
			err,
		)
	}

	return resourceContentsToToolResult(result.Contents)
}

func (p *Proxy) callResourceTemplates(
	ctx context.Context,
	child ChildEntry,
) (*protocol.ToolCallResultV1, error) {
	raw, err := child.Client.Call(
		ctx,
		protocol.MethodResourcesTemplates,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"listing resource templates from %s: %w",
			child.Client.Name(),
			err,
		)
	}

	templates, err := decodeResourceTemplatesList(raw)
	if err != nil {
		return nil, fmt.Errorf(
			"decoding resource templates from %s: %w",
			child.Client.Name(),
			err,
		)
	}

	text, err := json.Marshal(templates)
	if err != nil {
		return nil, fmt.Errorf("marshaling resource templates: %w", err)
	}

	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			{Type: "text", Text: string(text)},
		},
	}, nil
}

func (p *Proxy) findChild(name string) (ChildEntry, bool) {
	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()
	return findChildIn(children, name)
}

func findChildIn(children []ChildEntry, name string) (ChildEntry, bool) {
	for _, c := range children {
		if c.Client.Name() == name {
			return c, true
		}
	}
	return ChildEntry{}, false
}

func boolPtr(b bool) *bool { return &b }

func readOnlyAnnotations() *protocol.ToolAnnotations {
	return &protocol.ToolAnnotations{
		ReadOnlyHint: boolPtr(true),
	}
}

func prefixToolTitle(tool *protocol.ToolV1, serverName string) {
	if tool.Title != "" {
		tool.Title = serverName + ": " + tool.Title
	}
	if tool.Annotations != nil && tool.Annotations.Title != "" {
		tool.Annotations.Title = serverName + ": " + tool.Annotations.Title
	}
}

func splitPrefix(s, sep string) (prefix, rest string, ok bool) {
	i := strings.Index(s, sep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+len(sep):], true
}

func matchesAnnotationFilter(
	annotations *protocol.ToolAnnotations,
	filter *config.AnnotationFilter,
) bool {
	if filter == nil {
		return true
	}
	if annotations == nil {
		return false
	}

	// OR semantics: a tool matches if ANY configured hint matches.
	if filter.ReadOnlyHint != nil &&
		annotations.ReadOnlyHint != nil &&
		*annotations.ReadOnlyHint == *filter.ReadOnlyHint {
		return true
	}
	if filter.DestructiveHint != nil &&
		annotations.DestructiveHint != nil &&
		*annotations.DestructiveHint == *filter.DestructiveHint {
		return true
	}
	if filter.IdempotentHint != nil &&
		annotations.IdempotentHint != nil &&
		*annotations.IdempotentHint == *filter.IdempotentHint {
		return true
	}
	if filter.OpenWorldHint != nil &&
		annotations.OpenWorldHint != nil &&
		*annotations.OpenWorldHint == *filter.OpenWorldHint {
		return true
	}
	return false
}

func validateToolExists(
	toolName string,
	tools []protocol.ToolV1,
	filter *config.AnnotationFilter,
) error {
	registered := 0
	for _, t := range tools {
		if !matchesAnnotationFilter(t.Annotations, filter) {
			continue
		}
		registered++
		if t.Name == toolName {
			return nil
		}
	}
	return fmt.Errorf(
		"tool %q not found on server (%d tools registered)",
		toolName,
		registered,
	)
}

func paginateResourceResult(
	result protocol.ResourceReadResult,
	params paginate.Params,
) protocol.ResourceReadResult {
	for i, content := range result.Contents {
		if content.Text == "" {
			continue
		}
		sliced, err := paginate.SliceArray(content.Text, params)
		if err != nil {
			// Not a JSON array or pagination not active — pass through
			continue
		}
		wrapped, err := json.Marshal(sliced)
		if err != nil {
			continue
		}
		result.Contents[i].Text = string(wrapped)
	}
	return result
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
func decodeToolCallResult(
	raw json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
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
func decodeResourceTemplatesList(
	raw json.RawMessage,
) ([]protocol.ResourceTemplateV1, error) {
	var v1 protocol.ResourceTemplatesListResultV1
	if err := json.Unmarshal(raw, &v1); err == nil &&
		len(v1.ResourceTemplates) > 0 {
		return v1.ResourceTemplates, nil
	}

	var v0 protocol.ResourceTemplatesListResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		templates := make(
			[]protocol.ResourceTemplateV1,
			len(v0.ResourceTemplates),
		)
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

func downgradeContentBlocks(
	blocks []protocol.ContentBlockV1,
) []protocol.ContentBlock {
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

func upgradeContentBlocks(
	blocks []protocol.ContentBlock,
) []protocol.ContentBlockV1 {
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

// decodePromptsList tries V1 first, falls back to V0 and upgrades.
func decodePromptsList(raw json.RawMessage) ([]protocol.PromptV1, error) {
	var v1 protocol.PromptsListResultV1
	if err := json.Unmarshal(raw, &v1); err == nil && len(v1.Prompts) > 0 {
		return v1.Prompts, nil
	}

	var v0 protocol.PromptsListResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		prompts := make([]protocol.PromptV1, len(v0.Prompts))
		for i, p := range v0.Prompts {
			prompts[i] = protocol.PromptV1{
				Name:        p.Name,
				Description: p.Description,
				Arguments:   p.Arguments,
			}
		}
		return prompts, nil
	}

	return nil, fmt.Errorf("unable to decode prompts list response")
}

// decodePromptGetResult tries V1 first, falls back to V0 and upgrades.
func decodePromptGetResult(
	raw json.RawMessage,
) (*protocol.PromptGetResultV1, error) {
	var v1 protocol.PromptGetResultV1
	if err := json.Unmarshal(raw, &v1); err == nil {
		return &v1, nil
	}

	var v0 protocol.PromptGetResult
	if err := json.Unmarshal(raw, &v0); err == nil {
		messages := make([]protocol.PromptMessageV1, len(v0.Messages))
		for i, m := range v0.Messages {
			messages[i] = protocol.PromptMessageV1{
				Role: m.Role,
				Content: protocol.ContentBlockV1{
					Type:     m.Content.Type,
					Text:     m.Content.Text,
					MimeType: m.Content.MimeType,
					Data:     m.Content.Data,
				},
			}
		}
		return &protocol.PromptGetResultV1{
			Description: v0.Description,
			Messages:    messages,
		}, nil
	}

	return nil, fmt.Errorf("unable to decode prompt get result")
}
