package proxy

import (
	"context"
	"encoding/json"
	"fmt"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/pkg/moxyfile"
)

// maxToolListPages bounds the walk over a child's paginated tools/list, so a
// child that never stops returning a cursor cannot wedge a call.
const maxToolListPages = 50

// annotationRefusal is the result for a call to a tool that a server's
// [servers.annotations] filter hides from tools/list (moxy#450). A filtered
// tool is neither listed nor callable, like one excluded by --expose,
// /clown/exclude-tools or allow-tools.
func annotationRefusal(serverName, toolName string) *protocol.ToolCallResultV1 {
	return protocol.ErrorResultV1(fmt.Sprintf(
		"tool %q of server %q is hidden by its [servers.annotations] filter",
		toolName, serverName,
	))
}

// toolHiddenByFilter reports whether the named tool is one the filter drops
// from tools/list. A tool the child does not list is not hidden by the filter
// (the child reports it as unknown). An error means the child's tool list
// could not be read, which the caller treats as hidden: a filter that cannot
// be checked does not let a call through.
func toolHiddenByFilter(
	ctx context.Context,
	client ServerBackend,
	toolName string,
	filter *moxyfile.AnnotationFilter,
) (bool, error) {
	cursor := ""
	for page := 0; page < maxToolListPages; page++ {
		raw, err := client.Call(ctx, protocol.MethodToolsList, cursorParams(cursor))
		if err != nil {
			return true, fmt.Errorf("listing tools: %w", err)
		}
		tools, err := decodeToolsList(raw)
		if err != nil {
			return true, fmt.Errorf("decoding tools: %w", err)
		}
		for _, tool := range tools {
			if tool.Name == toolName {
				return !matchesAnnotationFilter(tool.Annotations, filter), nil
			}
		}
		var next struct {
			NextCursor string `json:"nextCursor"`
		}
		_ = json.Unmarshal(raw, &next)
		if next.NextCursor == "" {
			return false, nil
		}
		cursor = next.NextCursor
	}
	return true, fmt.Errorf("listing tools: more than %d pages", maxToolListPages)
}

// ephemeralToolHiddenByFilter is the same check against the tool list probed
// at startup for an ephemeral server.
func ephemeralToolHiddenByFilter(meta *EphemeralMeta, toolName string) bool {
	for _, tool := range meta.Tools {
		if tool.Name == toolName {
			return !matchesAnnotationFilter(tool.Annotations, meta.Config.Annotations)
		}
	}
	return false
}
