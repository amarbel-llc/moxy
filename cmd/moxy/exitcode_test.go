package main

import (
	"errors"
	"fmt"
	"testing"
)

// moxy#438: commands return their exit code instead of calling os.Exit, and
// main maps what comes back.
func TestExitWith(t *testing.T) {
	if err := exitWith(0); err != nil {
		t.Errorf("exitWith(0) = %v, want nil", err)
	}
	var coded exitCodeError
	if err := exitWith(3); !errors.As(err, &coded) || coded.code != 3 {
		t.Errorf("exitWith(3) = %v, want an exitCodeError carrying 3", err)
	}
}

func TestExitCodeFor(t *testing.T) {
	plain := errors.New("boom")
	cases := []struct {
		name string
		err  error
		args []string
		want int
	}{
		{"success", nil, []string{"status"}, 0},
		{"a command's own verdict", exitCodeError{code: 1}, []string{"narrows"}, 1},
		{"a verdict survives wrapping", fmt.Errorf("wrapped: %w", exitCodeError{code: 2}), []string{"status"}, 2},
		{"an error is exit 1", plain, []string{"serve", "mcp"}, 1},
		// `narrows` reserves 1 for "the child widens"; an error that never
		// reached the comparison must not read as that verdict.
		{"a narrows error is unreadable, not widens", plain, []string{"narrows", "--parent"}, exitUnreadable},
		{"no args", plain, nil, 1},
	}
	for _, c := range cases {
		if got := exitCodeFor(c.err, c.args); got != c.want {
			t.Errorf("%s: exitCodeFor = %d, want %d", c.name, got, c.want)
		}
	}
}
