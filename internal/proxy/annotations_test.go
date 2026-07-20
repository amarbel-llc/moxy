package proxy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/native"
)

// Regression coverage for #217: a native (moxin) child whose ToolSpec
// carries annotations must surface them through Proxy.ListToolsV1 and
// survive JSON marshaling end to end.
func TestListToolsV1PreservesMoxinAnnotations(t *testing.T) {
	rtrue := true
	child := native.NewServer(&native.NativeConfig{
		Name: "fixture",
		Tools: []native.ToolSpec{{
			Name:        "read",
			Description: "reads things",
			Command:     "echo",
			Annotations: &native.ToolAnnotations{
				ReadOnlyHint:   &rtrue,
				IdempotentHint: &rtrue,
			},
		}},
	})

	p := &Proxy{
		children: []ChildEntry{{
			Client: child,
			Capabilities: protocol.ServerCapabilitiesV1{
				Tools: &protocol.ToolsCapability{},
			},
		}},
	}

	result, err := p.ListToolsV1(context.Background(), "")
	if err != nil {
		t.Fatalf("ListToolsV1: %v", err)
	}

	var found *protocol.ToolV1
	for i := range result.Tools {
		if result.Tools[i].Name == "fixture.read" {
			found = &result.Tools[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("fixture.read not in result: %+v", result.Tools)
	}
	if found.Annotations == nil {
		t.Fatal("fixture.read has nil Annotations")
	}
	if found.Annotations.ReadOnlyHint == nil || !*found.Annotations.ReadOnlyHint {
		t.Error("readOnlyHint: want true")
	}

	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(wire), `"readOnlyHint":true`) {
		t.Errorf("marshaled result missing readOnlyHint: %s", wire)
	}
}
