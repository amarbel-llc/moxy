package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"1024", 1024, false},
		{"0", 0, false},
		{"64MB", 64 << 20, false},
		{"64MiB", 64 << 20, false},
		{"64M", 64 << 20, false},
		{"1G", 1 << 30, false},
		{"  32K ", 32 << 10, false},
		{"1T", 1 << 40, false},
		{"", 0, true},
		{"abc", 0, true},
		{"12x", 0, true},
		{"1.5M", 0, true},
	}
	for _, tc := range cases {
		got, err := parseSize(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseSize(%q) = %d, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSize(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestChildMaxMessageBytes(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		val  string
		want int64
	}{
		{"unset uses default", false, "", childMaxMessageDefault},
		{"valid override", true, "128MiB", 128 << 20},
		{"invalid falls back", true, "not-a-size", childMaxMessageDefault},
		{"zero falls back", true, "0", childMaxMessageDefault},
		{"negative-ish garbage falls back", true, "-5", childMaxMessageDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(childMaxMessageEnvVar, tc.val)
			} else {
				os.Unsetenv(childMaxMessageEnvVar)
			}
			if got := childMaxMessageBytes(); got != tc.want {
				t.Errorf("childMaxMessageBytes() = %d, want %d", got, tc.want)
			}
		})
	}
}

// frameMessage marshals a JSON-RPC message the way the stdio transport frames
// it on the wire: one line terminated by a newline.
func frameMessage(t *testing.T, id int64, method string, params any) []byte {
	t.Helper()
	msg, err := jsonrpc.NewRequest(jsonrpc.NewNumberID(id), method, params)
	if err != nil {
		t.Fatalf("building message: %v", err)
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshaling message: %v", err)
	}
	return append(data, '\n')
}

func TestStdioReaderDeliversNormalMessages(t *testing.T) {
	var in bytes.Buffer
	in.Write(frameMessage(t, 1, "ping", nil))
	in.Write(frameMessage(t, 2, "pong", nil))

	r := newStdioReader(&in, io.Discard, nil, stdioReaderOpts{
		server: "srv", ceiling: 1 << 20, drain: 1 << 20,
	})

	for i := 0; i < 2; i++ {
		msg, err := r.Read()
		if err != nil {
			t.Fatalf("Read() #%d error: %v", i, err)
		}
		if msg == nil {
			t.Fatalf("Read() #%d returned nil message", i)
		}
	}
	if _, err := r.Read(); !errors.Is(err, io.EOF) {
		t.Fatalf("Read() after last message = %v, want io.EOF", err)
	}
}

// An oversize line whose newline lands within the bufio buffer takes the
// "complete line" path: it is reported with the exact size and NewlineFound
// true, and the stream resyncs so the following message still reads.
func TestStdioReaderOverflowCompleteLineResync(t *testing.T) {
	var in bytes.Buffer
	big := frameMessage(t, 1, "big", map[string]string{"x": strings.Repeat("A", 200)})
	in.Write(big)
	in.Write(frameMessage(t, 2, "ok", nil))

	r := newStdioReader(&in, io.Discard, nil, stdioReaderOpts{
		server: "srv", ceiling: 64, drain: 64, // big line fits default 64KiB buf
	})

	_, err := r.Read()
	var tooLong *LineTooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("Read() on oversize line = %v, want *LineTooLongError", err)
	}
	if !tooLong.NewlineFound {
		t.Errorf("NewlineFound = false, want true (line was newline-terminated)")
	}
	if tooLong.Bytes != int64(len(big)-1) {
		t.Errorf("Bytes = %d, want %d (line length excluding newline)", tooLong.Bytes, len(big)-1)
	}

	// Stream resynced: the following message must still read.
	msg, err := r.Read()
	if err != nil {
		t.Fatalf("Read() after overflow error: %v (stream did not resync)", err)
	}
	if msg == nil {
		t.Fatal("Read() after overflow returned nil message")
	}
}

// With a tiny read buffer, an oversize line exceeds the buffer before its
// newline, exercising the drain path. A newline within the drain budget still
// yields NewlineFound true and a resynced stream.
func TestStdioReaderOverflowDrainResync(t *testing.T) {
	var in bytes.Buffer
	in.Write(frameMessage(t, 1, "big", map[string]string{"x": strings.Repeat("A", 200)}))
	in.Write(frameMessage(t, 2, "ok", nil))

	// ceiling above a normal message (~38B) but below the big line, so only
	// the big line trips it; readBuf below the big line forces the drain path.
	r := newStdioReader(&in, io.Discard, nil, stdioReaderOpts{
		server: "srv", ceiling: 64, drain: 1024, readBuf: 16,
	})

	_, err := r.Read()
	var tooLong *LineTooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("Read() on oversize line = %v, want *LineTooLongError", err)
	}
	if !tooLong.NewlineFound {
		t.Errorf("NewlineFound = false, want true (newline within drain budget)")
	}

	msg, err := r.Read()
	if err != nil {
		t.Fatalf("Read() after drain error: %v (stream did not resync)", err)
	}
	if msg == nil {
		t.Fatal("Read() after drain returned nil message")
	}
}

// A line that exhausts the drain budget before any newline is unrecoverable:
// NewlineFound is false.
func TestStdioReaderOverflowDrainBudgetExhausted(t *testing.T) {
	var in bytes.Buffer
	// 400 non-newline bytes; ceiling+drain is well below that.
	in.WriteString(strings.Repeat("A", 400))
	in.WriteByte('\n')

	r := newStdioReader(&in, io.Discard, nil, stdioReaderOpts{
		server: "srv", ceiling: 32, drain: 32, readBuf: 16, // budget 64 << 400
	})

	_, err := r.Read()
	var tooLong *LineTooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("Read() = %v, want *LineTooLongError", err)
	}
	if tooLong.NewlineFound {
		t.Errorf("NewlineFound = true, want false (budget exhausted before newline)")
	}
}

func TestStdioReaderSpillWritesFile(t *testing.T) {
	var in bytes.Buffer
	big := frameMessage(t, 1, "big", map[string]string{"x": strings.Repeat("A", 200)})
	in.Write(big)

	r := newStdioReader(&in, io.Discard, nil, stdioReaderOpts{
		server: "spilltest", ceiling: 64, drain: 64, spill: true,
	})

	_, err := r.Read()
	var tooLong *LineTooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("Read() = %v, want *LineTooLongError", err)
	}
	if tooLong.SpillPath == "" {
		t.Fatal("SpillPath empty, want a spill file path")
	}
	t.Cleanup(func() { os.Remove(tooLong.SpillPath) })

	got, err := os.ReadFile(tooLong.SpillPath)
	if err != nil {
		t.Fatalf("reading spill file: %v", err)
	}
	if !bytes.Equal(got, big) {
		t.Errorf("spill file has %d bytes, want the %d-byte line verbatim", len(got), len(big))
	}
}

func TestLineTooLongErrorMessage(t *testing.T) {
	resync := &LineTooLongError{Server: "neb", Ceiling: 1 << 20, Bytes: 5 << 20, NewlineFound: true}
	msg := resync.Error()
	for _, want := range []string{"neb", "MOXY_CHILD_MAX_MESSAGE", "5242880 bytes", "1048576 bytes"} {
		if !strings.Contains(msg, want) {
			t.Errorf("resync error %q missing %q", msg, want)
		}
	}

	spilled := &LineTooLongError{Server: "neb", Ceiling: 1 << 20, Bytes: 5 << 20, NewlineFound: true, SpillPath: "/tmp/x.jsonl"}
	if !strings.Contains(spilled.Error(), "/tmp/x.jsonl") {
		t.Errorf("spilled error %q missing spill path", spilled.Error())
	}

	fatal := &LineTooLongError{Server: "neb", Ceiling: 1 << 20, Bytes: 3 << 20, NewlineFound: false}
	if !strings.Contains(fatal.Error(), "unrecoverable") {
		t.Errorf("fatal error %q should flag unrecoverable stream", fatal.Error())
	}
}

// End-to-end: a LineTooLongError from the transport must surface through
// Call's child-gone path with the byte count and the tuning knob named, so the
// operator can act without digging in logs. Regression for #275.
func TestCallSurfacesOversizeByteCount(t *testing.T) {
	c := &Client{
		name: "nebulous-cg",
		transport: &stubTransport{readErr: &LineTooLongError{
			Server: "nebulous-cg", Ceiling: 1 << 20, Bytes: 5 << 20, NewlineFound: true,
		}},
		pending: make(map[string]chan *jsonrpc.Message),
		done:    make(chan struct{}),
	}
	c.readLoop()

	_, err := c.Call(context.Background(), "resource-templates", nil)
	if err == nil {
		t.Fatal("Call returned nil error, want an oversize child-gone error")
	}
	msg := err.Error()
	for _, want := range []string{"nebulous-cg", "MOXY_CHILD_MAX_MESSAGE", "5242880 bytes", "still running"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
	if strings.Contains(msg, "exited") {
		t.Errorf("error %q should not claim the child exited (it is alive)", msg)
	}
}
