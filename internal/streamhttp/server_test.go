package streamhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

type stubToolProvider struct{}

func (s *stubToolProvider) ListTools(_ context.Context) ([]protocol.Tool, error) {
	return nil, nil
}

func (s *stubToolProvider) CallTool(_ context.Context, _ string, _ json.RawMessage) (*protocol.ToolCallResult, error) {
	return nil, nil
}

func (s *stubToolProvider) ListToolsV1(_ context.Context, _ string) (*protocol.ToolsListResultV1, error) {
	return &protocol.ToolsListResultV1{
		Tools: []protocol.ToolV1{
			{Name: "test.echo", Description: "echoes input"},
		},
	}, nil
}

func (s *stubToolProvider) CallToolV1(_ context.Context, name string, _ json.RawMessage) (*protocol.ToolCallResultV1, error) {
	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{protocol.TextContentV1("called " + name)},
	}, nil
}

type stubResourceProvider struct{}

func (s *stubResourceProvider) ListResources(_ context.Context) ([]protocol.Resource, error) {
	return nil, nil
}

func (s *stubResourceProvider) ReadResource(_ context.Context, _ string) (*protocol.ResourceReadResult, error) {
	return &protocol.ResourceReadResult{}, nil
}

func (s *stubResourceProvider) ListResourceTemplates(_ context.Context) ([]protocol.ResourceTemplate, error) {
	return nil, nil
}

func (s *stubResourceProvider) ListResourcesV1(_ context.Context, _ string) (*protocol.ResourcesListResultV1, error) {
	return &protocol.ResourcesListResultV1{}, nil
}

func (s *stubResourceProvider) ListResourceTemplatesV1(_ context.Context, _ string) (*protocol.ResourceTemplatesListResultV1, error) {
	return &protocol.ResourceTemplatesListResultV1{}, nil
}

type stubPromptProvider struct{}

func (s *stubPromptProvider) ListPrompts(_ context.Context) ([]protocol.Prompt, error) {
	return nil, nil
}

func (s *stubPromptProvider) GetPrompt(_ context.Context, _ string, _ map[string]string) (*protocol.PromptGetResult, error) {
	return nil, nil
}

func (s *stubPromptProvider) ListPromptsV1(_ context.Context, _ string) (*protocol.PromptsListResultV1, error) {
	return &protocol.PromptsListResultV1{}, nil
}

func (s *stubPromptProvider) GetPromptV1(_ context.Context, _ string, _ map[string]string) (*protocol.PromptGetResultV1, error) {
	return &protocol.PromptGetResultV1{}, nil
}

func newTestServer() *Server {
	return New(Options{
		Tools:         &stubToolProvider{},
		Resources:     &stubResourceProvider{},
		Prompts:       &stubPromptProvider{},
		ServerName:    "test-moxy",
		ServerVersion: "0.0.0-test",
		Instructions:  "test instructions",
	})
}

func postJSON(srv http.Handler, body any, sessionID string) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set(headerMCPSessionID, sessionID)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestInitializeAndToolsList(t *testing.T) {
	srv := newTestServer()

	initMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(1)),
		Method:  "initialize",
		Params:  rawJSON(protocol.InitializeParamsV1{ProtocolVersion: protocol.ProtocolVersionV1}),
	}

	w := postJSON(srv, initMsg, "")
	if w.Code != http.StatusOK {
		t.Fatalf("initialize: want 200, got %d: %s", w.Code, w.Body.String())
	}

	sid := w.Header().Get(headerMCPSessionID)
	if sid == "" {
		t.Fatal("initialize: no session ID in response")
	}

	var initResp jsonrpc.Message
	if err := json.Unmarshal(w.Body.Bytes(), &initResp); err != nil {
		t.Fatalf("initialize: bad JSON: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("initialize: got error: %s", initResp.Error.Message)
	}

	var result protocol.InitializeResultV1
	if err := json.Unmarshal(initResp.Result, &result); err != nil {
		t.Fatalf("initialize: decode result: %v", err)
	}
	if result.Capabilities.Tools == nil || !result.Capabilities.Tools.ListChanged {
		t.Error("initialize: expected tools.listChanged=true")
	}
	if result.ServerInfo.Name != "test-moxy" {
		t.Errorf("initialize: server name = %q, want test-moxy", result.ServerInfo.Name)
	}

	toolsMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(2)),
		Method:  "tools/list",
	}
	w = postJSON(srv, toolsMsg, sid)
	if w.Code != http.StatusOK {
		t.Fatalf("tools/list: want 200, got %d: %s", w.Code, w.Body.String())
	}

	var toolsResp jsonrpc.Message
	if err := json.Unmarshal(w.Body.Bytes(), &toolsResp); err != nil {
		t.Fatalf("tools/list: bad JSON: %v", err)
	}

	var toolsResult protocol.ToolsListResultV1
	if err := json.Unmarshal(toolsResp.Result, &toolsResult); err != nil {
		t.Fatalf("tools/list: decode: %v", err)
	}
	if len(toolsResult.Tools) != 1 || toolsResult.Tools[0].Name != "test.echo" {
		t.Errorf("tools/list: got %+v, want [test.echo]", toolsResult.Tools)
	}
}

func TestSessionValidation(t *testing.T) {
	srv := newTestServer()

	toolsMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(1)),
		Method:  "tools/list",
	}

	w := postJSON(srv, toolsMsg, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("no session: want 404, got %d", w.Code)
	}

	initMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(1)),
		Method:  "initialize",
		Params:  rawJSON(protocol.InitializeParamsV1{ProtocolVersion: protocol.ProtocolVersionV1}),
	}
	w = postJSON(srv, initMsg, "")
	sid := w.Header().Get(headerMCPSessionID)

	w = postJSON(srv, toolsMsg, "wrong-session-id")
	if w.Code != http.StatusNotFound {
		t.Errorf("wrong session: want 404, got %d", w.Code)
	}

	w = postJSON(srv, toolsMsg, sid)
	if w.Code != http.StatusOK {
		t.Errorf("correct session: want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNotificationReturns202(t *testing.T) {
	srv := newTestServer()

	initMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(1)),
		Method:  "initialize",
		Params:  rawJSON(protocol.InitializeParamsV1{ProtocolVersion: protocol.ProtocolVersionV1}),
	}
	w := postJSON(srv, initMsg, "")
	sid := w.Header().Get(headerMCPSessionID)

	notifMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		Method:  "notifications/initialized",
	}
	w = postJSON(srv, notifMsg, sid)
	if w.Code != http.StatusAccepted {
		t.Errorf("notification: want 202, got %d", w.Code)
	}
}

func TestDeleteTerminatesSession(t *testing.T) {
	srv := newTestServer()

	initMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(1)),
		Method:  "initialize",
		Params:  rawJSON(protocol.InitializeParamsV1{ProtocolVersion: protocol.ProtocolVersionV1}),
	}
	w := postJSON(srv, initMsg, "")
	sid := w.Header().Get(headerMCPSessionID)

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set(headerMCPSessionID, sid)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: want 200, got %d", w.Code)
	}

	toolsMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(2)),
		Method:  "tools/list",
	}
	w = postJSON(srv, toolsMsg, sid)
	if w.Code != http.StatusNotFound {
		t.Errorf("after delete: want 404, got %d", w.Code)
	}
}

func TestSSEStreamReceivesNotification(t *testing.T) {
	srv := newTestServer()

	initMsg := jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(1)),
		Method:  "initialize",
		Params:  rawJSON(protocol.InitializeParamsV1{ProtocolVersion: protocol.ProtocolVersionV1}),
	}
	w := postJSON(srv, initMsg, "")
	sid := w.Header().Get(headerMCPSessionID)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set(headerMCPSessionID, sid)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /mcp: want 200, got %d", resp.StatusCode)
	}

	msg, _ := jsonrpc.NewNotification(protocol.MethodNotificationsToolsListChanged, nil)
	if err := srv.Notify(msg); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read SSE: %v", err)
	}
	body := string(buf[:n])
	if !bytes.Contains(buf[:n], []byte("notifications/tools/list_changed")) {
		t.Errorf("SSE event did not contain expected notification, got: %s", body)
	}
}

func TestHealthzReturnsOKWithoutSession(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /healthz: want 200, got %d", w.Code)
	}
}

func TestUnknownPathReturns404(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GET /unknown: want 404, got %d", w.Code)
	}
}

func idPtr(id jsonrpc.ID) *jsonrpc.ID { return &id }

func rawJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
