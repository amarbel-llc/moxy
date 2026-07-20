package mcpclient

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/transport"
)

// The MCP stdio transport frames each JSON-RPC message as one newline-delimited
// line (see the MCP "Transports" spec: messages "MUST NOT contain embedded
// newlines"). Neither the MCP spec nor JSON-RPC 2.0 defines a maximum message
// size, so a ceiling here is necessarily a moxy policy choice rather than a
// spec constant — which is why it is operator-tunable.
//
// stdioReader is moxy's own implementation of transport.Transport for stdio
// children. It replaces go-mcp's transport.Stdio (whose bufio.Scanner buffer
// cap is baked in with no setter) so moxy can (a) set the ceiling from the
// environment, (b) report the offending line's true size for tuning, and (c)
// drain past the ceiling to the next newline so the stream resyncs instead of
// wedging the child forever. See #275.

const (
	// childMaxMessageEnvVar caps the size of a single newline-delimited JSON
	// message moxy will buffer from a child's stdout before treating the line
	// as an oversize fault. Unset uses childMaxMessageDefault. Accepts a raw
	// byte count ("67108864") or a value with a unit suffix ("64MB", "256MiB",
	// "1G"); an unparseable or non-positive value falls back to the default.
	childMaxMessageEnvVar = "MOXY_CHILD_MAX_MESSAGE"

	// childSpillEnvVar, when truthy ("1"/"true"/"yes"/"on"), makes the reader
	// spill an oversize line to a temp file while draining it, for post-mortem
	// and as a workaround to recover a legitimately-huge payload. Off by
	// default so a misbehaving child cannot fill the spool.
	childSpillEnvVar = "MOXY_CHILD_SPILL_OVERSIZE"

	// childMaxMessageDefault is the deliver ceiling used when the env var is
	// unset: comfortably above realistic large responses (single-digit-to-tens
	// of MB) yet far below host-OOM territory.
	childMaxMessageDefault int64 = 64 << 20 // 64 MiB

	// stdioReadBufSize is the bufio read-buffer size. Lines longer than this
	// are handled by fragment accumulation in readOneLine; it only affects
	// syscall batching, not the size limit.
	stdioReadBufSize = 64 << 10 // 64 KiB
)

var _ transport.Transport = (*stdioReader)(nil)

// childMaxMessageBytes resolves the deliver ceiling (C1) from the environment,
// falling back to the default on unset/invalid input. Mirrors the lenient
// parse-or-default shape of streamhttp.heartbeatInterval.
func childMaxMessageBytes() int64 {
	v, set := os.LookupEnv(childMaxMessageEnvVar)
	if !set {
		return childMaxMessageDefault
	}
	n, err := parseSize(v)
	if err != nil || n <= 0 {
		return childMaxMessageDefault
	}
	return n
}

// childSpillOversize reports whether oversize lines should be spilled to disk.
func childSpillOversize() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(childSpillEnvVar))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// parseSize parses a byte-size string: a bare integer (bytes), or an integer
// with a unit suffix K/M/G/T optionally followed by "i" and/or "B" (so "M",
// "MB", and "MiB" are equivalent). Multipliers are binary (1024-based); the
// MB/MiB distinction is immaterial for a wire-buffer ceiling.
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("size %q has no numeric part", s)
	}
	n, err := strconv.ParseInt(s[:i], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing size %q: %w", s, err)
	}
	unit := strings.ToLower(strings.TrimSpace(s[i:]))
	unit = strings.TrimSuffix(unit, "b")
	unit = strings.TrimSuffix(unit, "i")
	var mult int64
	switch unit {
	case "":
		mult = 1
	case "k":
		mult = 1 << 10
	case "m":
		mult = 1 << 20
	case "g":
		mult = 1 << 30
	case "t":
		mult = 1 << 40
	default:
		return 0, fmt.Errorf("size %q has unknown unit %q", s, unit)
	}
	if mult > 1 && n > (1<<63-1)/mult {
		return 0, fmt.Errorf("size %q overflows int64", s)
	}
	return n * mult, nil
}

// LineTooLongError is returned by the stdio reader when a child emits a single
// newline-delimited message larger than the configured ceiling
// (MOXY_CHILD_MAX_MESSAGE). It carries the offending line's size so operators
// can tune the ceiling from the reported number. The child process is still
// alive — only its oversize message was refused. See #275.
type LineTooLongError struct {
	Server  string // child server name
	Ceiling int64  // the deliver ceiling that was exceeded, in bytes
	Bytes   int64  // line size read: exact content length when
	// NewlineFound, else the drain budget consumed (a
	// lower bound on the true size)
	NewlineFound bool // whether the terminating newline was reached within
	// the drain budget — if so the stream is resynced to
	// the next message; if not the stream is unrecoverable
	SpillPath string // temp file the oversize line was written to, "" if not spilled
}

func (e *LineTooLongError) Error() string {
	if e.NewlineFound {
		msg := fmt.Sprintf(
			"child %s sent a %s stdout message exceeding the %s ceiling (MOXY_CHILD_MAX_MESSAGE)",
			e.Server, humanizeBytes(e.Bytes), humanizeBytes(e.Ceiling),
		)
		if e.SpillPath != "" {
			msg += fmt.Sprintf("; spilled to %s", e.SpillPath)
		}
		return msg
	}
	return fmt.Sprintf(
		"child %s stdout message exceeded the %s ceiling (MOXY_CHILD_MAX_MESSAGE) with no newline after %s — stream unrecoverable",
		e.Server, humanizeBytes(e.Ceiling), humanizeBytes(e.Bytes),
	)
}

// humanizeBytes renders a byte count as "<human> (<n> bytes)" so the message
// is both readable and exact for tuning.
func humanizeBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d bytes", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB (%d bytes)", float64(n)/float64(div), "KMGT"[exp], n)
}

// stdioReaderOpts configures a stdioReader.
type stdioReaderOpts struct {
	server  string
	ceiling int64 // C1: max size delivered
	drain   int64 // C2: extra bytes read past the ceiling hunting for the
	// terminating newline (to resync + report true size)
	spill   bool
	readBuf int // bufio read-buffer size; 0 uses stdioReadBufSize. Test seam.
}

type stdioReader struct {
	server  string
	br      *bufio.Reader
	writer  io.Writer
	closer  io.Closer
	ceiling int64
	drain   int64
	spill   bool
	mu      sync.Mutex // serializes Write, mirroring go-mcp's Stdio
}

// newStdioReader builds a bounded stdio transport reading from r, writing to w,
// closing c on Close.
func newStdioReader(r io.Reader, w io.Writer, c io.Closer, opts stdioReaderOpts) *stdioReader {
	bufSize := opts.readBuf
	if bufSize <= 0 {
		bufSize = stdioReadBufSize
	}
	return &stdioReader{
		server:  opts.server,
		br:      bufio.NewReaderSize(r, bufSize),
		writer:  w,
		closer:  c,
		ceiling: opts.ceiling,
		drain:   opts.drain,
		spill:   opts.spill,
	}
}

// Read returns the next JSON-RPC message, or io.EOF at a clean end of stream,
// or *LineTooLongError when a line exceeds the ceiling.
func (t *stdioReader) Read() (*jsonrpc.Message, error) {
	line, err := t.readLine()
	if err != nil {
		return nil, err
	}
	var msg jsonrpc.Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, fmt.Errorf("parsing message: %w", err)
	}
	return &msg, nil
}

// readLine returns the next non-empty line (newline stripped), skipping blank
// lines to mirror go-mcp's Stdio.
func (t *stdioReader) readLine() ([]byte, error) {
	for {
		line, err := t.readOneLine()
		if err != nil {
			return nil, err
		}
		if len(line) == 0 {
			continue
		}
		return line, nil
	}
}

// readOneLine reads up to and including the next '\n' (which is stripped),
// enforcing the deliver ceiling. Returns io.EOF at a clean end of stream, a
// read error, or *LineTooLongError on overflow. A final unterminated line at
// EOF is returned as-is (Read's json.Unmarshal rejects a truncated fragment).
func (t *stdioReader) readOneLine() ([]byte, error) {
	var line []byte
	for {
		frag, err := t.br.ReadSlice('\n')
		switch {
		case err == nil:
			line = append(line, frag...)
			// Full line in hand (including the trailing newline, which
			// ReadSlice already consumed, so the stream is resynced).
			if int64(len(line)-1) > t.ceiling {
				return nil, t.tooLongComplete(line)
			}
			return line[:len(line)-1], nil // strip trailing '\n'
		case errors.Is(err, bufio.ErrBufferFull):
			line = append(line, frag...)
			if int64(len(line)) > t.ceiling {
				return nil, t.overflow(line)
			}
		default: // io.EOF or a read error
			line = append(line, frag...)
			if len(line) == 0 {
				return nil, err
			}
			if int64(len(line)) > t.ceiling {
				return nil, t.overflow(line)
			}
			return line, nil // final unterminated line; EOF surfaces next call
		}
	}
}

// overflow handles a line already past the ceiling. seen is the bytes read so
// far. It drains the remainder up to the drain budget — discarding (or
// spilling) as it counts — so the returned error carries the true size and the
// stream is left resynced at the next newline when one is found within budget.
// Peak memory stays bounded by the ceiling: the drained bytes are never
// retained.
func (t *stdioReader) overflow(seen []byte) error {
	total := int64(len(seen))
	budget := t.ceiling + t.drain

	var spillFile *os.File
	if t.spill {
		if spillFile = t.openSpill(); spillFile != nil {
			defer func() { _ = spillFile.Close() }()
			_, _ = spillFile.Write(seen)
		}
	}

	for total < budget {
		frag, err := t.br.ReadSlice('\n')
		total += int64(len(frag))
		if spillFile != nil {
			_, _ = spillFile.Write(frag)
		}
		switch {
		case err == nil:
			// Terminating newline found: stream resynced, true size known
			// (exclude the newline byte from the reported content length).
			return t.tooLong(total-1, true, spillFile)
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default: // io.EOF or read error before a newline within budget
			return t.tooLong(total, false, spillFile)
		}
	}
	// Drain budget exhausted without a newline: stream unrecoverable.
	return t.tooLong(total, false, spillFile)
}

// tooLongComplete handles an oversize line whose terminating newline was
// already consumed by a single ReadSlice, so the full line is in memory and no
// draining is needed. line includes the trailing newline.
func (t *stdioReader) tooLongComplete(line []byte) *LineTooLongError {
	var spillFile *os.File
	if t.spill {
		if spillFile = t.openSpill(); spillFile != nil {
			_, _ = spillFile.Write(line)
			_ = spillFile.Close()
		}
	}
	return t.tooLong(int64(len(line)-1), true, spillFile) // -1 excludes newline
}

func (t *stdioReader) tooLong(size int64, newlineFound bool, spill *os.File) *LineTooLongError {
	e := &LineTooLongError{
		Server:       t.server,
		Ceiling:      t.ceiling,
		Bytes:        size,
		NewlineFound: newlineFound,
	}
	if spill != nil {
		e.SpillPath = spill.Name()
	}
	return e
}

// openSpill creates a temp file for an oversize line, or returns nil (spilling
// is best-effort — a failure here must not mask the overflow error).
func (t *stdioReader) openSpill() *os.File {
	dir := filepath.Join(os.TempDir(), "moxy", "oversize")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	f, err := os.CreateTemp(dir, fmt.Sprintf("%s-*.jsonl", t.server))
	if err != nil {
		return nil
	}
	return f
}

// Write sends a newline-delimited JSON message. Mirrors go-mcp's Stdio.Write.
func (t *stdioReader) Write(msg *jsonrpc.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, err := fmt.Fprintf(t.writer, "%s\n", data); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}
	return nil
}

// Close closes the underlying closer, if any.
func (t *stdioReader) Close() error {
	if t.closer != nil {
		return t.closer.Close()
	}
	return nil
}
