package streamhttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
)

type slowToolProvider struct {
	delay time.Duration
}

func (s *slowToolProvider) ListTools(_ context.Context) ([]protocol.Tool, error) {
	return nil, nil
}

func (s *slowToolProvider) CallTool(_ context.Context, _ string, _ json.RawMessage) (*protocol.ToolCallResult, error) {
	return nil, nil
}

func (s *slowToolProvider) ListToolsV1(_ context.Context, _ string) (*protocol.ToolsListResultV1, error) {
	return &protocol.ToolsListResultV1{
		Tools: []protocol.ToolV1{{Name: "slow.echo", Description: "slow"}},
	}, nil
}

func (s *slowToolProvider) CallToolV1(ctx context.Context, name string, _ json.RawMessage) (*protocol.ToolCallResultV1, error) {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{protocol.TextContentV1("called " + name)},
	}, nil
}

func newSlowTestServer(delay time.Duration) *Server {
	return New(Options{
		Tools:         &slowToolProvider{delay: delay},
		Resources:     &stubResourceProvider{},
		Prompts:       &stubPromptProvider{},
		ServerName:    "test-moxy",
		ServerVersion: "0.0.0-test",
		Instructions:  "test instructions",
	})
}

func initSession(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	initMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(1)),
		Method:  "initialize",
		Params:  rawJSON(protocol.InitializeParamsV1{ProtocolVersion: protocol.ProtocolVersionV1}),
	}
	body, _ := json.Marshal(initMsg)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	sid := resp.Header.Get(headerMCPSessionID)
	if sid == "" {
		t.Fatal("initialize: empty session id")
	}
	return sid
}

// readAllSSEEvents collects "data:" payloads from an SSE response body
// until EOF. SSE comment lines (lines beginning with ":") are returned
// separately so tests can assert on either.
func readAllSSEEvents(r io.Reader) (events []string, comments []string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "data: "):
			events = append(events, strings.TrimPrefix(line, "data: "))
		case strings.HasPrefix(line, ": "):
			comments = append(comments, strings.TrimPrefix(line, ": "))
		}
	}
	return
}

func TestPostStreaming_HeartbeatEmitsProgress(t *testing.T) {
	t.Setenv(heartbeatEnvVar, "20ms")
	srv := newSlowTestServer(150 * time.Millisecond)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	sid := initSession(t, ts)

	params, _ := json.Marshal(map[string]any{
		"name": "slow.echo",
		"_meta": map[string]any{
			"progressToken": "tok-123",
		},
	})
	callMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(2)),
		Method:  "tools/call",
		Params:  params,
	}
	body, _ := json.Marshal(callMsg)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerMCPSessionID, sid)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	events, _ := readAllSSEEvents(resp.Body)
	if len(events) < 2 {
		t.Fatalf("want at least 2 SSE data events (>=1 progress + 1 response), got %d: %v", len(events), events)
	}

	var progressCount int
	var sawResponse bool
	for _, ev := range events {
		var probe jsonrpc.Message
		if err := json.Unmarshal([]byte(ev), &probe); err != nil {
			t.Errorf("event not valid JSON-RPC: %q (%v)", ev, err)
			continue
		}
		if probe.Method == "notifications/progress" {
			progressCount++
			if !bytes.Contains(probe.Params, []byte(`"tok-123"`)) {
				t.Errorf("progress notification missing token: %s", string(probe.Params))
			}
		}
		if probe.Result != nil && probe.ID != nil {
			sawResponse = true
		}
	}
	if progressCount == 0 {
		t.Errorf("expected at least 1 notifications/progress event, got 0")
	}
	if !sawResponse {
		t.Errorf("expected a tools/call response event, got none")
	}
}

func TestPostStreaming_HeartbeatCommentWhenNoToken(t *testing.T) {
	t.Setenv(heartbeatEnvVar, "20ms")
	srv := newSlowTestServer(150 * time.Millisecond)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	sid := initSession(t, ts)

	params, _ := json.Marshal(map[string]any{"name": "slow.echo"})
	callMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(2)),
		Method:  "tools/call",
		Params:  params,
	}
	body, _ := json.Marshal(callMsg)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerMCPSessionID, sid)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	events, comments := readAllSSEEvents(resp.Body)
	if len(comments) == 0 {
		t.Errorf("expected at least one SSE comment heartbeat, got none. events=%v", events)
	}
	for _, ev := range events {
		var probe jsonrpc.Message
		if err := json.Unmarshal([]byte(ev), &probe); err != nil {
			t.Errorf("event not valid JSON-RPC: %q (%v)", ev, err)
			continue
		}
		if probe.Method == "notifications/progress" {
			t.Errorf("did not expect notifications/progress without progressToken, got: %s", ev)
		}
	}
	// At least one event should be the response itself.
	if len(events) == 0 {
		t.Fatal("expected at least the tools/call response event, got none")
	}
}

func TestPostStreaming_DisabledFallsBackToJSON(t *testing.T) {
	t.Setenv(heartbeatEnvVar, "0")
	srv := newSlowTestServer(0)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	sid := initSession(t, ts)

	params, _ := json.Marshal(map[string]any{"name": "slow.echo"})
	callMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(2)),
		Method:  "tools/call",
		Params:  params,
	}
	body, _ := json.Marshal(callMsg)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerMCPSessionID, sid)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json (heartbeat disabled)", got)
	}
	var respMsg jsonrpc.Message
	if err := json.NewDecoder(resp.Body).Decode(&respMsg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if respMsg.Result == nil {
		t.Errorf("expected non-nil Result in fallback JSON response, got error=%v", respMsg.Error)
	}
}

func TestPostStreaming_ContextCancelStopsHeartbeat(t *testing.T) {
	t.Setenv(heartbeatEnvVar, "20ms")
	srv := newSlowTestServer(2 * time.Second)
	ts := httptest.NewServer(srv)
	defer ts.Close()
	sid := initSession(t, ts)

	params, _ := json.Marshal(map[string]any{"name": "slow.echo"})
	callMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(2)),
		Method:  "tools/call",
		Params:  params,
	}
	body, _ := json.Marshal(callMsg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerMCPSessionID, sid)

	resp, err := ts.Client().Do(req)
	if err == nil {
		// Some clients return a partial body before the context fires.
		// Reading should fail or hit EOF shortly.
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	// After cancellation, the test server must still be live and responsive.
	// Issue a fresh tools/list to confirm.
	listMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(3)),
		Method:  "tools/list",
	}
	listBody, _ := json.Marshal(listMsg)
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(listBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set(headerMCPSessionID, sid)
	resp2, err := ts.Client().Do(req2)
	if err != nil {
		t.Fatalf("post-cancel tools/list: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("post-cancel tools/list: want 200, got %d", resp2.StatusCode)
	}
}

func TestExtractProgressToken(t *testing.T) {
	cases := []struct {
		name   string
		params string
		want   string
	}{
		{"missing", `{}`, ""},
		{"no_meta", `{"name":"x"}`, ""},
		{"empty_meta", `{"_meta":{}}`, ""},
		{"null_token", `{"_meta":{"progressToken":null}}`, ""},
		{"string_token", `{"_meta":{"progressToken":"abc"}}`, `"abc"`},
		{"int_token", `{"_meta":{"progressToken":42}}`, `42`},
		{"invalid_json", `{not json`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractProgressToken(json.RawMessage(tc.params))
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", string(got), tc.want)
			}
		})
	}
}

func TestHeartbeatInterval(t *testing.T) {
	cases := []struct {
		name string
		env  string
		set  bool
		want time.Duration
	}{
		{"unset", "", false, heartbeatDefault},
		{"zero", "0", true, 0},
		{"off", "off", true, 0},
		{"empty", "", true, 0},
		{"5s", "5s", true, 5 * time.Second},
		{"100ms", "100ms", true, 100 * time.Millisecond},
		{"invalid", "garbage", true, heartbeatDefault},
		{"negative", "-5s", true, heartbeatDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(heartbeatEnvVar, tc.env)
			} else {
				oldVal, oldSet := os.LookupEnv(heartbeatEnvVar)
				_ = os.Unsetenv(heartbeatEnvVar)
				t.Cleanup(func() {
					if oldSet {
						_ = os.Setenv(heartbeatEnvVar, oldVal)
					} else {
						_ = os.Unsetenv(heartbeatEnvVar)
					}
				})
			}
			got := heartbeatInterval()
			if got != tc.want {
				t.Errorf("interval = %v, want %v", got, tc.want)
			}
		})
	}
}
