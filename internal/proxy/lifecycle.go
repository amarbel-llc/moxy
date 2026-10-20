package proxy

import "context"

// RestartServer restarts one child by name — a persistent child is respawned
// make-before-break, an ephemeral child re-probed, a moxin re-discovered —
// exactly as the `restart <server>` meta tool does. Exported for pkg/moxy's
// Instance.Restart (FDR 0014).
func (p *Proxy) RestartServer(ctx context.Context, serverName string) error {
	return p.restartServer(ctx, serverName)
}

// CloseChildren closes every child the proxy currently holds. It reads the
// live set under the lock, so children installed by a Reload or a restart are
// the ones closed — not the set a caller captured at startup.
func (p *Proxy) CloseChildren() {
	p.mu.RLock()
	children := p.children
	p.mu.RUnlock()
	for _, c := range children {
		_ = c.Client.Close()
	}
}
