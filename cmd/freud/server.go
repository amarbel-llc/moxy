package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
)

type freudServer struct {
	projectsDir string
	listCfg     ListConfig
}

// Resource templates (V0).
var templates = []protocol.ResourceTemplate{
	{
		URITemplate: "freud://sessions",
		Name:        "List all Claude Code sessions",
		Description: "List every Claude Code session transcript across all projects, sorted by most-recent activity first. Columnar text output with session id, last activity, message count, size, and resolved project path. Use ?offset=N&limit=M to paginate. Phase 1a: only format=columnar is accepted.",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "freud://sessions/{project}",
		Name:        "List sessions for one project",
		Description: "List Claude Code sessions for a single project. The {project} segment accepts either the raw project directory name (as it appears under ~/.claude/projects) or a URL-encoded absolute path, which is matched against each project's resolved cwd. Same format and pagination params as freud://sessions.",
		MimeType:    "text/plain",
	},
}

var templatesV1 = []protocol.ResourceTemplateV1{
	{
		URITemplate: "freud://sessions",
		Name:        "List all Claude Code sessions",
		Description: "List every Claude Code session transcript across all projects, sorted by most-recent activity first. Columnar text output with session id, last activity, message count, size, and resolved project path. Use ?offset=N&limit=M to paginate. Phase 1a: only format=columnar is accepted.",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "freud://sessions/{project}",
		Name:        "List sessions for one project",
		Description: "List Claude Code sessions for a single project. The {project} segment accepts either the raw project directory name (as it appears under ~/.claude/projects) or a URL-encoded absolute path, which is matched against each project's resolved cwd. Same format and pagination params as freud://sessions.",
		MimeType:    "text/plain",
	},
}

// ResourceProvider (base interface).

func (s *freudServer) ListResources(_ context.Context) ([]protocol.Resource, error) {
	return nil, nil
}

func (s *freudServer) ReadResource(_ context.Context, uri string) (*protocol.ResourceReadResult, error) {
	return nil, unknownURIError(uri)
}

func (s *freudServer) ListResourceTemplates(_ context.Context) ([]protocol.ResourceTemplate, error) {
	return templates, nil
}

// ResourceProviderV1.

func (s *freudServer) ListResourcesV1(_ context.Context, _ string) (*protocol.ResourcesListResultV1, error) {
	return &protocol.ResourcesListResultV1{}, nil
}

func (s *freudServer) ListResourceTemplatesV1(_ context.Context, _ string) (*protocol.ResourceTemplatesListResultV1, error) {
	return &protocol.ResourceTemplatesListResultV1{ResourceTemplates: templatesV1}, nil
}

// unknownURIError returns a structured hint pointing agents back to the
// discovery entry points. Same spirit as moxy's unknown-resource hints.
func unknownURIError(uri string) error {
	if !strings.HasPrefix(uri, "freud://") {
		return fmt.Errorf("not a freud URI: %s (try freud://sessions)", uri)
	}
	return fmt.Errorf("unknown freud resource: %s (try freud://sessions or freud://sessions/{project})", uri)
}

// Interface assertions.
var _ server.ResourceProviderV1 = (*freudServer)(nil)
