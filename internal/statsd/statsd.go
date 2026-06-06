// Package statsd emits fire-and-forget tool-dispatch metrics over UDP in
// classic statsd line protocol (no tags; dimensions live in the metric name):
//
//	moxy.<server>.<tool>.duration:<ms>|ms
//	moxy.<server>.<tool>.success:1|c     (or .failure / .abandoned)
//
// The connection is dialed once at process startup from STATSD_HOST (default
// 127.0.0.1) and STATSD_PORT (default 8125). MOXY_DISABLE_STATSD=1 disables
// emission entirely, as does a failed dial. Emission never blocks or fails a
// dispatch: write errors are swallowed, and every public function is a no-op
// when disabled. Mirrors the lifecyclelog idiom (package-level, init from
// env, silent no-op on failure).
package statsd

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Outcome classifies a tool dispatch for the counter metric.
type Outcome string

const (
	OutcomeSuccess   Outcome = "success"
	OutcomeFailure   Outcome = "failure"
	OutcomeAbandoned Outcome = "abandoned"
)

// OutcomeFor classifies one dispatch (or resource read / prompt get) for
// the counter metric. An error counts as failure — unless the context was
// already done, which counts as abandoned (the client gave up; the call
// didn't fail on its own terms). A tool-level error result also counts as
// failure. Shared by every emit site so the classification can't drift.
func OutcomeFor(ctxErr, err error, resultIsError bool) Outcome {
	switch {
	case err != nil && ctxErr != nil:
		return OutcomeAbandoned
	case err != nil:
		return OutcomeFailure
	case resultIsError:
		return OutcomeFailure
	default:
		return OutcomeSuccess
	}
}

var (
	mu   sync.RWMutex
	conn net.Conn
)

func init() {
	ReinitFromEnv()
}

// ReinitFromEnv closes any existing connection and re-dials from the
// STATSD_HOST / STATSD_PORT / MOXY_DISABLE_STATSD environment variables.
// Called once automatically at init; exported so tests can point emission
// at a local listener after t.Setenv.
func ReinitFromEnv() {
	mu.Lock()
	defer mu.Unlock()
	if conn != nil {
		_ = conn.Close()
		conn = nil
	}
	if os.Getenv("MOXY_DISABLE_STATSD") == "1" {
		return
	}
	host := os.Getenv("STATSD_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("STATSD_PORT")
	if port == "" {
		port = "8125"
	}
	c, err := net.Dial("udp", net.JoinHostPort(host, port))
	if err != nil {
		// Metrics must never break moxy: run disabled.
		return
	}
	conn = c
}

// EmitToolDispatch sends one UDP packet carrying the duration timer and the
// outcome counter for a single tool dispatch. No-op when disabled; write
// errors are swallowed.
func EmitToolDispatch(server, tool string, d time.Duration, outcome Outcome) {
	mu.RLock()
	c := conn
	mu.RUnlock()
	if c == nil {
		return
	}
	_, _ = c.Write([]byte(dispatchPacket(server, tool, d, outcome)))
}

// dispatchPacket renders the two newline-separated statsd lines for one
// dispatch (a multi-metric packet per the statsd datagram convention).
func dispatchPacket(server, tool string, d time.Duration, outcome Outcome) string {
	base := "moxy." + sanitizeSegment(server) + "." + sanitizeSegment(tool)
	return fmt.Sprintf(
		"%s.duration:%d|ms\n%s.%s:1|c",
		base, d.Milliseconds(), base, outcome,
	)
}

// sanitizeSegment maps a server or tool name onto the metric-name alphabet:
// [a-zA-Z0-9_-] pass through, everything else (dots, slashes, spaces, ...)
// becomes '_' so it can't introduce extra name segments or break the line
// protocol. An empty segment becomes '_' to keep the name well-formed.
func sanitizeSegment(s string) string {
	if s == "" {
		return "_"
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
