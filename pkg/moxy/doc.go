// Package moxy is the embedding facade for moxy: constructing a proxy from a
// moxyfile, listing its tools, calling one with a caller identity and a
// permission policy, reading the blob references a result carries, and
// supervising the child servers behind it — in process, with no MCP
// transport and no Claude Code hook in front.
//
// The CLI entry points (`moxy serve mcp`, `moxy serve-http`) are built on
// this package, so there is one implementation of the call path for the
// binary and for an embedder such as clown's headless agent loop. See
// docs/features/0014-embeddable-go-library.md for the design, the lifecycle
// contract and the migration plan; the surface fills in step by step and
// this doc comment is normative for whatever is exported here.
//
// Stability: pre-1.0. A minor bump of MOXY_VERSION may change this package's
// exported names; the changelog records what moved.
package moxy

// version is the library's build identity: MOXY_VERSION from version.env,
// injected by the nix build (`-X code.linenisgreat.com/moxy/pkg/moxy.version`).
// A build that does not set it reports "dev". It is the same value the
// binary reports, since the binary and the library are one module at one
// version.
var version = "dev"

// Version reports the moxy version this library was built as, or "dev" when
// the build injected none.
func Version() string { return version }
