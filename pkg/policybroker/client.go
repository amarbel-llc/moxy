package policybroker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code.linenisgreat.com/moxy/pkg/perms"
)

// Defaults for Options left zero (RFC 0002 sections 3, 7 and 8). The check
// deadline is unmeasured: the design record says tens of milliseconds, to be
// measured with a unix-socket round-trip benchmark.
const (
	DefaultDeadline     = 100 * time.Millisecond
	DefaultHelloTimeout = time.Second
	DefaultBackoffMin   = 100 * time.Millisecond
	DefaultBackoffMax   = 5 * time.Second
)

// Options configures a Client.
type Options struct {
	// Addr is the broker address as CLOWN_POLICY_BROKER carries it: an
	// absolute path is a unix socket, host:port is TCP.
	Addr string
	// Subjects are the subjects this client asks to serve. Empty means "the
	// broker's own session": the client adopts the session key of the
	// broker's hello. When set, a broker whose key is not among them is
	// refused.
	Subjects []string
	// Deadline is how long one check may take before it is no answer.
	Deadline time.Duration
	// HelloTimeout bounds dialing plus the hello exchange.
	HelloTimeout time.Duration
	// BackoffMin and BackoffMax bound the reconnect delay, which doubles
	// from min to max with jitter.
	BackoffMin, BackoffMax time.Duration
	// ClientName is sent in the hello, e.g. "moxy 0.6.46".
	ClientName string
	// Stderr receives connection state changes, once each. Nil discards.
	Stderr io.Writer
}

func (o Options) deadline() time.Duration { return orDefault(o.Deadline, DefaultDeadline) }

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// ParseAddr classifies a CLOWN_POLICY_BROKER value.
func ParseAddr(v string) (network, addr string, ok bool) {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "/") {
		return "unix", v, len(v) > 1
	}
	host, port, err := net.SplitHostPort(v)
	if err != nil || host == "" || port == "" {
		return "", "", false
	}
	return "tcp", v, true
}

// FromEnv builds a Client from CLOWN_POLICY_BROKER. It returns nil when the
// variable is unset or empty (no broker: the floor decides, exactly as without
// this protocol) and when it is malformed, after saying so on stderr.
func FromEnv(getenv func(string) string, stderr io.Writer, clientName string) *Client {
	v := strings.TrimSpace(getenv(EnvAddr))
	if v == "" {
		return nil
	}
	if _, _, ok := ParseAddr(v); !ok {
		if stderr != nil {
			fmt.Fprintf(stderr, "moxy: %s=%q is neither an absolute socket path nor host:port; policy broker disabled, the static floor decides\n", EnvAddr, v)
		}
		return nil
	}
	o := Options{Addr: v, ClientName: clientName, Stderr: stderr}
	if key := strings.TrimSpace(getenv(EnvSessionKey)); key != "" {
		o.Subjects = []string{key}
	}
	return New(o)
}

// Client is a standing connection to a policy broker. It implements
// perms.Gate. Safe for concurrent use.
type Client struct {
	o             Options
	network, addr string
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	nextID        atomic.Int64
	mu            sync.Mutex
	sess          *session
	upCh          chan struct{} // closed while a session is up; replaced when it drops
	subject       string
	lastLogged    string
	// failedFirst is closed once when the first connection attempt fails (any
	// error: no socket, refused, a refused hello). WaitConnected stops waiting
	// on it, since the one-shot caller has nothing to gain from the retries.
	failedFirst  chan struct{}
	firstFailure sync.Once
}

var _ perms.Gate = (*Client)(nil)

// New starts a client that dials in the background; it never blocks, and
// until the first hello completes every Check is no answer. A malformed Addr
// yields a client that never connects.
func New(o Options) *Client {
	c := &Client{o: o, upCh: make(chan struct{}), failedFirst: make(chan struct{})}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	network, addr, ok := ParseAddr(o.Addr)
	if !ok {
		c.cancel()
		return c
	}
	c.network, c.addr = network, addr
	c.wg.Add(1)
	go c.run()
	return c
}

// Connected reports whether a hello has completed on the current connection.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sess != nil
}

// WaitConnected blocks until a hello has completed, the first connection
// attempt has failed, or ctx is done, and reports whether a session is up. It exists for the one-shot client of a short-lived process (the
// PreToolUse hook): a standing client never needs it, since a Check made
// before the hello is simply no answer.
func (c *Client) WaitConnected(ctx context.Context) bool {
	for {
		c.mu.Lock()
		up, ready := c.upCh, c.sess != nil
		c.mu.Unlock()
		if ready {
			return true
		}
		select {
		case <-up:
		case <-c.failedFirst:
			// A late success still counts: the session may have come up in
			// the same instant the channel closed.
			c.mu.Lock()
			ready = c.sess != nil
			c.mu.Unlock()
			return ready
		case <-ctx.Done():
			return false
		case <-c.ctx.Done():
			return false
		}
	}
}

// Subject is the subject checks name: the broker's session key once connected
// (and after, across a reconnect, until a new hello replaces it).
func (c *Client) Subject() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subject
}

// Close stops the client and drops the connection. Checks afterwards are no
// answer.
func (c *Client) Close() error {
	c.cancel()
	c.mu.Lock()
	s := c.sess
	c.mu.Unlock()
	if s != nil {
		s.close()
	}
	c.wg.Wait()
	return nil
}

func (c *Client) logf(key, format string, args ...any) {
	if c.o.Stderr == nil {
		return
	}
	c.mu.Lock()
	if c.lastLogged == key {
		c.mu.Unlock()
		return
	}
	c.lastLogged = key
	c.mu.Unlock()
	fmt.Fprintf(c.o.Stderr, "moxy: policy broker: "+format+"\n", args...)
}

func (c *Client) run() {
	defer c.wg.Done()
	backoff := orDefault(c.o.BackoffMin, DefaultBackoffMin)
	maxBackoff := orDefault(c.o.BackoffMax, DefaultBackoffMax)
	for c.ctx.Err() == nil {
		s, err := c.connect()
		if err != nil {
			c.firstFailure.Do(func() { close(c.failedFirst) })
			c.logf("down:"+err.Error(), "unavailable (%v); the static floor decides until it answers", err)
			c.sleep(jitter(backoff))
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = orDefault(c.o.BackoffMin, DefaultBackoffMin)
		c.mu.Lock()
		c.sess = s
		close(c.upCh) // wake WaitConnected
		c.mu.Unlock()
		c.logf("up:"+s.key, "connected to %s (session %s)", c.addr, s.key)
		s.readLoop()
		c.mu.Lock()
		c.sess = nil
		c.upCh = make(chan struct{})
		c.mu.Unlock()
		s.close()
		c.sleep(jitter(backoff))
	}
}

func (c *Client) sleep(d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-c.ctx.Done():
	}
}

// jitter spreads d over [d/2, d).
func jitter(d time.Duration) time.Duration {
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + rand.N(half)
}

// connect dials and exchanges hellos. Any failure is a refusal: the
// connection is closed and the caller backs off.
func (c *Client) connect() (*session, error) {
	timeout := orDefault(c.o.HelloTimeout, DefaultHelloTimeout)
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(c.ctx, c.network, c.addr)
	if err != nil {
		return nil, err
	}
	s := &session{conn: conn, r: bufio.NewReaderSize(conn, MaxLine), pending: map[int64]chan result{}, dead: make(chan struct{})}
	fail := func(err error) (*session, error) {
		s.close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	hello := Message{
		V: Version, Type: TypeHello, Client: c.o.ClientName,
		Capabilities: []string{CapCheck}, Subjects: c.o.Subjects,
	}
	if err := s.send(hello); err != nil {
		return fail(fmt.Errorf("hello: %w", err))
	}
	m, err := s.readMessage()
	if err != nil {
		return fail(fmt.Errorf("hello: %w", err))
	}
	switch {
	case m.Type != TypeHello:
		return fail(fmt.Errorf("hello refused: first message was %q", m.Type))
	case m.V != Version:
		return fail(fmt.Errorf("hello refused: broker speaks protocol %d, want %d", m.V, Version))
	case !slices.Contains(m.Capabilities, CapCheck):
		return fail(errors.New("hello refused: broker lacks the check capability"))
	case m.SessionKey == "":
		return fail(errors.New("hello refused: broker sent no session key"))
	case len(c.o.Subjects) > 0 && !slices.Contains(c.o.Subjects, m.SessionKey):
		return fail(fmt.Errorf("hello refused: broker session %q is not one this client serves", m.SessionKey))
	}
	_ = conn.SetDeadline(time.Time{})
	s.key = m.SessionKey
	c.mu.Lock()
	c.subject = m.SessionKey
	c.mu.Unlock()
	return s, nil
}

type result struct {
	dec    perms.Decision
	reason string
	ok     bool
}

var noAnswer = result{dec: perms.Unknown}

// session is one established connection.
type session struct {
	conn net.Conn
	r    *bufio.Reader
	key  string

	wmu sync.Mutex

	mu      sync.Mutex
	pending map[int64]chan result
	dead    chan struct{}
	closed  bool
}

func (s *session) send(m Message) error {
	b, err := Encode(m)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err = s.conn.Write(b)
	return err
}

func (s *session) readMessage() (Message, error) {
	line, err := s.r.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return Message{}, fmt.Errorf("message longer than %d bytes", MaxLine)
		}
		return Message{}, err
	}
	return Decode(line)
}

func (s *session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.dead)
	s.mu.Unlock()
	_ = s.conn.Close()
}

func (s *session) deliver(id int64, r result) {
	s.mu.Lock()
	ch, ok := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if ok {
		ch <- r
	}
}

// readLoop dispatches verdicts to their checks until the connection ends or
// the broker breaks the protocol; either way the caller drops the session.
func (s *session) readLoop() {
	for {
		m, err := s.readMessage()
		if err != nil {
			return
		}
		switch m.Type {
		case TypeVerdict:
			dec, ok := decisionOf(m.Verdict)
			if !ok {
				s.deliver(m.ID, noAnswer)
				return // a verdict outside the three is a protocol error
			}
			s.deliver(m.ID, result{dec: dec, reason: m.Reason, ok: true})
		case TypeError:
			if m.ID == 0 {
				return // connection-level error: the broker is closing
			}
			s.deliver(m.ID, noAnswer)
		}
	}
}

func decisionOf(verdict string) (perms.Decision, bool) {
	switch verdict {
	case VerdictAllow:
		return perms.Allow, true
	case VerdictAsk:
		return perms.Ask, true
	case VerdictDeny:
		return perms.Deny, true
	}
	return perms.Unknown, false
}

// Check asks the broker for a verdict on op over res. answered is false when
// there is no answer — not connected, the deadline passed, the connection
// dropped, or the broker answered with an error — and only then does the
// caller apply its static floor (RFC 0002 section 6). Check never turns
// absence into a decision.
func (c *Client) Check(ctx context.Context, op perms.Op, res perms.Resource) (perms.Decision, string, bool) {
	c.mu.Lock()
	s, subject := c.sess, c.subject
	c.mu.Unlock()
	if s == nil {
		return perms.Unknown, "policy broker not connected", false
	}

	wait := c.o.deadline()
	if dl, ok := ctx.Deadline(); ok {
		wait = min(wait, time.Until(dl))
	}
	if wait <= 0 {
		return perms.Unknown, "policy broker: no time left to ask", false
	}

	id := c.nextID.Add(1)
	ch := make(chan result, 1)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return perms.Unknown, "policy broker connection closed", false
	}
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	err := s.send(Message{
		V: Version, Type: TypeCheck, ID: id, Subject: subject, Op: string(op),
		Resource:   &ResourceWire{Kind: res.Kind, URI: res.URI},
		DeadlineMS: max(1, wait.Milliseconds()),
	})
	if err != nil {
		s.close()
		return perms.Unknown, "policy broker write failed", false
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case r := <-ch:
		if !r.ok {
			return perms.Unknown, "policy broker gave no verdict", false
		}
		return r.dec, r.reason, true
	case <-timer.C:
		return perms.Unknown, fmt.Sprintf("policy broker did not answer within %s", wait), false
	case <-ctx.Done():
		return perms.Unknown, "policy broker: caller gave up", false
	case <-s.dead:
		return perms.Unknown, "policy broker connection lost", false
	}
}
