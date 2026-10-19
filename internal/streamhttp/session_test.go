package streamhttp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
)

func newPersistedServer(path string) *Server {
	return New(Options{
		Tools:         &stubToolProvider{},
		Resources:     &stubResourceProvider{},
		Prompts:       &stubPromptProvider{},
		ServerName:    "test-moxy",
		ServerVersion: "0.0.0-test",
		SessionIDPath: path,
	})
}

func initializeSID(t *testing.T, srv http.Handler) string {
	t.Helper()
	w := postJSON(srv, jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(1)),
		Method:  "initialize",
		Params:  rawJSON(protocol.InitializeParamsV1{ProtocolVersion: protocol.ProtocolVersionV1}),
	}, "")
	sid := w.Header().Get(headerMCPSessionID)
	if sid == "" {
		t.Fatalf("initialize: no session id (status %d)", w.Code)
	}
	return sid
}

func toolsList(srv http.Handler, sid string) *httptest.ResponseRecorder {
	return postJSON(srv, jsonrpc.Message{
		JSONRPC: jsonrpc.Version,
		ID:      idPtr(jsonrpc.NewNumberID(2)),
		Method:  "tools/list",
	}, sid)
}

// A restarted origin (a new Server over the same state file) must keep
// honoring the id issued before the restart, and reissue it on initialize.
func TestSessionSurvivesRestart(t *testing.T) {
	t.Setenv("MOXY_HEARTBEAT_INTERVAL", "0")
	path := filepath.Join(t.TempDir(), "state", "session-id")

	oldSID := initializeSID(t, newPersistedServer(path))

	restarted := newPersistedServer(path)
	if w := toolsList(restarted, oldSID); w.Code != http.StatusOK {
		t.Fatalf("old id after restart: want 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := initializeSID(t, restarted); got != oldSID {
		t.Errorf("initialize after restart issued %q, want the persisted %q", got, oldSID)
	}
	if w := toolsList(restarted, "00000000-0000-4000-8000-000000000000"); w.Code != http.StatusNotFound {
		t.Errorf("never-issued id: want 404, got %d", w.Code)
	}
}

// Without a state path the id is process-local, as before.
func TestSessionNotPersistedWithoutPath(t *testing.T) {
	t.Setenv("MOXY_HEARTBEAT_INTERVAL", "0")
	oldSID := initializeSID(t, newTestServer())
	if w := toolsList(newTestServer(), oldSID); w.Code != http.StatusNotFound {
		t.Errorf("want 404 from a fresh in-memory server, got %d", w.Code)
	}
}

// The session is shared by every client, so one client's DELETE must not end
// it for the others.
func TestDeleteLeavesOtherClientsWorking(t *testing.T) {
	t.Setenv("MOXY_HEARTBEAT_INTERVAL", "0")
	srv := newTestServer()
	sid := initializeSID(t, srv)
	if other := initializeSID(t, srv); other != sid {
		t.Fatalf("second client got %q, want shared %q", other, sid)
	}

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set(headerMCPSessionID, sid)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: want 200, got %d", w.Code)
	}

	if w := toolsList(srv, sid); w.Code != http.StatusOK {
		t.Errorf("other client after DELETE: want 200, got %d: %s", w.Code, w.Body.String())
	}
}
