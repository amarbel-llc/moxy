package native

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// An input that names a file is substituted only when it is a blob URI and
// nothing else: substituting part of it yields a path the permission check
// never saw. Other inputs keep substring substitution.
func TestSubstituteResourcePathOnlyAsAWholeValue(t *testing.T) {
	src := &fakeMadder{blobs: map[string]string{"blake2b256-abc": "x"}}
	s := &Server{config: &NativeConfig{Name: "t"}, madder: src}
	spec := &ToolSpec{
		SubstituteArgs: []string{"path", "files", "stdin"},
		Resources:      []ResourceArg{{Param: "path", Op: "read"}, {Param: "files", Op: "read"}},
	}
	args := json.RawMessage(`{
		"path": "madder://blobs/blake2b256-abc/../../x",
		"files": ["a/madder://blobs/blake2b256-abc", "madder://blobs/blake2b256-abc?x=1"],
		"stdin": "see madder://blobs/blake2b256-abc here"
	}`)
	out, sub, err := s.substituteInputBlobURIs(context.Background(), spec, args)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cleanup()
	var got struct {
		Path  string   `json:"path"`
		Files []string `json:"files"`
		Stdin string   `json:"stdin"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Path != "madder://blobs/blake2b256-abc/../../x" {
		t.Errorf("path = %q, want it unchanged", got.Path)
	}
	for _, f := range got.Files {
		if strings.Contains(f, "/dev/fd/") {
			t.Errorf("files entry substituted in part: %q", f)
		}
	}
	if !strings.HasPrefix(got.Stdin, "see /dev/fd/") {
		t.Errorf("stdin = %q, want substring substitution", got.Stdin)
	}

	out, sub, err = s.substituteInputBlobURIs(context.Background(), spec,
		json.RawMessage(`{"path": "madder://blobs/blake2b256-abc"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Cleanup()
	if err := json.Unmarshal(out, &got); err != nil || got.Path != "/dev/fd/3" {
		t.Errorf("whole value: path = %q, %v", got.Path, err)
	}
}
