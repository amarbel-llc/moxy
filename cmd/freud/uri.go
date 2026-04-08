package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// sessionsRequest is the parsed form of a freud://sessions[/{project}] URI.
type sessionsRequest struct {
	// project is the raw {project} segment from the URI, post URL-decode.
	// Empty for freud://sessions (the all-projects list).
	project string

	// offset and limit implement caller-driven pagination. When both are
	// zero, the handler may instead return a head+tail summary if the
	// total exceeds the configured threshold.
	offset int
	limit  int

	// format selects the output format. Phase 1a accepts only "columnar"
	// (and the empty string, which is treated as the default).
	format string
}

// paginationRequested reports whether the caller passed offset or limit
// query params; in that mode the handler skips progressive disclosure.
func (r sessionsRequest) paginationRequested() bool {
	return r.offset > 0 || r.limit > 0
}

// parseSessionsURI parses a freud://sessions[/{project}][?query] URI into a
// sessionsRequest. Unknown ?format= values are rejected.
func parseSessionsURI(uri string) (sessionsRequest, error) {
	var req sessionsRequest

	const prefix = "freud://sessions"
	if !strings.HasPrefix(uri, prefix) {
		return req, fmt.Errorf("not a freud sessions URI: %s", uri)
	}
	rest := strings.TrimPrefix(uri, prefix)

	pathPart := rest
	queryPart := ""
	if idx := strings.Index(rest, "?"); idx >= 0 {
		pathPart = rest[:idx]
		queryPart = rest[idx+1:]
	}

	// Path is either empty (the all-sessions list) or "/<project>" where
	// project is URL-encoded.
	switch {
	case pathPart == "" || pathPart == "/":
		// All sessions.
	case strings.HasPrefix(pathPart, "/"):
		decoded, err := url.PathUnescape(pathPart[1:])
		if err != nil {
			return req, fmt.Errorf("decoding project segment: %w", err)
		}
		if decoded == "" {
			return req, fmt.Errorf("empty project segment in URI: %s", uri)
		}
		req.project = decoded
	default:
		return req, fmt.Errorf("invalid sessions URI shape: %s", uri)
	}

	if queryPart == "" {
		return req, nil
	}

	values, err := url.ParseQuery(queryPart)
	if err != nil {
		return req, fmt.Errorf("invalid query params: %w", err)
	}

	if v := values.Get("offset"); v != "" {
		n, parseErr := strconv.Atoi(v)
		if parseErr != nil || n < 0 {
			return req, fmt.Errorf("invalid offset: %s", v)
		}
		req.offset = n
	}
	if v := values.Get("limit"); v != "" {
		n, parseErr := strconv.Atoi(v)
		if parseErr != nil || n < 0 {
			return req, fmt.Errorf("invalid limit: %s", v)
		}
		req.limit = n
	}
	if v := values.Get("format"); v != "" {
		if v != "columnar" {
			return req, fmt.Errorf("format=%s reserved for future use; only format=columnar is supported in Phase 1a", v)
		}
		req.format = v
	}

	return req, nil
}
