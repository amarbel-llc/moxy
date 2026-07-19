package proxy

import (
	"context"
	"fmt"
	"strings"

	"code.linenisgreat.com/moxy/internal/native"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// madderBlobProvider serves madder://blobs/{digest} resource reads by
// shelling out to `madder cat <digest>` via the configured backend.
// It also exposes the URI template so MCP clients can discover it.
type madderBlobProvider struct {
	madder native.MadderBackend
}

const blobURIPrefix = "madder://blobs/"

func (p *madderBlobProvider) ReadResource(
	ctx context.Context,
	uri string,
) (*protocol.ResourceReadResult, error) {
	if p.madder == nil {
		return nil, fmt.Errorf("no madder backend configured")
	}
	if !strings.HasPrefix(uri, blobURIPrefix) {
		return nil, fmt.Errorf("uri %q is not a madder blob URI", uri)
	}
	digest := strings.TrimPrefix(uri, blobURIPrefix)
	if idx := strings.Index(digest, "?"); idx >= 0 {
		digest = digest[:idx]
	}
	if digest == "" || strings.Contains(digest, "/") {
		return nil, fmt.Errorf("uri %q has no digest segment", uri)
	}
	body, err := p.madder.CatBytes(ctx, digest)
	if err != nil {
		return nil, err
	}
	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{{
			URI:      uri,
			MimeType: "text/plain",
			Text:     string(body),
		}},
	}, nil
}

func (p *madderBlobProvider) ListResources(_ context.Context) []protocol.ResourceV1 {
	return nil
}

func (p *madderBlobProvider) ListResourceTemplates(_ context.Context) []protocol.ResourceTemplateV1 {
	return []protocol.ResourceTemplateV1{
		{
			URITemplate: "madder://blobs/{digest}",
			Name:        "Cached tool result blob",
			Description: "Read a content-addressable blob from madder by markl-id digest. Tool outputs above the inline-token threshold are stashed here.",
			MimeType:    "text/plain",
		},
	}
}
