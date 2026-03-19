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

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"
)

type Client struct {
	name      string
	cmd       *exec.Cmd
	transport *transport.Stdio
	pending   map[string]chan *jsonrpc.Message
	mu        sync.Mutex
	nextID    atomic.Int64
	done      chan struct{}
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
		return nil, nil, fmt.Errorf("starting %s: %w", name, err)
	}

	c := &Client{
		name:      name,
		cmd:       cmd,
		transport: transport.NewStdioWithCloser(stdout, stdin, stdin),
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
				return
			}
			return
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
		// Notifications from child are silently dropped.
	}
}

func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := jsonrpc.NewNumberID(c.nextID.Add(1))

	msg, err := jsonrpc.NewRequest(id, method, params)
	if err != nil {
		return nil, err
	}

	ch := make(chan *jsonrpc.Message, 1)
	c.mu.Lock()
	c.pending[id.String()] = ch
	c.mu.Unlock()

	if err := c.transport.Write(msg); err != nil {
		c.mu.Lock()
		delete(c.pending, id.String())
		c.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id.String())
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		c.mu.Lock()
		delete(c.pending, id.String())
		c.mu.Unlock()
		return nil, fmt.Errorf("child process %s exited unexpectedly", c.name)
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

func (c *Client) Notify(method string, params any) error {
	msg, err := jsonrpc.NewNotification(method, params)
	if err != nil {
		return err
	}
	return c.transport.Write(msg)
}

func (c *Client) Close() error {
	c.transport.Close()
	return c.cmd.Wait()
}

func (c *Client) Name() string {
	return c.name
}
