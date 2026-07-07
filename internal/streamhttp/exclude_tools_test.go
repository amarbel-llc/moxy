package streamhttp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/amarbel-llc/moxy/internal/toolexclude"
)

// stubToolExcluderProvider embeds stubToolProvider and additionally
// implements ToolExcluder, so tests can exercise the real
// /clown/exclude-tools success path without touching stubToolProvider (kept
// minimal for every other existing test, which relies on it NOT
// implementing ToolExcluder being a non-issue since none of them hit this
// route).
type stubToolExcluderProvider struct {
	stubToolProvider
	set toolexclude.Set
}

func (s *stubToolExcluderProvider) SetToolExclude(set toolexclude.Set) { s.set = set }
func (s *stubToolExcluderProvider) ToolExclude() toolexclude.Set       { return s.set }

func newTestServerWithExcluder() (*Server, *stubToolExcluderProvider) {
	excluder := &stubToolExcluderProvider{}
	srv := New(Options{
		Tools:         excluder,
		Resources:     &stubResourceProvider{},
		Prompts:       &stubPromptProvider{},
		ServerName:    "test-moxy",
		ServerVersion: "0.0.0-test",
	})
	return srv, excluder
}

func excludeReqBody(t *testing.T, body excludeToolsBody) *bytes.Reader {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshaling request body: %v", err)
	}
	return bytes.NewReader(data)
}

func decodeExcludeBody(t *testing.T, w *httptest.ResponseRecorder) excludeToolsBody {
	t.Helper()
	var body excludeToolsBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response body %q: %v", w.Body.String(), err)
	}
	sort.Strings(body.Exclude)
	return body
}

func TestExcludeToolsNotImplementedWithoutExcluder(t *testing.T) {
	srv := newTestServer() // stubToolProvider does not implement ToolExcluder
	req := httptest.NewRequest(http.MethodGet, "/clown/exclude-tools", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("want 501, got %d", w.Code)
	}
}

func TestExcludeToolsGetEmpty(t *testing.T) {
	srv, _ := newTestServerWithExcluder()
	req := httptest.NewRequest(http.MethodGet, "/clown/exclude-tools", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := decodeExcludeBody(t, w)
	if len(body.Exclude) != 0 {
		t.Errorf("Exclude = %v, want empty", body.Exclude)
	}
}

func TestExcludeToolsPostReplacesSet(t *testing.T) {
	srv, excluder := newTestServerWithExcluder()

	postReq := httptest.NewRequest(http.MethodPost, "/clown/exclude-tools",
		excludeReqBody(t, excludeToolsBody{Exclude: []string{"chix", "folio.write"}}))
	postW := httptest.NewRecorder()
	srv.ServeHTTP(postW, postReq)

	if postW.Code != http.StatusOK {
		t.Fatalf("POST: want 200, got %d, body %q", postW.Code, postW.Body.String())
	}
	postBody := decodeExcludeBody(t, postW)
	want := []string{"chix", "folio.write"}
	if len(postBody.Exclude) != len(want) {
		t.Fatalf("POST response Exclude = %v, want %v", postBody.Exclude, want)
	}
	for i := range want {
		if postBody.Exclude[i] != want[i] {
			t.Errorf("POST response Exclude = %v, want %v", postBody.Exclude, want)
		}
	}

	if !excluder.ToolExclude().Excludes("chix", "chix.build") {
		t.Error("proxy-side ToolExclude() was not actually updated by the POST")
	}

	// A second POST with a different set fully replaces the first — the
	// old "chix" exclusion must be gone, not merged.
	postReq2 := httptest.NewRequest(http.MethodPost, "/clown/exclude-tools",
		excludeReqBody(t, excludeToolsBody{Exclude: []string{"folio.read"}}))
	postW2 := httptest.NewRecorder()
	srv.ServeHTTP(postW2, postReq2)

	getReq := httptest.NewRequest(http.MethodGet, "/clown/exclude-tools", nil)
	getW := httptest.NewRecorder()
	srv.ServeHTTP(getW, getReq)
	getBody := decodeExcludeBody(t, getW)
	if len(getBody.Exclude) != 1 || getBody.Exclude[0] != "folio.read" {
		t.Errorf("after replace, GET Exclude = %v, want [folio.read]", getBody.Exclude)
	}
	if excluder.ToolExclude().Excludes("chix", "chix.build") {
		t.Error("full-replace POST should have dropped the prior \"chix\" exclusion")
	}
}

func TestExcludeToolsRejectsOtherMethods(t *testing.T) {
	srv, _ := newTestServerWithExcluder()
	req := httptest.NewRequest(http.MethodDelete, "/clown/exclude-tools", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

func TestExcludeToolsRejectsInvalidJSON(t *testing.T) {
	srv, _ := newTestServerWithExcluder()
	req := httptest.NewRequest(http.MethodPost, "/clown/exclude-tools", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}
