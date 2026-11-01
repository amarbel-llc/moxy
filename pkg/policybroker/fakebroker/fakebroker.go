// Package fakebroker is an in-process policy broker for tests (RFC 0002): a
// listener that speaks the broker side of the protocol and answers checks
// from a script. It is the test double for the client in pkg/policybroker; the
// real broker is spinclass serve.
package fakebroker

import (
	"bufio"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"code.linenisgreat.com/moxy/pkg/policybroker"
)

// Answer is a scripted reply to one check. Verdict "" sends nothing (a hung
// broker); Error sends an error message instead of a verdict.
type Answer struct {
	Verdict string
	Reason  string
	Error   string
	Delay   time.Duration
}

// Fake is a listening policy broker.
type Fake struct {
	ln net.Listener

	mu         sync.Mutex
	sessionKey string
	hello      func(client policybroker.Message) *policybroker.Message
	answer     func(policybroker.Message) Answer
	conns      []net.Conn
	checks     []policybroker.Message
	hellos     []policybroker.Message
	accepted   int
}

// New starts a fake broker on a unix socket in a temp directory. By default
// it greets with sessionKey and answers every check allow.
func New(t testing.TB, sessionKey string) *Fake {
	t.Helper()
	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "broker.sock"))
	if err != nil {
		t.Fatalf("fakebroker: listen: %v", err)
	}
	f := &Fake{ln: ln, sessionKey: sessionKey}
	f.answer = func(policybroker.Message) Answer { return Answer{Verdict: policybroker.VerdictAllow} }
	go f.accept()
	t.Cleanup(f.Close)
	return f
}

// Addr is the value for CLOWN_POLICY_BROKER.
func (f *Fake) Addr() string { return f.ln.Addr().String() }

// Network is "unix".
func (f *Fake) Network() string { return "unix" }

// Answer sets the script that answers checks.
func (f *Fake) Answer(fn func(check policybroker.Message) Answer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answer = fn
}

// Hello overrides the broker's hello; returning nil sends none (the client
// should time out and refuse).
func (f *Fake) Hello(fn func(client policybroker.Message) *policybroker.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hello = fn
}

// Checks returns the checks received so far.
func (f *Fake) Checks() []policybroker.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]policybroker.Message(nil), f.checks...)
}

// Hellos returns the client hellos received so far, one per connection.
func (f *Fake) Hellos() []policybroker.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]policybroker.Message(nil), f.hellos...)
}

// Accepted is how many connections were accepted.
func (f *Fake) Accepted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.accepted
}

// DropConnections closes every live connection but keeps listening: a broker
// that crashed and came back.
func (f *Fake) DropConnections() {
	f.mu.Lock()
	conns := f.conns
	f.conns = nil
	f.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// Close stops listening and drops every connection.
func (f *Fake) Close() {
	_ = f.ln.Close()
	f.DropConnections()
}

func (f *Fake) accept() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conns = append(f.conns, c)
		f.accepted++
		f.mu.Unlock()
		go f.serve(c)
	}
}

func (f *Fake) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReaderSize(c, policybroker.MaxLine)
	var wmu sync.Mutex
	send := func(m policybroker.Message) {
		b, err := policybroker.Encode(m)
		if err != nil {
			return
		}
		wmu.Lock()
		defer wmu.Unlock()
		_, _ = c.Write(b)
	}

	line, err := r.ReadBytes('\n')
	if err != nil {
		return
	}
	clientHello, err := policybroker.Decode(line)
	if err != nil {
		return
	}
	f.mu.Lock()
	f.hellos = append(f.hellos, clientHello)
	helloFn, key := f.hello, f.sessionKey
	f.mu.Unlock()

	greeting := &policybroker.Message{
		V: policybroker.Version, Type: policybroker.TypeHello, Broker: "fakebroker",
		Capabilities: []string{policybroker.CapCheck}, SessionKey: key,
	}
	if helloFn != nil {
		greeting = helloFn(clientHello)
	}
	if greeting == nil {
		// say nothing, hold the line open until the peer gives up
		_, _ = r.ReadBytes('\n')
		return
	}
	send(*greeting)

	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		m, err := policybroker.Decode(line)
		if err != nil || m.Type != policybroker.TypeCheck {
			continue
		}
		f.mu.Lock()
		f.checks = append(f.checks, m)
		answer := f.answer
		f.mu.Unlock()
		go func(m policybroker.Message) {
			a := answer(m)
			if a.Delay > 0 {
				time.Sleep(a.Delay)
			}
			switch {
			case a.Error != "":
				send(policybroker.Message{V: policybroker.Version, Type: policybroker.TypeError, ID: m.ID, Code: a.Error})
			case a.Verdict != "":
				send(policybroker.Message{V: policybroker.Version, Type: policybroker.TypeVerdict, ID: m.ID, Verdict: a.Verdict, Reason: a.Reason})
			}
		}(m)
	}
}
