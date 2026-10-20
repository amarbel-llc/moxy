package moxy

import (
	"context"
	"errors"
	"io"
	"time"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/server"

	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/internal/proxy"
	"code.linenisgreat.com/moxy/pkg/moxyfile"
	"code.linenisgreat.com/moxy/pkg/perms"
)

// BlobStore is the blob backend behind madder://blobs/<digest> references:
// where oversized or cache-results=always outputs go, and where an input
// reference is read from when a tool argument names one. The default
// (Options.Blobs nil) is the in-process madder client over the user's
// default store, exactly what the binary uses.
type BlobStore = native.MadderBackend

// ResultStore is the optional capability a BlobStore may have of writing to a
// named store; the async meta tools persist terminal results through it. The
// default madder client has it.
type ResultStore interface {
	WriteToStore(ctx context.Context, storeID string, content io.Reader) (string, error)
}

// ConnectFunc spawns or connects one [[servers]] entry and completes its
// MCP initialize handshake. Options.Connect overrides the default (stdio
// spawn for a command, streamable HTTP with the configured credentials for a
// url); the proxy also calls it to restart or re-probe a child.
type ConnectFunc = proxy.ConnectFunc

// ServerSummary describes one child server as Servers reports it: running
// (with capability counts) or failed (with the error).
type ServerSummary = proxy.ServerSummary

// DefaultAsyncResultStore is the user-level madder store the async meta
// tools write terminal results to when Options.AsyncResultStore is empty
// (see docs/features/0004-async-tool-dispatch.md). moxy only writes to it;
// provisioning it is the host's job.
const DefaultAsyncResultStore = "moxy-async"

// DefaultCloseTimeout bounds Close's child teardown when
// Options.CloseTimeout is zero: clown grants about five seconds between
// SIGTERM and SIGKILL, and the binary budgets one of them for children.
const DefaultCloseTimeout = time.Second

// ErrClosed is returned by every Instance method after Close.
var ErrClosed = errors.New("moxy: instance is closed")

// Options describes the moxy to open. Exactly one of Dir, File and Config
// names the effective moxyfile; the zero Options walks the hierarchy for the
// working directory, which is what `moxy serve mcp` does.
type Options struct {
	// Dir is walked the way the binary walks the working directory: the
	// global moxyfile, every directory from $HOME down to Dir, includes.
	// Empty with File and Config unset means the working directory.
	Dir string
	// File is one moxyfile plus its includes and nothing else — how a
	// reference written by `moxy show-effective-moxyfile` is read.
	File string
	// Config is an already-loaded moxyfile (an inline brief). It is taken in
	// its literal form: header $VAR references are expanded at serve time,
	// the way the loaders leave them.
	Config *moxyfile.Config

	// Parent, when set, is the reference the effective moxyfile must narrow
	// (FDR 0013): Open refuses to start anything that widens it, and every
	// Reload is judged against the same pinned reference plus the
	// allow-tools baseline the instance started with (moxy#461). Load one
	// from a file with LoadParentReference. ParentLabel names it in
	// refusals; empty means the file's path when it came from one.
	Parent      *moxyfile.Config
	ParentLabel string

	// MoxinPath is the moxin search path (moxin(7) DISCOVERY); empty means
	// the default hierarchy under Dir. SystemMoxinDir overrides the moxins
	// bundled with the build; empty means the build's own, unless the
	// moxyfile turns them off with builtin-native = false.
	MoxinPath      string
	SystemMoxinDir string

	// SessionID scopes caches, spool names and journal entries. Empty means
	// "no-session"; the binary passes its CLAUDE_SESSION_ID /
	// SPINCLASS_SESSION_ID / generated chain.
	SessionID string

	// Permissions is the serve-time permission policy (moxy#486). The zero
	// value is Delegate, today's behaviour: the funnel resolves nothing.
	Permissions perms.Policy

	// NameTemplate renders advertised tool and prompt names; empty is
	// "{server}.{tool}". Expose is the tool-exposure selector (FDR 0006);
	// empty exposes everything.
	NameTemplate string
	Expose       string

	// MetaTools registers moxy's own tools (restart, batch, async,
	// async-result, async-cancel). RegisterTools, when set, registers
	// further builtins first — the binary adds go-mcp's `status` tool.
	MetaTools     bool
	RegisterTools func(*server.ToolRegistryV1)
	// AsyncResultStore names the store async results are written to;
	// empty is DefaultAsyncResultStore.
	AsyncResultStore string

	// Blobs is the blob backend; nil opens the default madder client and
	// verifies the default store exists, failing Open when it does not.
	Blobs BlobStore
	// Connect spawns child servers; nil uses the default.
	Connect ConnectFunc
	// Notify receives tools/list_changed and the notifications children
	// forward; a transport usually supplies it later via SetNotifier.
	Notify func(*jsonrpc.Message) error
	// Stderr receives the startup chatter the binary prints (which child
	// connected, which moxin was skipped and why). Nil discards it.
	Stderr io.Writer
	// CloseTimeout bounds child teardown in Close; zero is
	// DefaultCloseTimeout.
	CloseTimeout time.Duration
}

func (o Options) stderr() io.Writer {
	if o.Stderr == nil {
		return io.Discard
	}
	return o.Stderr
}

func (o Options) asyncResultStore() string {
	if o.AsyncResultStore == "" {
		return DefaultAsyncResultStore
	}
	return o.AsyncResultStore
}

func (o Options) closeTimeout() time.Duration {
	if o.CloseTimeout <= 0 {
		return DefaultCloseTimeout
	}
	return o.CloseTimeout
}
