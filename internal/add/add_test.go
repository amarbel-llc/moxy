package add

import (
	"testing"

	"code.linenisgreat.com/moxy/pkg/moxyfile"
)

func TestBuildServerConfigNoAnnotations(t *testing.T) {
	srv := buildCommandServerConfig("grit", "grit mcp", nil)
	if srv.Name != "grit" {
		t.Errorf("expected name grit, got %q", srv.Name)
	}
	if srv.Command.Executable() != "grit" {
		t.Errorf("expected executable grit, got %q", srv.Command.Executable())
	}
	if srv.Annotations != nil {
		t.Error("expected nil annotations")
	}
}

func TestBuildServerConfigWithAnnotations(t *testing.T) {
	srv := buildCommandServerConfig("grit", "grit", []string{"readOnlyHint", "destructiveHint"})
	if srv.Annotations == nil {
		t.Fatal("expected annotations")
	}
	if srv.Annotations.ReadOnlyHint == nil || !*srv.Annotations.ReadOnlyHint {
		t.Error("expected readOnlyHint = true")
	}
	if srv.Annotations.DestructiveHint == nil || !*srv.Annotations.DestructiveHint {
		t.Error("expected destructiveHint = true")
	}
	if srv.Annotations.IdempotentHint != nil {
		t.Error("expected idempotentHint to be nil")
	}
}

var _ = moxyfile.ServerConfig{} // ensure config import is used
