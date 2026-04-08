package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// handleTranscriptRead serves freud://transcript/{session_id}, returning the
// raw JSONL file as a single text content block. Phase 1b: no rendering, no
// filtering, no pagination — see docs/features/0003-freud.md for the future
// work that's been deferred.
func (s *freudServer) handleTranscriptRead(uri string) (*protocol.ResourceReadResult, error) {
	id, err := parseTranscriptURI(uri)
	if err != nil {
		return nil, err
	}

	path, ok, err := findSessionByID(s.projectsDir, s.cache, id)
	if err != nil {
		return nil, fmt.Errorf("locating session: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("unknown session id: %s (use freud://sessions to discover available ids)", id)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading transcript: %w", err)
	}

	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{
			{URI: uri, MimeType: "application/x-ndjson", Text: string(data)},
		},
	}, nil
}

// parseTranscriptURI extracts the session id from a
// freud://transcript/{session_id} URI. The id segment is URL-decoded so a
// caller can encode unusual characters, even though session ids in the wild
// are always plain UUIDs.
func parseTranscriptURI(uri string) (string, error) {
	const prefix = "freud://transcript/"
	if !strings.HasPrefix(uri, prefix) {
		return "", fmt.Errorf("not a freud transcript URI: %s", uri)
	}
	rest := strings.TrimPrefix(uri, prefix)
	// Strip any query string — Phase 1b accepts no params, but parse them
	// off so we don't accidentally accept "abc?garbage" as a literal id.
	if idx := strings.Index(rest, "?"); idx >= 0 {
		rest = rest[:idx]
	}
	if rest == "" {
		return "", fmt.Errorf("empty session id in URI: %s", uri)
	}
	id, err := url.PathUnescape(rest)
	if err != nil {
		return "", fmt.Errorf("decoding session id: %w", err)
	}
	if id == "" {
		return "", fmt.Errorf("empty session id after decode: %s", uri)
	}
	return id, nil
}

// findSessionByID walks all project directories looking for a JSONL file
// whose stem matches id. Returns the absolute filepath and ok=true on the
// first match, ok=false if no project contains it. Walks the project cache
// to avoid re-stating directories that haven't changed since the last scan.
func findSessionByID(projectsDir string, cache *projectCache, id string) (string, bool, error) {
	projects, err := cache.scanProjects(projectsDir)
	if err != nil {
		return "", false, err
	}

	target := id + ".jsonl"
	for _, p := range projects {
		candidate := filepath.Join(projectsDir, p.dirName, target)
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, true, nil
		}
	}
	return "", false, nil
}
