package mcpclient

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"
)

type echoToolProvider struct{}

func (p *echoToolProvider) ListTools(_ context.Context) ([]protocol.Tool, error) {
	return []protocol.Tool{
		{
			Name:        "echo",
			Description: "Returns the input as output",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}}}`),
		},
	}, nil
}

func (p *echoToolProvider) CallTool(_ context.Context, name string, args json.RawMessage) (*protocol.ToolCallResult, error) {
	var input struct {
		Message string `json:"message"`
	}
	json.Unmarshal(args, &input)
	return &protocol.ToolCallResult{
		Content: []protocol.ContentBlock{
			{Type: "text", Text: input.Message},
		},
	}, nil
}

func startTestHTTPServer(t *testing.T) string {
	t.Helper()

	st := transport.NewStreamableHTTP(":0")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	if err := st.Start(ctx); err != nil {
		t.Fatalf("starting test server: %v", err)
	}

	srv, err := server.New(st, server.Options{
		ServerName:    "test-http-server",
		ServerVersion: "0.1.0",
		Tools:         &echoToolProvider{},
	})
	if err != nil {
		t.Fatalf("creating test server: %v", err)
	}

	go srv.Run(ctx)

	return "http://" + st.Addr()
}

func TestHTTPTransportConnectAndInitialize(t *testing.T) {
	addr := startTestHTTPServer(t)

	ctx := context.Background()
	tr := NewHTTPTransport(addr)
	client, result, err := ConnectAndInitialize(ctx, "test", tr)
	if err != nil {
		t.Fatalf("ConnectAndInitialize: %v", err)
	}
	defer client.Close()

	if result.ServerInfo.Name != "test-http-server" {
		t.Errorf("server name: got %q, want %q", result.ServerInfo.Name, "test-http-server")
	}
}

func TestHTTPTransportToolCall(t *testing.T) {
	addr := startTestHTTPServer(t)

	ctx := context.Background()
	tr := NewHTTPTransport(addr)
	client, _, err := ConnectAndInitialize(ctx, "test", tr)
	if err != nil {
		t.Fatalf("ConnectAndInitialize: %v", err)
	}
	defer client.Close()

	// List tools
	raw, err := client.Call(ctx, protocol.MethodToolsList, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}

	var toolsResult struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(raw, &toolsResult); err != nil {
		t.Fatalf("unmarshaling tools: %v", err)
	}
	if len(toolsResult.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(toolsResult.Tools))
	}

	// Call tool
	args, _ := json.Marshal(map[string]string{"message": "hello from http"})
	callParams := protocol.ToolCallParams{
		Name:      "echo",
		Arguments: args,
	}
	raw, err = client.Call(ctx, protocol.MethodToolsCall, callParams)
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}

	var callResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &callResult); err != nil {
		t.Fatalf("unmarshaling call result: %v", err)
	}
	if len(callResult.Content) != 1 || callResult.Content[0].Text != "hello from http" {
		t.Errorf("expected 'hello from http', got %v", callResult.Content)
	}
}

func TestHTTPTransportWithHeaders(t *testing.T) {
	addr := startTestHTTPServer(t)

	ctx := context.Background()
	tr := NewHTTPTransport(addr, WithHeaders(map[string]string{
		"X-Custom": "test-value",
	}))

	client, _, err := ConnectAndInitialize(ctx, "test", tr)
	if err != nil {
		t.Fatalf("ConnectAndInitialize with headers: %v", err)
	}
	defer client.Close()
}

func TestHTTPTransportSessionID(t *testing.T) {
	addr := startTestHTTPServer(t)

	ctx := context.Background()
	tr := NewHTTPTransport(addr)
	client, _, err := ConnectAndInitialize(ctx, "test", tr)
	if err != nil {
		t.Fatalf("ConnectAndInitialize: %v", err)
	}
	defer client.Close()

	sid := tr.SessionID()
	if sid == "" {
		t.Error("expected session ID after initialize")
	}
}

func TestHTTPTransportClose(t *testing.T) {
	addr := startTestHTTPServer(t)

	ctx := context.Background()
	tr := NewHTTPTransport(addr)
	client, _, err := ConnectAndInitialize(ctx, "test", tr)
	if err != nil {
		t.Fatalf("ConnectAndInitialize: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestSSEScanner(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		events []string
	}{
		{
			name:   "single event",
			input:  "data: {\"id\":1}\n\n",
			events: []string{`{"id":1}`},
		},
		{
			name:   "multiple events",
			input:  "data: {\"id\":1}\n\ndata: {\"id\":2}\n\n",
			events: []string{`{"id":1}`, `{"id":2}`},
		},
		{
			name:   "multiline data",
			input:  "data: line1\ndata: line2\n\n",
			events: []string{"line1\nline2"},
		},
		{
			name:   "with id and event fields",
			input:  "id: 1\nevent: message\ndata: hello\n\n",
			events: []string{"hello"},
		},
		{
			name:   "with comments",
			input:  ": comment\ndata: hello\n\n",
			events: []string{"hello"},
		},
		{
			name:   "trailing event without blank line",
			input:  "data: hello",
			events: []string{"hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := newSSEScanner(strings.NewReader(tt.input))
			var got []string
			for scanner.Scan() {
				got = append(got, string(scanner.Data()))
			}
			if scanner.Err() != nil {
				t.Fatalf("scanner error: %v", scanner.Err())
			}
			if len(got) != len(tt.events) {
				t.Fatalf("got %d events, want %d: %v", len(got), len(tt.events), got)
			}
			for i := range got {
				if got[i] != tt.events[i] {
					t.Errorf("event %d: got %q, want %q", i, got[i], tt.events[i])
				}
			}
		})
	}
}
