package native

import (
	"encoding/json"
	"testing"
)

// TestShapeDynamicPermsInput_StripsUnlistedContent is the end-to-end
// guard for the bug where folio.write of Rust-doc-comment content
// (starting `//!`) triggered spurious permission prompts. The leak
// path: shapeDynamicPermsInput → BuildExtraArgs → argv[2] carrying the
// file content into the perms script. After the fix,
// shapeDynamicPermsInput uses BuildPermsArgs and the content field
// never reaches the script.
func TestShapeDynamicPermsInput_StripsUnlistedContent(t *testing.T) {
	spec := &DynamicPermsSpec{
		Command:  "/bin/true",
		ArgOrder: []string{"file_path"},
	}
	args := json.RawMessage(`{"file_path":"/x","content":"//! foo bar"}`)

	stdin, argv, err := shapeDynamicPermsInput(spec, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdin != "" {
		t.Errorf("stdin = %q, want empty (no stdin-param configured)", stdin)
	}
	if len(argv) != 1 {
		t.Fatalf("argv = %v, want exactly one slot (file_path); content must not leak", argv)
	}
	if argv[0] != "/x" {
		t.Errorf("argv[0] = %q, want \"/x\"", argv[0])
	}
}
