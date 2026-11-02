package statsd

import (
	"context"
	"io"
	"time"

	"code.linenisgreat.com/moxy/pkg/perms"
)

// timedGate reports each check of the wrapped gate through EmitBrokerCheck.
type timedGate struct {
	path string
	next perms.Gate
}

// TimedGate wraps a policy-broker gate so every check is timed and its outcome
// counted under path (moxy#493). The verdict is returned untouched.
func TimedGate(path string, g perms.Gate) perms.Gate {
	return timedGate{path: path, next: g}
}

func (t timedGate) Check(ctx context.Context, op perms.Op, res perms.Resource) (perms.Decision, string, bool) {
	start := time.Now()
	dec, reason, answered := t.next.Check(ctx, op, res)
	EmitBrokerCheck(t.path, time.Since(start), BrokerOutcome(dec, answered))
	return dec, reason, answered
}

// Close closes the wrapped gate when it can be closed, so wrapping a standing
// client does not hide its lifecycle from the owner.
func (t timedGate) Close() error {
	if c, ok := t.next.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// BrokerOutcome names a check's result for the counter: no answer is the
// floor, whatever the decision field holds.
func BrokerOutcome(dec perms.Decision, answered bool) string {
	if !answered {
		return BrokerFloor
	}
	switch dec {
	case perms.Allow:
		return BrokerAllow
	case perms.Deny:
		return BrokerDeny
	case perms.Ask:
		return BrokerAsk
	}
	return BrokerFloor
}
