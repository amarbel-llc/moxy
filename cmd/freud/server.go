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
	cache       *projectCache
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
	{
		URITemplate: "freud://transcript/{session_id}",
		Name:        "Read a session transcript",
		Description: "Return the raw JSONL transcript for a single past Claude Code session, looked up by session id alone (UUIDs are unique across all projects). Phase 1b: returns the unfiltered file contents — no rendering, no pagination, no filters. Discover ids via freud://sessions.",
		MimeType:    "application/x-ndjson",
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
	{
		URITemplate: "freud://transcript/{session_id}",
		Name:        "Read a session transcript",
		Description: "Return the raw JSONL transcript for a single past Claude Code session, looked up by session id alone (UUIDs are unique across all projects). Phase 1b: returns the unfiltered file contents — no rendering, no pagination, no filters. Discover ids via freud://sessions.",
		MimeType:    "application/x-ndjson",
	},
}

// ResourceProvider (base interface).

func (s *freudServer) ListResources(_ context.Context) ([]protocol.Resource, error) {
	return nil, nil
}

func (s *freudServer) ReadResource(_ context.Context, uri string) (*protocol.ResourceReadResult, error) {
	switch {
	case strings.HasPrefix(uri, "freud://transcript/"):
		return s.handleTranscriptRead(uri)
	case strings.HasPrefix(uri, "freud://sessions"):
		return s.handleSessionsList(uri)
	default:
		return nil, unknownURIError(uri)
	}
}

func (s *freudServer) handleSessionsList(uri string) (*protocol.ResourceReadResult, error) {
	req, err := parseSessionsURI(uri)
	if err != nil {
		return nil, err
	}

	var (
		rows     []sessionRow
		projects []projectInfo
	)
	if req.project == "" {
		rows, err = scanAllSessions(s.projectsDir, s.cache)
		if err != nil {
			return nil, fmt.Errorf("scanning sessions: %w", err)
		}
	} else {
		rows, projects, err = scanProjectSessions(s.projectsDir, s.cache, req.project)
		if err != nil {
			return nil, fmt.Errorf("scanning project sessions: %w", err)
		}
		if rows == nil {
			return nil, unknownProjectError(req.project, projects)
		}
	}

	text := s.renderSessions(rows, req, uri)
	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{
			{URI: uri, MimeType: "text/plain", Text: text},
		},
	}, nil
}

// renderSessions chooses between paginated, progressive-disclosure, and
// full-list rendering based on the request and the configured thresholds.
// The continuation URI for progressive disclosure strips any existing query
// string and points back at the same resource path.
func (s *freudServer) renderSessions(rows []sessionRow, req sessionsRequest, uri string) string {
	if req.paginationRequested() {
		return formatSessions(pageRows(rows, req.offset, req.limit))
	}
	if len(rows) > s.listCfg.MaxRows {
		head := rows[:s.listCfg.HeadRows]
		tailStart := len(rows) - s.listCfg.TailRows
		if tailStart < s.listCfg.HeadRows {
			tailStart = s.listCfg.HeadRows
		}
		tail := rows[tailStart:]
		return formatSessionsTruncated(head, tail, len(rows), uriWithoutQuery(uri))
	}
	return formatSessions(rows)
}

// uriWithoutQuery strips any "?..." suffix from a freud URI so the
// continuation hint can append fresh query params.
func uriWithoutQuery(uri string) string {
	if idx := strings.Index(uri, "?"); idx >= 0 {
		return uri[:idx]
	}
	return uri
}

// unknownProjectError returns a structured hint listing up to 10 known
// project directory names so the agent can re-issue with a valid one.
func unknownProjectError(requested string, projects []projectInfo) error {
	if len(projects) == 0 {
		return fmt.Errorf("unknown project %q: no projects found under ~/.claude/projects", requested)
	}

	const maxNames = 10
	var names []string
	for i, p := range projects {
		if i >= maxNames {
			break
		}
		names = append(names, p.dirName)
	}
	suffix := ""
	if len(projects) > maxNames {
		suffix = fmt.Sprintf(" (and %d more)", len(projects)-maxNames)
	}
	return fmt.Errorf(
		"unknown project %q: known projects are %s%s",
		requested,
		strings.Join(names, ", "),
		suffix,
	)
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
