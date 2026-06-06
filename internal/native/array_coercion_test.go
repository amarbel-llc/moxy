package native

import (
	"encoding/json"
	"testing"
)

// Regression coverage for #309: when an input is declared `type: array` in
// the tool schema but the client passes a scalar (string/number/bool), the
// arg layer must coerce it to a single-element JSON array before serializing
// — so scripts splitting array args with `jq -r '.[]'` always receive valid
// JSON. Arrays pass through untouched; null stays an empty slot.

const arrayPathsSchema = `{
	"type": "object",
	"properties": {
		"paths": {"type": "array"},
		"ref": {"type": "string"}
	}
}`

func TestBuildExtraArgsArrayCoercion(t *testing.T) {
	t.Run("scalar string coerced to single-element array (arg_order)", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"paths":"file.txt"}`),
			json.RawMessage(arrayPathsSchema),
			[]string{"paths", "ref"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 1 || args[0] != `["file.txt"]` {
			t.Errorf(`args = %v, want ["[\"file.txt\"]"]`, args)
		}
	})

	t.Run("array passes through unchanged (arg_order)", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"paths":["a.txt","b.txt"]}`),
			json.RawMessage(arrayPathsSchema),
			[]string{"paths", "ref"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 1 || args[0] != `["a.txt","b.txt"]` {
			t.Errorf(`args = %v, want ["[\"a.txt\",\"b.txt\"]"]`, args)
		}
	})

	t.Run("scalar number coerced (arg_order)", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"paths":42}`),
			json.RawMessage(arrayPathsSchema),
			[]string{"paths"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 1 || args[0] != `[42]` {
			t.Errorf("args = %v, want [\"[42]\"]", args)
		}
	})

	t.Run("null for array key stays empty slot", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"paths":null}`),
			json.RawMessage(arrayPathsSchema),
			[]string{"paths"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// null behaves like an absent key: empty slot, then trailing-trimmed.
		if len(args) != 0 {
			t.Errorf("args = %v, want []", args)
		}
	})

	t.Run("string-declared key is not coerced", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"ref":"main"}`),
			json.RawMessage(arrayPathsSchema),
			[]string{"paths", "ref"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 2 || args[0] != "" || args[1] != "main" {
			t.Errorf(`args = %v, want ["", "main"]`, args)
		}
	})

	t.Run("scalar coerced in schema-ordered fallback (no arg_order)", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"paths":"file.txt"}`),
			json.RawMessage(arrayPathsSchema),
			nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 1 || args[0] != `["file.txt"]` {
			t.Errorf(`args = %v, want ["[\"file.txt\"]"]`, args)
		}
	})

	t.Run("scalar coerced for unlisted key appended after arg_order", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"ref":"main","paths":"x.txt"}`),
			json.RawMessage(arrayPathsSchema),
			[]string{"ref"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 2 || args[0] != "main" || args[1] != `["x.txt"]` {
			t.Errorf(`args = %v, want ["main", "[\"x.txt\"]"]`, args)
		}
	})

	t.Run("no schema means no coercion", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"paths":"file.txt"}`),
			nil,
			[]string{"paths"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 1 || args[0] != "file.txt" {
			t.Errorf(`args = %v, want ["file.txt"]`, args)
		}
	})
}

func TestBuildPermsArgsArrayCoercion(t *testing.T) {
	t.Run("scalar string coerced under perms arg_order", func(t *testing.T) {
		args, err := BuildPermsArgs(
			json.RawMessage(`{"paths":"file.txt"}`),
			json.RawMessage(arrayPathsSchema),
			[]string{"paths"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 1 || args[0] != `["file.txt"]` {
			t.Errorf(`args = %v, want ["[\"file.txt\"]"]`, args)
		}
	})

	t.Run("array passes through under perms arg_order", func(t *testing.T) {
		args, err := BuildPermsArgs(
			json.RawMessage(`{"paths":["a.txt"]}`),
			json.RawMessage(arrayPathsSchema),
			[]string{"paths"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 1 || args[0] != `["a.txt"]` {
			t.Errorf(`args = %v, want ["[\"a.txt\"]"]`, args)
		}
	})
}
