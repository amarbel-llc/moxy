package statsd

import (
	"context"
	"strings"
	"testing"

	"code.linenisgreat.com/moxy/pkg/perms"
)

type scriptedGate struct {
	dec      perms.Decision
	answered bool
}

func (g scriptedGate) Check(context.Context, perms.Op, perms.Resource) (perms.Decision, string, bool) {
	return g.dec, "why", g.answered
}

func TestTimedGateReportsAndPassesTheVerdictThrough(t *testing.T) {
	for _, tc := range []struct {
		gate scriptedGate
		want string
	}{
		{scriptedGate{perms.Allow, true}, ".check.allow:1|c"},
		{scriptedGate{perms.Deny, true}, ".check.deny:1|c"},
		{scriptedGate{perms.Ask, true}, ".check.ask:1|c"},
		{scriptedGate{perms.Unknown, false}, ".check.floor:1|c"},
		// an unanswered check is the floor even if a decision rode along
		{scriptedGate{perms.Allow, false}, ".check.floor:1|c"},
	} {
		pc := listenUDP(t)
		ReinitFromEnv()
		g := TimedGate("server", tc.gate)
		dec, reason, answered := g.Check(context.Background(), perms.Read, perms.Resource{Kind: "file", URI: "/x"})
		if dec != tc.gate.dec || reason != "why" || answered != tc.gate.answered {
			t.Errorf("verdict changed: got (%v,%q,%v) from %+v", dec, reason, answered, tc.gate)
		}
		got := readPacket(t, pc)
		if !strings.HasPrefix(got, "moxy.policy_broker.server.check.duration:") || !strings.HasSuffix(got, tc.want) {
			t.Errorf("packet %q, want suffix %q", got, tc.want)
		}
	}
}
