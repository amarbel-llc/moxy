package main

import "testing"

func TestDecodeProjectDirName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty input yields empty", "", ""},
		{"simple absolute path", "-tmp-foo", "/tmp/foo"},
		{"home subdir", "-home-sasha-eng", "/home/sasha/eng"},
		{"single segment", "-bin", "/bin"},
		{"no leading dash still rooted", "tmp-foo", "/tmp/foo"},
		// The encoding loses dots: ".worktrees" became "-worktrees" on
		// disk, indistinguishable from a real "/worktrees". The decoder
		// can't recover the dot — it returns the rooted form and the
		// caller marks it (heuristic).
		{"dot lost in encoding", "-home-sasha--worktrees-foo", "/home/sasha//worktrees/foo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := decodeProjectDirName(c.in)
			if got != c.want {
				t.Errorf("decodeProjectDirName(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
