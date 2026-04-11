package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// moxyResourceProvider handles moxy:// discovery resources.
// It holds a back-reference to the Proxy to access children,
// ephemeral metadata, and server summaries.
type moxyResourceProvider struct {
	proxy *Proxy
}

func (m *moxyResourceProvider) ReadResource(
	ctx context.Context,
	uri string,
) (*protocol.ResourceReadResult, error) {
	path := strings.TrimPrefix(uri, "moxy://")
	parts := strings.SplitN(path, "/", 3)

	if len(parts) == 0 {
		return nil, fmt.Errorf("unknown moxy resource URI %q", uri)
	}

	// moxy://servers or moxy://servers/{server}
	if parts[0] == "servers" {
		return m.readServers(ctx, uri, parts[1:])
	}

	if len(parts) < 2 || parts[0] != "tools" {
		return nil, fmt.Errorf("unknown moxy resource URI %q", uri)
	}

	serverName := parts[1]
	tools, err := m.proxy.getToolsForServer(ctx, serverName)
	if err != nil {
		return nil, err
	}

	if len(parts) == 2 {
		// moxy://tools/{server} — tool list summary
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

	// moxy://tools/{server}/{tool} — full tool schema
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

func (m *moxyResourceProvider) readServers(
	ctx context.Context,
	uri string,
	rest []string,
) (*protocol.ResourceReadResult, error) {
	summaries := m.proxy.CollectServerSummaries(ctx)

	if len(rest) == 0 {
		// moxy://servers — all servers
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

	// moxy://servers/{server}
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

func (m *moxyResourceProvider) ListResources(ctx context.Context) []protocol.ResourceV1 {
	p := m.proxy

	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()

	var resources []protocol.ResourceV1

	// moxy://tools/{server} for each server with tools
	for _, child := range children {
		if child.Capabilities.Tools != nil {
			resources = append(resources, protocol.ResourceV1{
				URI:         "moxy://tools/" + child.Client.Name(),
				Name:        child.Client.Name() + " tools",
				Description: fmt.Sprintf("List of tools available on %s", child.Client.Name()),
				MimeType:    "application/json",
			})
		}
	}
	for serverName, meta := range p.ephemeral {
		if len(meta.Tools) > 0 {
			resources = append(resources, protocol.ResourceV1{
				URI:         "moxy://tools/" + serverName,
				Name:        serverName + " tools",
				Description: fmt.Sprintf("List of tools available on %s", serverName),
				MimeType:    "application/json",
			})
		}
	}

	// moxy://servers
	resources = append(resources, protocol.ResourceV1{
		URI:         "moxy://servers",
		Name:        "servers",
		Description: "List of all child servers with capability counts and status",
		MimeType:    "application/json",
	})

	return resources
}

func (m *moxyResourceProvider) ListResourceTemplates(_ context.Context) []protocol.ResourceTemplateV1 {
	return []protocol.ResourceTemplateV1{
		{
			URITemplate: "moxy://servers/{server}",
			Name:        "Server details",
			Description: "Returns capability counts and status for a single child server",
			MimeType:    "application/json",
		},
		{
			URITemplate: "moxy://tools/{server}",
			Name:        "List tools for a server",
			Description: "Returns tool names and descriptions for a child server",
			MimeType:    "application/json",
		},
		{
			URITemplate: "moxy://tools/{server}/{tool}",
			Name:        "Tool schema",
			Description: "Returns the full JSON schema for a specific tool on a child server",
			MimeType:    "application/json",
		},
	}
}

func (m *moxyResourceProvider) fallbackUnknownServer(
	uri string,
	serverName string,
) (*protocol.ResourceReadResult, error) {
	p := m.proxy

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

func (m *moxyResourceProvider) fallbackNoResources(
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
