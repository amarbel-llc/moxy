package proxy

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/pkg/moxyfile"
)

// manifestTimeout bounds a manifest-command run. The command exists to answer
// without starting the server, so one that takes long is broken.
const manifestTimeout = 30 * time.Second

// ephemeralMetaFor returns the metadata for a server moxy does not keep running
// from startup: a lazy one (manifest-command, moxy#525) or an ephemeral one.
// nil means a persistent child. A manifest-command wins over a global
// ephemeral default; a per-server ephemeral = true alongside it is refused at
// parse time.
func ephemeralMetaFor(cfg moxyfile.ServerConfig, globalEphemeral *bool) *EphemeralMeta {
	switch {
	case cfg.IsLazy():
		return &EphemeralMeta{Config: cfg, Lazy: true}
	case cfg.IsEphemeral(globalEphemeral):
		return &EphemeralMeta{Config: cfg}
	}
	return nil
}

// runManifest runs a lazy server's manifest-command and decodes its output as
// a tools/list result. The server itself is not started.
func runManifest(ctx context.Context, cfg moxyfile.ServerConfig) ([]protocol.ToolV1, error) {
	parts := cfg.ManifestCommand
	if len(parts) == 0 {
		return nil, fmt.Errorf("server %q has no manifest-command", cfg.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, manifestTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("manifest-command %q: %w: %s",
			strings.Join(parts, " "), err, strings.TrimSpace(stderr.String()))
	}
	tools, err := decodeToolsList(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("manifest-command %q: output is not a tools/list result: %w",
			strings.Join(parts, " "), err)
	}
	return tools, nil
}

// loadManifest fills a lazy server's tool list from its manifest-command. A
// failing command is a load error: the server is marked failed (so the client
// sees <name>.status saying why) and is never spawned.
func (p *Proxy) loadManifest(ctx context.Context, meta *EphemeralMeta) error {
	tools, err := runManifest(ctx, meta.Config)
	if err != nil {
		return err
	}
	meta.Tools = tools
	meta.Capabilities = protocol.ServerCapabilitiesV1{Tools: &protocol.ToolsCapability{}}
	meta.ServerInfo = protocol.ImplementationV1{Name: meta.Config.Name}
	return nil
}

// lazyBackend returns the running client of a lazy server, spawning it on the
// first call. It then stays running (moxy#525), until a restart or shutdown.
func (p *Proxy) lazyBackend(ctx context.Context, meta *EphemeralMeta) (ServerBackend, error) {
	meta.lazyMu.Lock()
	defer meta.lazyMu.Unlock()
	if meta.lazyClient != nil {
		return meta.lazyClient, nil
	}
	// The child must outlive the request that triggered it: on the HTTP
	// transport ctx is the per-request context (#408).
	client, err := p.spawnEphemeral(context.WithoutCancel(ctx), meta.Config.Name)
	if err != nil {
		return nil, err
	}
	meta.lazyClient = client
	return client, nil
}

// closeLazy stops a lazy server's running client, if it has been started.
func (meta *EphemeralMeta) closeLazy() {
	meta.lazyMu.Lock()
	client := meta.lazyClient
	meta.lazyClient = nil
	meta.lazyMu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}
