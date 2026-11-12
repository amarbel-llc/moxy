package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/toolfilter"
	"code.linenisgreat.com/moxy/pkg/moxyfile"
)

// pagedBackend answers tools/list from fixed pages (cursor "" is page 0, the
// cursor "p<n>" is page n) and tools/call with a fixed ok result, recording
// the tools it was asked to call.
type pagedBackend struct {
	pages   []string
	listErr error
	called  []string
}

func (b *pagedBackend) Call(_ context.Context, method string, params any) (json.RawMessage, error) {
	switch method {
	case protocol.MethodToolsList:
		if b.listErr != nil {
			return nil, b.listErr
		}
		page := 0
		if c, ok := params.(*cursorParam); ok && c != nil {
			page = int(c.Cursor[1] - '0')
		}
		return json.RawMessage(b.pages[page]), nil
	case protocol.MethodToolsCall:
		b.called = append(b.called, params.(protocol.ToolCallParams).Name)
		return json.RawMessage(`{"content":[{"type":"text","text":"ran"}]}`), nil
	}
	return nil, errors.New("unexpected method " + method)
}
func (b *pagedBackend) Notify(string, any) error                 { return nil }
func (b *pagedBackend) SetOnNotification(func(*jsonrpc.Message)) {}
func (b *pagedBackend) Name() string                             { return "srv" }
func (b *pagedBackend) Close() error                             { return nil }

func readOnlyFilter() *moxyfile.AnnotationFilter {
	yes := true
	return &moxyfile.AnnotationFilter{ReadOnlyHint: &yes}
}

func filteredProxy(b *pagedBackend, filter *moxyfile.AnnotationFilter) *Proxy {
	return &Proxy{
		toolFilter: toolfilter.All(),
		children: []ChildEntry{{
			Client:       b,
			Config:       moxyfile.ServerConfig{Name: "srv", Annotations: filter},
			Capabilities: toolsCapable(),
		}},
	}
}

const (
	readTool  = `{"name":"read","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}}`
	writeTool = `{"name":"write","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":false}}`
	bareTool  = `{"name":"bare","inputSchema":{"type":"object"}}`
)

func call(t *testing.T, p *Proxy, name string) *protocol.ToolCallResultV1 {
	t.Helper()
	res, err := p.CallToolV1(context.Background(), name, nil)
	if err != nil {
		t.Fatalf("CallToolV1(%s): %v", name, err)
	}
	return res
}

func errorText(res *protocol.ToolCallResultV1) string {
	if !res.IsError || len(res.Content) == 0 {
		return ""
	}
	return res.Content[0].Text
}

// moxy#450: a tool the filter hides from tools/list is not callable by name.
func TestAnnotationFilterRefusesAHiddenToolAtCall(t *testing.T) {
	b := &pagedBackend{pages: []string{`{"tools":[` + readTool + `,` + writeTool + `,` + bareTool + `]}`}}
	p := filteredProxy(b, readOnlyFilter())

	if res := call(t, p, "srv.read"); res.IsError {
		t.Errorf("the listed tool was refused: %s", errorText(res))
	}
	for _, hidden := range []string{"srv.write", "srv.bare"} {
		res := call(t, p, hidden)
		if !strings.Contains(errorText(res), "hidden by its [servers.annotations] filter") {
			t.Errorf("%s: want the filter refusal, got %+v", hidden, res)
		}
	}
	if got := strings.Join(b.called, ","); got != "read" {
		t.Errorf("the child was asked to run %q, want only read", got)
	}
}

func TestAnnotationFilterUnsetCallsEverything(t *testing.T) {
	b := &pagedBackend{pages: []string{`{"tools":[` + writeTool + `]}`}}
	p := filteredProxy(b, nil)
	if res := call(t, p, "srv.write"); res.IsError {
		t.Errorf("no filter, yet refused: %s", errorText(res))
	}
}

func TestAnnotationFilterFindsAToolOnALaterPage(t *testing.T) {
	b := &pagedBackend{pages: []string{
		`{"tools":[` + readTool + `],"nextCursor":"p1"}`,
		`{"tools":[` + writeTool + `]}`,
	}}
	p := filteredProxy(b, readOnlyFilter())
	if res := call(t, p, "srv.write"); !strings.Contains(errorText(res), "hidden by its") {
		t.Errorf("a hidden tool on page 2 was not refused: %+v", res)
	}
}

func TestAnnotationFilterFailsClosedWhenTheListCannotBeRead(t *testing.T) {
	b := &pagedBackend{listErr: errors.New("child is wedged")}
	p := filteredProxy(b, readOnlyFilter())
	res := call(t, p, "srv.read")
	if !strings.Contains(errorText(res), "cannot check the [servers.annotations] filter") {
		t.Errorf("want a refusal naming the check, got %+v", res)
	}
	if len(b.called) != 0 {
		t.Errorf("the child ran %v although the filter could not be checked", b.called)
	}
}

// A tool the child does not list is not the filter's to refuse: the child
// reports it unknown.
func TestAnnotationFilterLeavesAnUnlistedToolToTheChild(t *testing.T) {
	b := &pagedBackend{pages: []string{`{"tools":[` + readTool + `]}`}}
	p := filteredProxy(b, readOnlyFilter())
	call(t, p, "srv.nonesuch")
	if got := strings.Join(b.called, ","); got != "nonesuch" {
		t.Errorf("the child was asked to run %q, want nonesuch", got)
	}
}

func TestAnnotationFilterRefusesAHiddenEphemeralTool(t *testing.T) {
	yes, no := true, false
	meta := &EphemeralMeta{
		Config: moxyfile.ServerConfig{Name: "eph", Annotations: readOnlyFilter()},
		Tools: []protocol.ToolV1{
			{Name: "read", Annotations: &protocol.ToolAnnotations{ReadOnlyHint: &yes}},
			{Name: "write", Annotations: &protocol.ToolAnnotations{ReadOnlyHint: &no}},
		},
	}
	if !ephemeralToolHiddenByFilter(meta, "write") {
		t.Error("write should be hidden")
	}
	if ephemeralToolHiddenByFilter(meta, "read") {
		t.Error("read should be visible")
	}
	if ephemeralToolHiddenByFilter(meta, "nonesuch") {
		t.Error("an unlisted tool is the child's to report")
	}
	p := &Proxy{toolFilter: toolfilter.All(), ephemeral: map[string]*EphemeralMeta{"eph": meta}}
	res, err := p.callToolEphemeral(context.Background(), "eph", "write", nil)
	if err != nil || !strings.Contains(errorText(res), "hidden by its") {
		t.Errorf("callToolEphemeral(write) = %+v, %v; want the filter refusal before any spawn", res, err)
	}
}
