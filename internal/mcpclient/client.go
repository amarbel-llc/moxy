package mcpclient

import (
	"context"
	"encoding/json"
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
		name:      name,
		cmd:       cmd,
		transport: transport.NewStdioWithCloser(stdout, stdin, stdin),
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
			if err == io.EOF {
				logLifecycle("readLoop EOF %s (child exited)", c.name)
			} else {
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
		return nil, fmt.Errorf("child process %s exited unexpectedly", c.name)
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
