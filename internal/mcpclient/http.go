package mcpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"
)

// headerMCPSessionID is the MCP session header name (inlined since
// go-mcp removed its HTTP transport in v0.0.5+).
const headerMCPSessionID = "Mcp-Session-Id"

// HTTPTransport implements transport.Transport for MCP Streamable HTTP.
// It POSTs JSON-RPC messages to a remote server and reads responses
// from the HTTP response body (plain JSON or SSE).
type HTTPTransport struct {
	url       string
	client    *http.Client
	headers   map[string]string
	sessionID string
	incoming  chan *jsonrpc.Message
	closed    chan struct{}
	mu        sync.Mutex
}

var _ transport.Transport = (*HTTPTransport)(nil)

// HTTPTransportOption configures an HTTPTransport.
type HTTPTransportOption func(*HTTPTransport)

// WithHeaders sets static headers sent with every request.
func WithHeaders(headers map[string]string) HTTPTransportOption {
	return func(t *HTTPTransport) {
		for k, v := range headers {
			t.headers[k] = v
		}
	}
}

// WithBearerToken sets an Authorization: Bearer header.
func WithBearerToken(token string) HTTPTransportOption {
	return func(t *HTTPTransport) {
		t.headers["Authorization"] = "Bearer " + token
	}
}

// NewHTTPTransport creates a new HTTP client transport for the given URL.
func NewHTTPTransport(url string, opts ...HTTPTransportOption) *HTTPTransport {
	t := &HTTPTransport{
		url:      url,
		client:   &http.Client{},
		headers:  make(map[string]string),
		incoming: make(chan *jsonrpc.Message, 64),
		closed:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Write sends a JSON-RPC message to the server via HTTP POST.
// For requests (messages with an ID), it reads the response and enqueues it.
// For notifications, it accepts a 202 response.
func (t *HTTPTransport) Write(msg *jsonrpc.Message) error {
	select {
	case <-t.closed:
		return fmt.Errorf("transport closed")
	default:
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	t.mu.Lock()
	sessionID := t.sessionID
	t.mu.Unlock()

	if sessionID != "" {
		req.Header.Set(headerMCPSessionID, sessionID)
	}

	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	// Save session ID from initialize response
	if sid := resp.Header.Get(headerMCPSessionID); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}

	// Notifications get 202 with no body
	if resp.StatusCode == http.StatusAccepted {
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	contentType := resp.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "text/event-stream") {
		return t.readSSEResponse(resp.Body)
	}

	// Plain JSON response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	var respMsg jsonrpc.Message
	if err := json.Unmarshal(body, &respMsg); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	t.incoming <- &respMsg
	return nil
}

// readSSEResponse parses SSE events from the response body and enqueues messages.
func (t *HTTPTransport) readSSEResponse(body io.Reader) error {
	scanner := newSSEScanner(body)
	for scanner.Scan() {
		data := scanner.Data()
		if len(data) == 0 {
			continue
		}

		var msg jsonrpc.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		t.incoming <- &msg
	}
	return scanner.Err()
}

// Read returns the next message received from the server.
func (t *HTTPTransport) Read() (*jsonrpc.Message, error) {
	select {
	case msg := <-t.incoming:
		return msg, nil
	case <-t.closed:
		return nil, io.EOF
	}
}

// Close closes the transport and sends a DELETE to terminate the session.
func (t *HTTPTransport) Close() error {
	select {
	case <-t.closed:
		return nil
	default:
		close(t.closed)
	}

	t.mu.Lock()
	sessionID := t.sessionID
	t.mu.Unlock()

	if sessionID != "" {
		req, err := http.NewRequest(http.MethodDelete, t.url, nil)
		if err == nil {
			req.Header.Set(headerMCPSessionID, sessionID)
			for k, v := range t.headers {
				req.Header.Set(k, v)
			}
			resp, err := t.client.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}
	}

	return nil
}

// SessionID returns the current session ID, if any.
func (t *HTTPTransport) SessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}
