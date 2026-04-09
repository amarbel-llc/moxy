package proxy

import (
	"context"
	"encoding/json"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
)

// ServerBackend abstracts the proxy's interaction with child servers.
// Both proxied MCP servers (mcpclient.Client) and config-declared virtual
// servers (native.Server) implement this interface.
type ServerBackend interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	Notify(method string, params any) error
	SetOnNotification(fn func(*jsonrpc.Message))
	Name() string
	Close() error
}
