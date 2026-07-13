package mcpclient

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
)

// stubTransport drives readLoop straight to its termination path: Read returns
// the configured error on the first call, Write is a no-op success. It lets a
// test close c.done for a chosen reason (EOF vs a non-EOF read error) without a
// real child process.
type stubTransport struct {
	readErr error
}

func (s *stubTransport) Read() (*jsonrpc.Message, error) { return nil, s.readErr }
func (s *stubTransport) Write(*jsonrpc.Message) error    { return nil }
func (s *stubTransport) Close() error                    { return nil }

// A call that finds the child gone must report *why* the connection died: a
// clean stdout EOF (the child exited) reads differently from a non-EOF read
// error (framing/pipe corruption, where the child may still be running). The
// pre-fix message collapsed both into "exited unexpectedly", which is what the
// #275 triage could not disambiguate. Regression for #275.
func TestCallChildGoneErrorDistinguishesEOFFromReadError(t *testing.T) {
	cases := []struct {
		name    string
		readErr error
		want    []string
		notWant []string
	}{
		{
			name:    "clean EOF means the child exited",
			readErr: io.EOF,
			want:    []string{"chrest", "exited", "EOF"},
			notWant: []string{"may still be running"},
		},
		{
			name:    "non-EOF read error may leave a live child",
			readErr: errors.New("invalid character 'x' looking for beginning of value"),
			want:    []string{"chrest", "read error", "may still be running"},
			notWant: []string{"exited"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{
				name:      "chrest",
				transport: &stubTransport{readErr: tc.readErr},
				pending:   make(map[string]chan *jsonrpc.Message),
				done:      make(chan struct{}),
			}
			// Run readLoop synchronously so c.done is closed and the
			// termination reason recorded before the call.
			c.readLoop()

			_, err := c.Call(context.Background(), "browser-info", nil)
			if err == nil {
				t.Fatal("Call returned nil error, want a child-gone error")
			}
			msg := err.Error()
			for _, w := range tc.want {
				if !strings.Contains(msg, w) {
					t.Errorf("error %q missing substring %q", msg, w)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(msg, nw) {
					t.Errorf("error %q unexpectedly contains %q", msg, nw)
				}
			}
		})
	}
}
