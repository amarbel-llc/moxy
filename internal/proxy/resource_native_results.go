package proxy

import (
	"context"
	"fmt"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// nativeResultProvider handles moxy.native://results/ resource reads,
// backed by the native server result cache.
type nativeResultProvider struct {
	reader ResultReader
}

func (n *nativeResultProvider) ReadResource(
	_ context.Context,
	uri string,
) (*protocol.ResourceReadResult, error) {
	if n.reader == nil {
		return nil, fmt.Errorf("no result reader configured")
	}
	output, err := n.reader.ReadResult(uri)
	if err != nil {
		return nil, err
	}
	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{{
			URI:      uri,
			MimeType: "text/plain",
			Text:     output,
		}},
	}, nil
}

func (n *nativeResultProvider) ListResources(_ context.Context) []protocol.ResourceV1 {
	return nil
}

func (n *nativeResultProvider) ListResourceTemplates(_ context.Context) []protocol.ResourceTemplateV1 {
	return []protocol.ResourceTemplateV1{
		{
			URITemplate: "moxy.native://results/{session}/{id}",
			Name:        "Cached tool result",
			Description: "Read cached output from a native server tool call",
			MimeType:    "text/plain",
		},
	}
}
