package streamhttp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
)

func initializeBody(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(1)),
		Method:  "initialize",
		Params:  rawJSON(protocol.InitializeParamsV1{ProtocolVersion: protocol.ProtocolVersionV1}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type httpAnswer struct {
	status  int
	session string
	body    []byte
	err     error
}

// askAsync POSTs body to /mcp over a real TCP connection and reports the
// answer on the returned channel.
func askAsync(url string, body []byte, sessionID string) <-chan httpAnswer {
	out := make(chan httpAnswer, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, url+"/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if sessionID != "" {
			req.Header.Set(headerMCPSessionID, sessionID)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			out <- httpAnswer{err: err}
			return
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		out <- httpAnswer{status: resp.StatusCode, session: resp.Header.Get(headerMCPSessionID), body: data}
	}()
	return out
}

func TestStartupGateHealthzReportsStartingThenOK(t *testing.T) {
	gate := NewStartupGate(time.Minute)
	ts := httptest.NewServer(gate)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz before ready: connection failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("healthz before ready: want 503, got %d", resp.StatusCode)
	}
	if string(bytes.TrimSpace(body)) != `{"status":"starting"}` {
		t.Errorf("healthz before ready: body = %q", body)
	}

	gate.Ready(newTestServer())

	resp, err = http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz after ready: want 200, got %d", resp.StatusCode)
	}
}

func TestStartupGateHoldsInitializeUntilReadyThenServesCompleteInstructions(t *testing.T) {
	t.Setenv("MOXY_HEARTBEAT_INTERVAL", "0")
	gate := NewStartupGate(time.Minute)
	ts := httptest.NewServer(gate)
	defer ts.Close()

	// The connection is accepted and the request is in flight, not refused.
	pending := askAsync(ts.URL, initializeBody(t), "")
	select {
	case got := <-pending:
		t.Fatalf("initialize answered before ready: %+v", got)
	case <-time.After(150 * time.Millisecond):
	}

	gate.Ready(newTestServer())

	var got httpAnswer
	select {
	case got = <-pending:
	case <-time.After(5 * time.Second):
		t.Fatal("held initialize was not released by Ready")
	}
	if got.err != nil || got.status != http.StatusOK {
		t.Fatalf("held initialize: status=%d err=%v body=%s", got.status, got.err, got.body)
	}
	if got.session == "" {
		t.Error("held initialize: no Mcp-Session-Id")
	}
	var msg jsonrpc.Message
	if err := json.Unmarshal(got.body, &msg); err != nil {
		t.Fatalf("held initialize: bad JSON: %v", err)
	}
	var result protocol.InitializeResultV1
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		t.Fatalf("held initialize: decode: %v", err)
	}
	if result.Instructions != "test instructions" {
		t.Errorf("instructions = %q, want the complete post-startup text", result.Instructions)
	}

	// After readiness, tools/list under the minted session is complete.
	toolsBody, _ := json.Marshal(jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(2)),
		Method:  "tools/list",
	})
	tools := <-askAsync(ts.URL, toolsBody, got.session)
	if tools.err != nil || tools.status != http.StatusOK {
		t.Fatalf("tools/list: status=%d err=%v body=%s", tools.status, tools.err, tools.body)
	}
	var toolsMsg jsonrpc.Message
	if err := json.Unmarshal(tools.body, &toolsMsg); err != nil {
		t.Fatal(err)
	}
	var list protocol.ToolsListResultV1
	if err := json.Unmarshal(toolsMsg.Result, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 1 || list.Tools[0].Name != "test.echo" {
		t.Errorf("tools/list after ready = %+v, want [test.echo]", list.Tools)
	}
}

func TestStartupGateHoldTimeoutIsWellFormed503(t *testing.T) {
	gate := NewStartupGate(100 * time.Millisecond)
	ts := httptest.NewServer(gate)
	defer ts.Close()

	got := <-askAsync(ts.URL, initializeBody(t), "")
	if got.err != nil {
		t.Fatalf("held request failed at the connection level: %v", got.err)
	}
	if got.status != http.StatusServiceUnavailable {
		t.Fatalf("want 503 after the hold cap, got %d", got.status)
	}
}

func TestStartupGateReadyIsIdempotent(t *testing.T) {
	gate := NewStartupGate(time.Minute)
	first := newTestServer()
	gate.Ready(first)
	gate.Ready(newTestServer()) // must not panic on a second close
	if gate.handler() != http.Handler(first) {
		t.Error("second Ready replaced the installed handler")
	}
}
