package proxy

import (
	"context"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// ResourceProvider handles resource reads for a URI scheme prefix.
// Synthetic providers are registered on the Proxy and dispatched
// before child-server routing in ReadResource.
type ResourceProvider interface {
	ReadResource(ctx context.Context, uri string) (*protocol.ResourceReadResult, error)
	ListResources(ctx context.Context) []protocol.ResourceV1
	ListResourceTemplates(ctx context.Context) []protocol.ResourceTemplateV1
}
