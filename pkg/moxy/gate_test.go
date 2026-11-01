package moxy

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"code.linenisgreat.com/moxy/pkg/perms"
)

// scriptedGate answers from a fixed table by resource URI; a URI absent from
// it is "no answer". It records what it was asked.
type scriptedGate struct {
	mu       sync.Mutex
	verdicts map[string]perms.Decision
	asked    []string
}

func (g *scriptedGate) Check(_ context.Context, op perms.Op, res perms.Resource) (perms.Decision, string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked = append(g.asked, string(op)+" "+res.URI)
	d, ok := g.verdicts[res.URI]
	return d, "scripted", ok
}

func callDynfile(t *testing.T, inst *Instance) (*Result, string) {
	t.Helper()
	res, err := inst.Call(context.Background(), Call{
		Name: "fix.dynfile",
		Args: json.RawMessage(`{"path":"/somewhere/else"}`),
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	var text strings.Builder
	for _, b := range res.Content {
		text.WriteString(b.Text)
	}
	return res, text.String()
}

// moxy#485: a plain tools/call under Delegate (no hook in front, or a hook
// that cannot see the broker) still honours a broker deny, and only that: ask
// stays the client's prompt, and no answer is today's behaviour.
func TestDelegateFunnelRefusesABrokerDenyOnly(t *testing.T) {
	cases := []struct {
		name    string
		verdict map[string]perms.Decision
		refused bool
	}{
		{"deny", map[string]perms.Decision{"file:///somewhere/else": perms.Deny}, true},
		{"ask", map[string]perms.Decision{"file:///somewhere/else": perms.Ask}, false},
		{"allow", map[string]perms.Decision{"file:///somewhere/else": perms.Allow}, false},
		{"no answer", map[string]perms.Decision{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gate := &scriptedGate{verdicts: tc.verdict}
			inst := openFixture(t, func(o *Options) { o.Gate = gate })
			res, text := callDynfile(t, inst)
			if res.IsError != tc.refused {
				t.Fatalf("isError = %v, want %v (%s)", res.IsError, tc.refused, text)
			}
			if tc.refused && (res.Decision != perms.Deny || !strings.Contains(text, "moxy denied")) {
				t.Errorf("decision = %q, text = %q", res.Decision, text)
			}
		})
	}
}

func TestStandaloneDelegateConsultsNothing(t *testing.T) {
	inst := openFixture(t, nil)
	res, text := callDynfile(t, inst)
	if res.IsError || text != "ran dynfile" {
		t.Fatalf("isError = %v, text = %q; no gate must be today's behaviour", res.IsError, text)
	}
}

// Under Enforce the broker's verdict replaces the floor when it answers, and
// the floor (here: ask, which nobody can answer) stands when it does not.
func TestEnforceTakesTheBrokerVerdictAndFallsToTheFloorOnNoAnswer(t *testing.T) {
	enforce := func(g perms.Gate) *Instance {
		return openFixture(t, func(o *Options) {
			o.Permissions = perms.Policy{Mode: perms.Enforce}
			o.Gate = g
		})
	}
	uri := "file:///somewhere/else"

	res, text := callDynfile(t, enforce(&scriptedGate{verdicts: map[string]perms.Decision{uri: perms.Allow}}))
	if res.IsError || text != "ran dynfile" || res.Decision != perms.Allow {
		t.Errorf("broker allow: isError=%v decision=%q text=%q", res.IsError, res.Decision, text)
	}

	res, _ = callDynfile(t, enforce(&scriptedGate{verdicts: map[string]perms.Decision{uri: perms.Ask}}))
	if !res.IsError || res.Decision != perms.Ask {
		t.Errorf("broker ask: isError=%v decision=%q", res.IsError, res.Decision)
	}

	res, _ = callDynfile(t, enforce(&scriptedGate{verdicts: map[string]perms.Decision{uri: perms.Deny}}))
	if !res.IsError || res.Decision != perms.Deny {
		t.Errorf("broker deny: isError=%v decision=%q", res.IsError, res.Decision)
	}

	res, _ = callDynfile(t, enforce(&scriptedGate{verdicts: map[string]perms.Decision{}}))
	if !res.IsError || res.Decision != perms.Ask {
		t.Errorf("no answer: isError=%v decision=%q, want the floor's ask refused", res.IsError, res.Decision)
	}
}
