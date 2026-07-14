package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/amarbel-llc/moxy/internal/lifecyclelog"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"
)

// logLifecycle forwards to the shared lifecyclelog package. Kept as a local
// alias so existing call sites don't churn and the message format stays
// grep-compatible with pre-c0f7e4f lifecycle.log lines.
func logLifecycle(format string, args ...any) {
	lifecyclelog.Log(format, args...)
}

type Client struct {
	name           string
	cmd            *exec.Cmd // nil for HTTP servers
	transport      transport.Transport
	pending        map[string]chan *jsonrpc.Message
	mu             sync.Mutex
	nextID         atomic.Int64
	done           chan struct{}
	onNotification func(*jsonrpc.Message)

	startedAt time.Time
	pid       int
	waitOnce  sync.Once
	waitErr   error

	// readErr records the error that terminated readLoop (io.EOF for a clean
	// stdout close, i.e. child exit; a non-EOF error for a framing/pipe fault
	// where the child may still be running). Written once in readLoop before
	// close(c.done); safe to read after observing c.done closed, since the
	// channel close synchronizes-after the write.
	readErr error
}

func (c *Client) SetOnNotification(fn func(*jsonrpc.Message)) {
	c.onNotification = fn
}

func SpawnAndInitialize(ctx context.Context, name, command string, args []string) (*Client, *protocol.InitializeResultV1, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("creating stdin pipe for %s: %w", name, err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("creating stdout pipe for %s: %w", name, err)
	}

	if err := cmd.Start(); err != nil {
		logLifecycle("spawn FAIL %s: %v", name, err)
		return nil, nil, fmt.Errorf("starting %s: %w", name, err)
	}
	logLifecycle("spawn OK %s pid=%d cmd=%s", name, cmd.Process.Pid, command)

	c := &Client{
		name: name,
		cmd:  cmd,
		transport: newStdioReader(stdout, stdin, stdin, stdioReaderOpts{
			server:  name,
			ceiling: childMaxMessageBytes(),
			// Drain up to one extra ceiling past the limit hunting for the
			// terminating newline: enough to report the true size and resync
			// the stream for a merely-large line, while still capping a child
			// that streams with no newline at all (bounded memory either way).
			drain: childMaxMessageBytes(),
			spill: childSpillOversize(),
		}),
		pending:   make(map[string]chan *jsonrpc.Message),
		done:      make(chan struct{}),
		startedAt: time.Now(),
		pid:       cmd.Process.Pid,
	}

	go c.readLoop()

	result, err := c.initialize(ctx)
	if err != nil {
		logLifecycle("initialize FAIL %s: %v", name, err)
		c.Close()
		return nil, nil, fmt.Errorf("initializing %s: %w", name, err)
	}
	logLifecycle("initialize OK %s server=%s version=%s", name, result.ServerInfo.Name, result.ServerInfo.Version)

	return c, result, nil
}

// ConnectAndInitialize creates a Client that connects to a remote HTTP MCP server.
func ConnectAndInitialize(ctx context.Context, name string, t transport.Transport) (*Client, *protocol.InitializeResultV1, error) {
	c := &Client{
		name:      name,
		transport: t,
		pending:   make(map[string]chan *jsonrpc.Message),
		done:      make(chan struct{}),
	}

	go c.readLoop()

	result, err := c.initialize(ctx)
	if err != nil {
		c.Close()
		return nil, nil, fmt.Errorf("initializing %s: %w", name, err)
	}

	return c, result, nil
}

func (c *Client) initialize(ctx context.Context) (*protocol.InitializeResultV1, error) {
	params := protocol.InitializeParamsV1{
		ProtocolVersion: protocol.ProtocolVersionV1,
		Capabilities:    protocol.ClientCapabilitiesV1{},
		ClientInfo: protocol.ImplementationV1{
			Name:    "moxy",
			Version: "0.1.0",
		},
	}

	raw, err := c.Call(ctx, protocol.MethodInitialize, params)
	if err != nil {
		return nil, fmt.Errorf("initialize call: %w", err)
	}

	var result protocol.InitializeResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding initialize result: %w", err)
	}

	if err := c.Notify(protocol.MethodInitialized, nil); err != nil {
		return nil, fmt.Errorf("sending initialized notification: %w", err)
	}

	return &result, nil
}

func (c *Client) readLoop() {
	defer close(c.done)

	for {
		msg, err := c.transport.Read()
		if err != nil {
			var tooLong *LineTooLongError
			// A recoverable oversize line is NON-fatal: the reader drained
			// past it to the next message boundary, so the child is still
			// alive and the stream is back in frame. Keep serving — the one
			// pending Call awaiting the dropped (uncorrelatable, we never
			// parsed its id) response fails on its own context deadline. Only
			// an unrecoverable oversize (no newline within the drain budget)
			// or any other read error is terminal. See #275.
			if errors.As(err, &tooLong) && tooLong.NewlineFound {
				logLifecycle(
					"readLoop OVERSIZE(recovered) %s bytes=%d ceiling=%d spill=%q",
					c.name, tooLong.Bytes, tooLong.Ceiling, tooLong.SpillPath,
				)
				continue
			}
			// Terminal: record why before close(c.done) unblocks any pending
			// Call, so the child-gone error can distinguish a clean exit (EOF)
			// from a framing fault or an unrecoverable oversize line.
			c.readErr = err
			switch {
			case errors.Is(err, io.EOF):
				logLifecycle("readLoop EOF %s (child exited)", c.name)
			case errors.As(err, &tooLong): // NewlineFound == false
				logLifecycle(
					"readLoop OVERSIZE %s bytes=%d ceiling=%d spill=%q (unrecoverable)",
					c.name, tooLong.Bytes, tooLong.Ceiling, tooLong.SpillPath,
				)
			default:
				logLifecycle("readLoop ERROR %s: %v", c.name, err)
			}
			// Reap the child so we can log exit code/signal. Run in a
			// goroutine: Wait() can block on zombie reaping edge cases
			// and readLoop's defer close(c.done) should fire promptly to
			// unblock any pending Call().
			if c.cmd != nil {
				go c.reapChild()
			}
			return
		}

		if msg.IsNotification() {
			if c.onNotification != nil {
				c.onNotification(msg)
			}
			continue
		}

		if msg.IsResponse() {
			c.mu.Lock()
			ch, ok := c.pending[msg.ID.String()]
			if ok {
				delete(c.pending, msg.ID.String())
			}
			c.mu.Unlock()

			if ok {
				ch <- msg
				close(ch)
			}
		}
	}
}

// reapChild waits for the child process once and logs its exit status
// (pid, code, signal, wall-time-since-spawn). Safe to call multiple times
// and from multiple goroutines; only the first call performs cmd.Wait().
func (c *Client) reapChild() error {
	c.waitOnce.Do(func() {
		if c.cmd == nil {
			return
		}
		c.waitErr = c.cmd.Wait()
		dur := time.Since(c.startedAt)
		ps := c.cmd.ProcessState
		code := -1
		signalStr := "none"
		if ps != nil {
			code = ps.ExitCode()
			if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				signalStr = ws.Signal().String()
			}
		}
		logLifecycle(
			"exit %s pid=%d code=%d signal=%s dur=%s err=%v",
			c.name, c.pid, code, signalStr, dur, c.waitErr,
		)
	})
	return c.waitErr
}

func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	start := time.Now()
	id := jsonrpc.NewNumberID(c.nextID.Add(1))
	idStr := id.String()
	logLifecycle("call START %s method=%s id=%s", c.name, method, idStr)

	msg, err := jsonrpc.NewRequest(id, method, params)
	if err != nil {
		logLifecycle("call BUILD_ERR %s method=%s id=%s err=%v", c.name, method, idStr, err)
		return nil, err
	}

	ch := make(chan *jsonrpc.Message, 1)
	c.mu.Lock()
	c.pending[idStr] = ch
	c.mu.Unlock()

	if err := c.transport.Write(msg); err != nil {
		c.mu.Lock()
		delete(c.pending, idStr)
		c.mu.Unlock()
		logLifecycle("call WRITE_ERR %s method=%s id=%s dur=%s err=%v",
			c.name, method, idStr, time.Since(start), err)
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, idStr)
		c.mu.Unlock()
		logLifecycle("call CANCEL %s method=%s id=%s dur=%s err=%v",
			c.name, method, idStr, time.Since(start), ctx.Err())
		return nil, ctx.Err()
	case <-c.done:
		c.mu.Lock()
		delete(c.pending, idStr)
		c.mu.Unlock()
		logLifecycle("call CHILDGONE %s method=%s id=%s dur=%s",
			c.name, method, idStr, time.Since(start))
		return nil, c.childGoneError()
	case resp := <-ch:
		if resp.Error != nil {
			logLifecycle("call ERR %s method=%s id=%s dur=%s err=%v",
				c.name, method, idStr, time.Since(start), resp.Error)
			return nil, resp.Error
		}
		logLifecycle("call DONE %s method=%s id=%s dur=%s",
			c.name, method, idStr, time.Since(start))
		return resp.Result, nil
	}
}

// childGoneError builds the error a Call returns when it finds c.done closed.
// It reflects why readLoop terminated so the message is self-diagnosing: a
// clean stdout EOF is a real child exit, while a non-EOF read error is a
// framing/pipe fault where the child process may still be running. The numeric
// exit code/signal, when the child is reaped, stays in lifecycle.log's
// "exit ... code=... signal=..." line. See #275.
func (c *Client) childGoneError() error {
	var tooLong *LineTooLongError
	switch {
	case c.readErr == nil || errors.Is(c.readErr, io.EOF):
		return fmt.Errorf(
			"child process %s exited (EOF on stdout; see lifecycle.log for exit code/signal)",
			c.name,
		)
	case errors.As(c.readErr, &tooLong):
		// An unrecoverable oversize line (no newline within the drain budget);
		// a recoverable one never reaches here — readLoop skips it and keeps
		// serving. Wrap with %w so the proxy dispatch can detect the type and
		// respawn the child. Name the knob so the fix is self-evident too.
		return fmt.Errorf(
			"child process %s: %w (raise MOXY_CHILD_MAX_MESSAGE or restart the child)",
			c.name, c.readErr,
		)
	default:
		return fmt.Errorf(
			"child process %s stdout read error: %v — process may still be running (see lifecycle.log)",
			c.name, c.readErr,
		)
	}
}

func (c *Client) Notify(method string, params any) error {
	msg, err := jsonrpc.NewNotification(method, params)
	if err != nil {
		return err
	}
	if err := c.transport.Write(msg); err != nil {
		logLifecycle("notify WRITE_ERR %s method=%s err=%v", c.name, method, err)
		return err
	}
	return nil
}

func (c *Client) Close() error {
	logLifecycle("close %s", c.name)
	c.transport.Close()
	if c.cmd != nil {
		return c.reapChild()
	}
	return nil
}

func (c *Client) Name() string {
	return c.name
}
