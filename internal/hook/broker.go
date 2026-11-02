package hook

import (
	"context"
	"os"
	"sync"
	"time"

	"code.linenisgreat.com/moxy/internal/statsd"
	"code.linenisgreat.com/moxy/pkg/perms"
	"code.linenisgreat.com/moxy/pkg/policybroker"
)

// brokerDialWait bounds how long the hook waits for the broker's hello before
// the check is no answer and the static floor decides. The hook is a
// short-lived process (a fresh one per PreToolUse event), so it cannot afford
// the standing client's second-long hello budget.
const brokerDialWait = 250 * time.Millisecond

// brokerMetricPath is the hook's segment in moxy.policy_broker.<path>.* (the
// server's standing client reports as "server"), moxy#493.
const brokerMetricPath = "hook"

// hookGate is the hook's policy-broker client (RFC 0002 section 9): same wire
// as the server's standing client, but dialled per process and only when a
// call names a resource, because Claude Code decides the prompt here before
// the call reaches the server. It fails closed the same way: no answer means
// the floor.
type hookGate struct {
	once   sync.Once
	client *policybroker.Client
}

var _ perms.Gate = (*hookGate)(nil)

func (g *hookGate) Check(ctx context.Context, op perms.Op, res perms.Resource) (perms.Decision, string, bool) {
	g.once.Do(func() {
		// stderr is nil: the hook's stdout is the Claude Code protocol and
		// its stderr is shown to the agent.
		c := policybroker.FromEnv(os.Getenv, nil, "moxy-hook")
		if c == nil {
			return
		}
		// New dials in the background, so the dial cost is the wait for the
		// hello: the whole brokerDialWait when nothing answers.
		start := time.Now()
		wait, cancel := context.WithTimeout(ctx, brokerDialWait)
		defer cancel()
		connected := c.WaitConnected(wait)
		statsd.EmitBrokerDial(brokerMetricPath, time.Since(start), connected)
		g.client = c
	})
	if g.client == nil {
		return perms.Unknown, "no policy broker configured", false
	}
	return statsd.TimedGate(brokerMetricPath, g.client).Check(ctx, op, res)
}

func (g *hookGate) close() {
	if g.client != nil {
		_ = g.client.Close()
	}
}

// brokerGate returns the hook's gate when CLOWN_POLICY_BROKER is set, nil
// otherwise (the resolver then behaves exactly as without the protocol).
func brokerGate() *hookGate {
	if os.Getenv(policybroker.EnvAddr) == "" {
		return nil
	}
	return &hookGate{}
}
