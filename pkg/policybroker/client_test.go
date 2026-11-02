package policybroker_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"code.linenisgreat.com/moxy/pkg/perms"
	"code.linenisgreat.com/moxy/pkg/policybroker"
	"code.linenisgreat.com/moxy/pkg/policybroker/fakebroker"
)

var testFile = perms.Resource{Kind: perms.KindFile, URI: "file:///work/a.txt"}

func newClient(t *testing.T, f *fakebroker.Fake, mutate func(*policybroker.Options)) *policybroker.Client {
	t.Helper()
	o := policybroker.Options{
		Addr:         f.Addr(),
		Deadline:     500 * time.Millisecond,
		HelloTimeout: 500 * time.Millisecond,
		BackoffMin:   5 * time.Millisecond,
		BackoffMax:   20 * time.Millisecond,
		ClientName:   "moxy test",
	}
	if mutate != nil {
		mutate(&o)
	}
	c := policybroker.New(o)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func waitConnected(t *testing.T, c *policybroker.Client) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !c.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("client never completed the hello")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestFromEnvUnsetMeansNoBroker(t *testing.T) {
	for _, val := range []string{"", "   "} {
		env := func(string) string { return val }
		if c := policybroker.FromEnv(env, nil, "t"); c != nil {
			t.Errorf("FromEnv(%q) = %v, want nil", val, c)
		}
	}
}

func TestFromEnvRejectsAnAddressThatIsNeitherPathNorHostPort(t *testing.T) {
	var stderr bytes.Buffer
	env := func(string) string { return "broker.sock" }
	if c := policybroker.FromEnv(env, &stderr, "t"); c != nil {
		t.Fatalf("FromEnv = %v, want nil", c)
	}
	if !strings.Contains(stderr.String(), policybroker.EnvAddr) {
		t.Errorf("diagnostic %q does not name %s", stderr.String(), policybroker.EnvAddr)
	}
}

func TestHelloBothWaysAndCheckShape(t *testing.T) {
	f := fakebroker.New(t, "sess-1")
	c := newClient(t, f, nil)
	waitConnected(t, c)

	if got := c.Subject(); got != "sess-1" {
		t.Errorf("Subject = %q, want the broker's session key", got)
	}
	hellos := f.Hellos()
	if len(hellos) != 1 {
		t.Fatalf("hellos = %v", hellos)
	}
	h := hellos[0]
	if h.V != 1 || h.Type != "hello" || h.Client != "moxy test" || len(h.Subjects) != 0 ||
		len(h.Capabilities) != 1 || h.Capabilities[0] != "check" {
		t.Errorf("client hello = %+v", h)
	}

	dec, _, answered := c.Check(context.Background(), perms.Read, testFile)
	if !answered || dec != perms.Allow {
		t.Fatalf("Check = (%q, %v), want allow", dec, answered)
	}
	checks := f.Checks()
	if len(checks) != 1 {
		t.Fatalf("checks = %v", checks)
	}
	m := checks[0]
	if m.V != 1 || m.Type != "check" || m.ID == 0 || m.Subject != "sess-1" || m.Op != "read" ||
		m.Resource == nil || m.Resource.Kind != "file" || m.Resource.URI != testFile.URI || m.DeadlineMS != 500 {
		t.Errorf("check = %+v", m)
	}
}

func TestVerdictsMapToDecisions(t *testing.T) {
	f := fakebroker.New(t, "s")
	f.Answer(func(m policybroker.Message) fakebroker.Answer {
		switch m.Resource.URI {
		case "file:///ask":
			return fakebroker.Answer{Verdict: "ask", Reason: "why ask"}
		case "file:///deny":
			return fakebroker.Answer{Verdict: "deny", Reason: "why deny"}
		}
		return fakebroker.Answer{Verdict: "allow", Reason: "why allow"}
	})
	c := newClient(t, f, nil)
	waitConnected(t, c)
	for uri, want := range map[string]perms.Decision{
		"file:///ask": perms.Ask, "file:///deny": perms.Deny, "file:///ok": perms.Allow,
	} {
		dec, reason, answered := c.Check(context.Background(), perms.Write, perms.Resource{Kind: perms.KindFile, URI: uri})
		if !answered || dec != want || !strings.HasPrefix(reason, "why ") {
			t.Errorf("Check(%s) = (%q, %q, %v), want %q", uri, dec, reason, answered, want)
		}
	}
}

func TestNotConnectedIsNoAnswerAndDoesNotBlock(t *testing.T) {
	f := fakebroker.New(t, "s")
	addr := f.Addr()
	f.Close() // nothing listens any more
	c := policybroker.New(policybroker.Options{Addr: addr, Deadline: time.Second, BackoffMin: time.Millisecond, BackoffMax: 5 * time.Millisecond})
	t.Cleanup(func() { _ = c.Close() })
	start := time.Now()
	dec, _, answered := c.Check(context.Background(), perms.Read, testFile)
	if answered || dec != perms.Unknown {
		t.Fatalf("Check = (%q, %v), want no answer", dec, answered)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Errorf("an unreachable broker held the check for %s", time.Since(start))
	}
}

func TestRefusedHelloIsNoAnswer(t *testing.T) {
	cases := map[string]struct {
		subjects []string
		hello    func(policybroker.Message) *policybroker.Message
	}{
		"wrong session key": {
			subjects: []string{"mine"},
			hello: func(policybroker.Message) *policybroker.Message {
				return &policybroker.Message{V: 1, Type: "hello", Capabilities: []string{"check"}, SessionKey: "someone-else"}
			},
		},
		"no session key": {
			hello: func(policybroker.Message) *policybroker.Message {
				return &policybroker.Message{V: 1, Type: "hello", Capabilities: []string{"check"}}
			},
		},
		"other version": {
			hello: func(policybroker.Message) *policybroker.Message {
				return &policybroker.Message{V: 2, Type: "hello", Capabilities: []string{"check"}, SessionKey: "s"}
			},
		},
		"no check capability": {
			hello: func(policybroker.Message) *policybroker.Message {
				return &policybroker.Message{V: 1, Type: "hello", SessionKey: "s"}
			},
		},
		"not a hello": {
			hello: func(policybroker.Message) *policybroker.Message {
				return &policybroker.Message{V: 1, Type: "verdict", SessionKey: "s"}
			},
		},
		"silence": {
			hello: func(policybroker.Message) *policybroker.Message { return nil },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := fakebroker.New(t, "s")
			f.Hello(tc.hello)
			c := newClient(t, f, func(o *policybroker.Options) {
				o.Subjects = tc.subjects
				o.HelloTimeout = 50 * time.Millisecond
			})
			time.Sleep(120 * time.Millisecond) // let at least one attempt finish
			if c.Connected() {
				t.Fatal("client considers itself connected after a refused hello")
			}
			dec, _, answered := c.Check(context.Background(), perms.Read, testFile)
			if answered || dec != perms.Unknown {
				t.Errorf("Check = (%q, %v), want no answer", dec, answered)
			}
			if len(f.Checks()) != 0 {
				t.Errorf("a check was sent over a refused connection: %v", f.Checks())
			}
		})
	}
}

func TestKnownSubjectMustMatchTheBroker(t *testing.T) {
	f := fakebroker.New(t, "mine")
	c := newClient(t, f, func(o *policybroker.Options) { o.Subjects = []string{"other", "mine"} })
	waitConnected(t, c)
	if got := c.Subject(); got != "mine" {
		t.Errorf("Subject = %q", got)
	}
	if h := f.Hellos()[0]; len(h.Subjects) != 2 {
		t.Errorf("client hello subjects = %v", h.Subjects)
	}
}

func TestMissedDeadlineIsNoAnswer(t *testing.T) {
	f := fakebroker.New(t, "s")
	f.Answer(func(policybroker.Message) fakebroker.Answer {
		return fakebroker.Answer{Verdict: "allow", Delay: 300 * time.Millisecond}
	})
	c := newClient(t, f, func(o *policybroker.Options) { o.Deadline = 30 * time.Millisecond })
	waitConnected(t, c)
	start := time.Now()
	dec, _, answered := c.Check(context.Background(), perms.Read, testFile)
	if answered || dec != perms.Unknown {
		t.Fatalf("Check = (%q, %v), want no answer", dec, answered)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("deadline 30ms but Check took %s", elapsed)
	}
	// the late verdict must not poison the next check
	f.Answer(func(policybroker.Message) fakebroker.Answer { return fakebroker.Answer{Verdict: "deny"} })
	time.Sleep(350 * time.Millisecond)
	if dec, _, answered := c.Check(context.Background(), perms.Read, testFile); !answered || dec != perms.Deny {
		t.Errorf("next Check = (%q, %v), want deny", dec, answered)
	}
}

func TestContextDeadlineBoundsTheCheck(t *testing.T) {
	f := fakebroker.New(t, "s")
	f.Answer(func(policybroker.Message) fakebroker.Answer {
		return fakebroker.Answer{Verdict: "allow", Delay: time.Second}
	})
	c := newClient(t, f, nil)
	waitConnected(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, answered := c.Check(ctx, perms.Read, testFile); answered {
		t.Fatal("answered past the caller's deadline")
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Errorf("Check ignored the context deadline: %s", time.Since(start))
	}
}

func TestErrorAndUnknownVerdictAreNoAnswer(t *testing.T) {
	f := fakebroker.New(t, "s")
	f.Answer(func(m policybroker.Message) fakebroker.Answer {
		if m.Resource.URI == "file:///err" {
			return fakebroker.Answer{Error: "internal"}
		}
		return fakebroker.Answer{Verdict: "maybe"}
	})
	c := newClient(t, f, nil)
	waitConnected(t, c)
	for _, uri := range []string{"file:///err", "file:///weird"} {
		dec, _, answered := c.Check(context.Background(), perms.Read, perms.Resource{Kind: perms.KindFile, URI: uri})
		if answered || dec != perms.Unknown {
			t.Errorf("Check(%s) = (%q, %v), want no answer", uri, dec, answered)
		}
	}
}

func TestDroppedConnectionIsNoAnswerThenReconnects(t *testing.T) {
	f := fakebroker.New(t, "s")
	hang := make(chan struct{})
	f.Answer(func(policybroker.Message) fakebroker.Answer {
		<-hang
		return fakebroker.Answer{}
	})
	c := newClient(t, f, nil)
	waitConnected(t, c)

	// a check in flight when the broker dies is no answer, promptly
	var wg sync.WaitGroup
	wg.Add(1)
	var answered bool
	go func() {
		defer wg.Done()
		_, _, answered = c.Check(context.Background(), perms.Read, testFile)
	}()
	for len(f.Checks()) == 0 {
		time.Sleep(time.Millisecond)
	}
	f.DropConnections()
	wg.Wait()
	close(hang)
	if answered {
		t.Fatal("a check answered although its connection dropped")
	}

	// the client dials again and a fresh hello precedes the next check
	f.Answer(func(policybroker.Message) fakebroker.Answer { return fakebroker.Answer{Verdict: "deny"} })
	deadline := time.Now().Add(3 * time.Second)
	for f.Accepted() < 2 || !c.Connected() {
		if time.Now().After(deadline) {
			t.Fatalf("no reconnect: accepted=%d connected=%v", f.Accepted(), c.Connected())
		}
		time.Sleep(2 * time.Millisecond)
	}
	if len(f.Hellos()) != 2 {
		t.Errorf("hellos = %d, want a hello per connection", len(f.Hellos()))
	}
	if dec, _, ok := c.Check(context.Background(), perms.Read, testFile); !ok || dec != perms.Deny {
		t.Errorf("Check after reconnect = (%q, %v), want deny", dec, ok)
	}
}

func TestConcurrentChecksMatchRepliesByID(t *testing.T) {
	f := fakebroker.New(t, "s")
	f.Answer(func(m policybroker.Message) fakebroker.Answer {
		// replies arrive in reverse order of the delay each check asks for
		switch m.Resource.URI {
		case "file:///slow-allow":
			return fakebroker.Answer{Verdict: "allow", Delay: 60 * time.Millisecond}
		}
		return fakebroker.Answer{Verdict: "deny"}
	})
	c := newClient(t, f, nil)
	waitConnected(t, c)
	var wg sync.WaitGroup
	results := make([]perms.Decision, 2)
	for i, uri := range []string{"file:///slow-allow", "file:///fast-deny"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _, _ = c.Check(context.Background(), perms.Read, perms.Resource{Kind: perms.KindFile, URI: uri})
		}()
	}
	wg.Wait()
	if results[0] != perms.Allow || results[1] != perms.Deny {
		t.Errorf("results = %v, want [allow deny]", results)
	}
}

func TestCloseStopsReconnectingAndAnswersNothing(t *testing.T) {
	f := fakebroker.New(t, "s")
	c := newClient(t, f, nil)
	waitConnected(t, c)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, answered := c.Check(context.Background(), perms.Read, testFile); answered {
		t.Error("closed client answered")
	}
}

func TestWaitConnectedBlocksUntilTheHello(t *testing.T) {
	f := fakebroker.New(t, "s")
	c := newClient(t, f, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !c.WaitConnected(ctx) {
		t.Fatal("WaitConnected = false with a healthy broker")
	}
	if !c.Connected() {
		t.Error("WaitConnected returned before the session was up")
	}

	down := fakebroker.New(t, "s")
	addr := down.Addr()
	down.Close()
	c2 := policybroker.New(policybroker.Options{Addr: addr, BackoffMin: time.Millisecond, BackoffMax: 5 * time.Millisecond})
	t.Cleanup(func() { _ = c2.Close() })
	short, cancel2 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel2()
	if c2.WaitConnected(short) {
		t.Error("WaitConnected = true with nothing listening")
	}
}

// A dial that fails outright (nothing at the path, nothing listening) must end
// WaitConnected at once: the hook would otherwise spend its whole hello wait
// (250 ms) on every call to a dead broker (moxy#494).
func TestWaitConnectedReturnsAtOnceWhenTheFirstDialFails(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.sock")
	c := policybroker.New(policybroker.Options{Addr: absent})
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if c.WaitConnected(ctx) {
		t.Fatal("WaitConnected = true with no socket")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("WaitConnected took %s after a failed dial, want it to return at once", d)
	}
}

func TestFromEnvTakesTheExpectedSubjectFromSpinclass(t *testing.T) {
	envFor := func(addr, key string) func(string) string {
		return func(name string) string {
			switch name {
			case policybroker.EnvAddr:
				return addr
			case policybroker.EnvSessionKey:
				return key
			}
			return ""
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	f := fakebroker.New(t, "moxy/neat-linden")
	match := policybroker.FromEnv(envFor(f.Addr(), "moxy/neat-linden"), nil, "t")
	t.Cleanup(func() { _ = match.Close() })
	if !match.WaitConnected(ctx) {
		t.Fatal("a broker whose key matches SPINCLASS_SESSION_ID was refused")
	}

	other := policybroker.FromEnv(envFor(f.Addr(), "somebody/else"), nil, "t")
	t.Cleanup(func() { _ = other.Close() })
	short, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	if other.WaitConnected(short) {
		t.Error("a broker answering for another session was accepted")
	}

	adopt := policybroker.FromEnv(envFor(f.Addr(), ""), nil, "t")
	t.Cleanup(func() { _ = adopt.Close() })
	if !adopt.WaitConnected(ctx) || adopt.Subject() != "moxy/neat-linden" {
		t.Errorf("with SPINCLASS_SESSION_ID unset the client must adopt the broker's key; subject = %q", adopt.Subject())
	}
}

func TestTCPAddressForm(t *testing.T) {
	// the confined form is host:port; the client must dial it as TCP
	o := policybroker.Options{Addr: "127.0.0.1:1"}
	network, addr, ok := policybroker.ParseAddr(o.Addr)
	if !ok || network != "tcp" || addr != "127.0.0.1:1" {
		t.Errorf("ParseAddr = (%q, %q, %v)", network, addr, ok)
	}
	network, addr, ok = policybroker.ParseAddr("/run/x/broker.sock")
	if !ok || network != "unix" || addr != "/run/x/broker.sock" {
		t.Errorf("ParseAddr = (%q, %q, %v)", network, addr, ok)
	}
	for _, bad := range []string{"", "broker.sock", "host", ":", "host:"} {
		if _, _, ok := policybroker.ParseAddr(bad); ok {
			t.Errorf("ParseAddr(%q) ok", bad)
		}
	}
}
