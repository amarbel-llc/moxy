package native

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
)

// writeLikeServer builds a server whose single "write" tool mirrors folio's
// write: arg-order [file_path, content], a schema declaring exactly those two
// string properties (both required), and a body that writes $2 to $1 the same
// way moxins/folio/bin/write does (printf '%s', verbatim).
func writeLikeServer() *Server {
	return NewServer(&NativeConfig{
		Name: "test-server",
		Tools: []ToolSpec{
			{
				Name:     "write",
				Command:  "bash",
				Args:     []string{"-c", `printf '%s' "$2" > "$1"`, "write"},
				ArgOrder: []string{"file_path", "content"},
				Input:    json.RawMessage(`{"type":"object","properties":{"file_path":{"type":"string"},"content":{"type":"string"}},"required":["file_path","content"]}`),
				InputParsed: &InputSchema{
					Type:     "object",
					Required: []string{"file_path", "content"},
					Properties: map[string]PropertySchema{
						"file_path": {Type: "string"},
						"content":   {Type: "string"},
					},
				},
			},
		},
	})
}

func callWrite(t *testing.T, srv *Server, args string) protocol.ToolCallResultV1 {
	t.Helper()
	raw, err := srv.Call(context.Background(), "tools/call", protocol.ToolCallParams{
		Name:      "write",
		Arguments: json.RawMessage(args),
	})
	if err != nil {
		t.Fatalf("Call tools/call: %v", err)
	}
	var result protocol.ToolCallResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return result
}

func resultText(result protocol.ToolCallResultV1) string {
	var b strings.Builder
	for _, c := range result.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

// TestArgValidationRejectsEditStyleArgs pins the fix for #362: calling the
// whole-file write tool with Edit-style old_string/new_string (the wrong tool
// shape) must error on the unknown keys instead of silently truncating the
// file to the new_string fragment.
//
// Before the guard, buildExtraArgs trimmed the absent `content` slot and
// appended the unknown new_string as a trailing positional, so it landed on
// the script's $2 and clobbered the file while reporting success.
func TestArgValidationRejectsEditStyleArgs(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "real-source.go")
	const original = "package real\n\n// ... 250 lines of real source ...\n"
	if err := os.WriteFile(out, []byte(original), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	args := fmt.Sprintf(
		`{"file_path":%q,"old_string":"func Old() {}","new_string":"func New() {}"}`,
		out,
	)
	result := callWrite(t, writeLikeServer(), args)

	if !result.IsError {
		t.Fatalf("expected IsError for Edit-style args, got success: %q", resultText(result))
	}
	msg := resultText(result)
	for _, want := range []string{"unknown argument", "new_string", "old_string"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}

	// The file must be UNTOUCHED — the guard rejects before the script runs.
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != original {
		t.Errorf("file was modified despite rejection\n got %q\nwant %q", string(got), original)
	}
}

// TestArgValidationRejectsWrongKeyName pins the fix for #358: calling write
// with `path` instead of `file_path` must error on the unknown key rather than
// leaving the required file_path slot empty (which produced an opaque
// `mv "$tmp" ""` failure and never created the target).
func TestArgValidationRejectsWrongKeyName(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "new-file.txt")

	args := fmt.Sprintf(`{"path":%q,"content":"hello"}`, target)
	result := callWrite(t, writeLikeServer(), args)

	if !result.IsError {
		t.Fatalf("expected IsError for wrong key name, got success: %q", resultText(result))
	}
	msg := resultText(result)
	for _, want := range []string{"unknown argument", "path", "valid:"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}
	if _, err := os.Stat(target); err == nil {
		t.Errorf("target %q must not be created when the key name is wrong", target)
	}
}

// TestArgValidationRejectsMissingRequired covers the required-presence half of
// the guard: omitting a required arg errors clearly instead of dispatching the
// script with an empty positional (which would write an empty file).
func TestArgValidationRejectsMissingRequired(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")

	args := fmt.Sprintf(`{"file_path":%q}`, target)
	result := callWrite(t, writeLikeServer(), args)

	if !result.IsError {
		t.Fatalf("expected IsError for missing required arg, got success: %q", resultText(result))
	}
	msg := resultText(result)
	for _, want := range []string{"missing required argument", "content"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}
	if _, err := os.Stat(target); err == nil {
		t.Errorf("target %q must not be created when a required arg is missing", target)
	}
}

// TestArgValidationRejectsNullRequired treats an explicit JSON null for a
// required arg as missing — null would otherwise reach the script as an empty
// positional, the same failure mode as omission.
func TestArgValidationRejectsNullRequired(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")

	args := fmt.Sprintf(`{"file_path":%q,"content":null}`, target)
	result := callWrite(t, writeLikeServer(), args)

	if !result.IsError {
		t.Fatalf("expected IsError for null required arg, got success: %q", resultText(result))
	}
	if msg := resultText(result); !strings.Contains(msg, "missing required argument") {
		t.Errorf("error message %q missing %q", msg, "missing required argument")
	}
}

// TestArgValidationAllowsValidArgs guards the happy path: a correctly-shaped
// call still dispatches and writes the content verbatim.
func TestArgValidationAllowsValidArgs(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")

	args := fmt.Sprintf(`{"file_path":%q,"content":"valid content"}`, out)
	result := callWrite(t, writeLikeServer(), args)

	if result.IsError {
		t.Fatalf("valid args must succeed, got error: %q", resultText(result))
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "valid content" {
		t.Errorf("content = %q, want %q", string(got), "valid content")
	}
}

// TestArgValidationAllowsMetaKey ensures keys reserved by the MCP "_meta"
// convention pass the unknown-key gate (a compliant client may attach _meta to
// tool arguments; it must not be mistaken for a caller mistake).
func TestArgValidationAllowsMetaKey(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")

	args := fmt.Sprintf(
		`{"file_path":%q,"content":"ok","_meta":{"progressToken":"t1"}}`,
		out,
	)
	result := callWrite(t, writeLikeServer(), args)

	if result.IsError {
		t.Fatalf("_meta key must be allowed, got error: %q", resultText(result))
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "ok" {
		t.Errorf("content = %q, want %q", string(got), "ok")
	}
}
