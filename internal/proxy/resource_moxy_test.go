package proxy

import (
	"context"
	"encoding/json"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
)

// #460: the moxy://tools resources answer from the allow-tools set tools/list
// uses, and filtering the replayed ephemeral cache leaves the cache whole.
func TestMoxyToolsResourcesApplyAllowTools(t *testing.T) {
	cached := []protocol.ToolV1{{Name: "find"}, {Name: "write"}}
	p := &Proxy{
		ephemeral: map[string]*EphemeralMeta{
			"eph": {Capabilities: toolsCapable(), Tools: cached},
		},
		toolAllow: allowSet([]string{"eph.write"}),
	}
	m := &moxyResourceProvider{proxy: p}
	ctx := context.Background()

	res, err := m.ReadResource(ctx, "moxy://tools/eph")
	if err != nil {
		t.Fatalf("read moxy://tools/eph: %v", err)
	}
	var summaries []struct{ Name string }
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &summaries); err != nil {
		t.Fatalf("summary is not JSON: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Name != "write" {
		t.Errorf("moxy://tools/eph = %+v, want only write", summaries)
	}

	if _, err := m.ReadResource(ctx, "moxy://tools/eph/find"); err == nil {
		t.Error("moxy://tools/eph/find returned the schema of a tool outside allow-tools")
	}
	if _, err := m.ReadResource(ctx, "moxy://tools/eph/write"); err != nil {
		t.Errorf("moxy://tools/eph/write: %v", err)
	}

	if cached[0].Name != "find" || cached[1].Name != "write" {
		t.Errorf("ephemeral cache = %+v, want it untouched", cached)
	}
}
