package moxyfile

import (
	"strings"
	"testing"
)

// moxy#486: permission-mode and enforce-allow parse strictly, merge enforce-
// wins / grants-union down the hierarchy, and render canonically.

func TestParsePermissionModeRejectsUnknownValues(t *testing.T) {
	for _, ok := range []string{``, `permission-mode = "delegate"`, `permission-mode = "enforce"`} {
		if _, err := ParseLiteral([]byte(ok)); err != nil {
			t.Errorf("%q: unexpected error %v", ok, err)
		}
	}
	if _, err := ParseLiteral([]byte(`permission-mode = "ask"`)); err == nil || !strings.Contains(err.Error(), "permission-mode") {
		t.Errorf("permission-mode = ask: err = %v, want a permission-mode error", err)
	}
	// A restriction key written with the wrong type is an error, never unset.
	if _, err := ParseLiteral([]byte(`permission-mode = true`)); err == nil {
		t.Error("permission-mode = true must be refused")
	}
	if _, err := ParseLiteral([]byte(`enforce-allow = "smith"`)); err == nil {
		t.Error("enforce-allow as a string must be refused")
	}
	if _, err := ParseLiteral([]byte(`enforce-allow = ["smith.a.b"]`)); err == nil {
		t.Error("an enforce-allow entry with two dots must be refused")
	}
}

func TestMergePermissionModeEnforceWins(t *testing.T) {
	enforce := Config{PermissionMode: PermissionModeEnforce, EnforceAllow: []string{"smith"}}
	delegate := Config{PermissionMode: PermissionModeDelegate, EnforceAllow: []string{"grit.status", "smith"}}

	if got := Merge(enforce, delegate); !got.Enforces() {
		t.Errorf("enforce then delegate: PermissionMode = %q, want enforce to stick", got.PermissionMode)
	} else if len(got.EnforceAllow) != 2 {
		t.Errorf("grants = %v, want the union [smith grit.status]", got.EnforceAllow)
	}
	if got := Merge(delegate, enforce); !got.Enforces() {
		t.Errorf("delegate then enforce: PermissionMode = %q, want enforce", got.PermissionMode)
	}
	if got := Merge(Config{}, delegate); got.PermissionMode != PermissionModeDelegate {
		t.Errorf("unset then delegate: PermissionMode = %q, want delegate made explicit", got.PermissionMode)
	}
	if got := Merge(Config{}, Config{}); got.PermissionMode != "" || got.Enforces() {
		t.Errorf("unset then unset: PermissionMode = %q, want unset (delegate)", got.PermissionMode)
	}
}

func TestRenderEffectiveWritesPermissionKeys(t *testing.T) {
	cfg, err := ParseLiteral([]byte("permission-mode = \"enforce\"\nenforce-allow = [\"smith\", \"grit.status\", \"smith\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(RenderEffective(cfg))
	if !strings.Contains(out, "permission-mode = \"enforce\"\n") {
		t.Errorf("render lacks permission-mode:\n%s", out)
	}
	if !strings.Contains(out, "enforce-allow = [\"grit.status\", \"smith\"]\n") {
		t.Errorf("render lacks sorted, deduplicated enforce-allow:\n%s", out)
	}
	back, err := ParseLiteral([]byte(out))
	if err != nil {
		t.Fatalf("render does not parse: %v\n%s", err, out)
	}
	if !back.Enforces() || len(back.EnforceAllow) != 2 {
		t.Errorf("round trip lost the keys: %+v", back)
	}
}
