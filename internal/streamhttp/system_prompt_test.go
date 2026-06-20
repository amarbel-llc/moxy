package streamhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServerWithFragment(fragment string) *Server {
	return New(Options{
		Tools:                &stubToolProvider{},
		Resources:            &stubResourceProvider{},
		Prompts:              &stubPromptProvider{},
		ServerName:           "test-moxy",
		ServerVersion:        "0.0.0-test",
		SystemPromptFragment: fragment,
	})
}

func TestSystemPromptServesFragment(t *testing.T) {
	srv := newTestServerWithFragment("## hello\nbody\n")
	req := httptest.NewRequest(http.MethodGet, "/clown/system-prompt", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/markdown; charset=utf-8", ct)
	}
	if got := w.Body.String(); got != "## hello\nbody\n" {
		t.Errorf("body = %q", got)
	}
}

func TestSystemPromptEmptyReturns204(t *testing.T) {
	srv := newTestServer() // no SystemPromptFragment set
	req := httptest.NewRequest(http.MethodGet, "/clown/system-prompt", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("want 204, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("204 body should be empty, got %q", w.Body.String())
	}
}

func TestSystemPromptRejectsNonGet(t *testing.T) {
	srv := newTestServerWithFragment("x")
	req := httptest.NewRequest(http.MethodPost, "/clown/system-prompt", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}
