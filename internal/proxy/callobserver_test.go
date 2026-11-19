package proxy

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/pkg/perms"
)

type recordingObserver struct {
	mu    sync.Mutex
	names []string
	args  []string
}

func (o *recordingObserver) Observe(name string, args json.RawMessage) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.names = append(o.names, name)
	o.args = append(o.args, string(args))
}

type panickingObserver struct{}

func (panickingObserver) Observe(string, json.RawMessage) { panic("observer exploded") }

func echoProxy() *Proxy {
	child := &toolsListBackend{
		name:      "srv",
		toolsList: `{"content":[{"type":"text","text":"ok"}]}`,
	}
	return &Proxy{children: []ChildEntry{{Client: child, Capabilities: toolsCapable()}}}
}

func dispatchJSON(t *testing.T, p *Proxy, name, args string) string {
	t.Helper()
	res, err := p.CallToolV1(context.Background(), name, json.RawMessage(args))
	if err != nil {
		t.Fatalf("CallToolV1: %v", err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// Shadow mode (moxy#489): a call observer sees every dispatch and changes
// nothing about its outcome.
func TestCallObserverSeesDispatchAndOutcomeIsIdentical(t *testing.T) {
	const args = `{"a":1}`
	off := dispatchJSON(t, echoProxy(), "srv.thing", args)

	rec := &recordingObserver{}
	p := echoProxy()
	p.SetCallObserver(rec)
	on := dispatchJSON(t, p, "srv.thing", args)

	if on != off {
		t.Errorf("outcome differs with the observer on:\n on: %s\noff: %s", on, off)
	}
	if len(rec.names) != 1 || rec.names[0] != "srv.thing" || rec.args[0] != args {
		t.Errorf("observer saw names=%v args=%v", rec.names, rec.args)
	}
}

func TestCallObserverSeesRefusedCallsToo(t *testing.T) {
	rec := &recordingObserver{}
	p := echoProxy()
	p.SetCallObserver(rec)
	_ = dispatchJSON(t, p, "nosuch.thing", `{}`) // unknown server: an error result
	if len(rec.names) != 1 {
		t.Errorf("observer saw %v, want the one refused call", rec.names)
	}
}

// The observer is told about a batch once, as the wrapper, which presents its
// inner calls; the inner calls the batch then dispatches are not observed a
// second time (moxy#523: they were counted twice).
func TestBatchInnerCallsAreNotObservedAgain(t *testing.T) {
	rec := &recordingObserver{}
	p := echoProxy()
	p.SetResolver(perms.NewResolverWithPerms(map[string]perms.ToolPermInfo{
		"srv.thing": {Perm: native.PermsAlwaysAllow},
	}))
	p.SetCallObserver(rec)

	res, err := p.HandleBatch(context.Background(), json.RawMessage(
		`{"calls":[{"tool":"srv.thing","args":{"a":1}},{"tool":"srv.thing","args":{"a":2}}]}`))
	if err != nil || res.IsError {
		t.Fatalf("HandleBatch: res=%+v err=%v", res, err)
	}
	if len(rec.names) != 0 {
		t.Fatalf("the batch's inner calls were observed again: %v", rec.names)
	}

	// A direct call is still observed.
	_ = dispatchJSON(t, p, "srv.thing", `{"a":3}`)
	if len(rec.names) != 1 {
		t.Fatalf("a direct call must still be observed once, got %v", rec.names)
	}
}

func TestPanickingCallObserverChangesNothing(t *testing.T) {
	const args = `{"a":1}`
	off := dispatchJSON(t, echoProxy(), "srv.thing", args)

	p := echoProxy()
	p.SetCallObserver(panickingObserver{})
	if on := dispatchJSON(t, p, "srv.thing", args); on != off {
		t.Errorf("outcome differs with a panicking observer:\n on: %s\noff: %s", on, off)
	}
}

func TestNilCallObserverIsOff(t *testing.T) {
	p := echoProxy()
	p.SetCallObserver(nil)
	_ = dispatchJSON(t, p, "srv.thing", `{}`) // must not panic
}
