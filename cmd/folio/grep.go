package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// grepParams holds parsed grep query parameters.
type grepParams struct {
	Pattern         string
	Path            string
	Glob            string // file glob filter (e.g. "*.go")
	FileType        string // rg --type (e.g. "go", "py")
	OutputMode      string // "files_with_matches" (default), "content", "count"
	Context         int    // -C context lines (only for content mode)
	CaseInsensitive bool
}

// runGrep executes ripgrep with the given parameters and returns the output.
func runGrep(params grepParams) (string, error) {
	args := buildRgArgs(params)

	cmd := exec.Command("rg", args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimRight(string(out), "\n")

	if err != nil {
		// rg exits 1 when no matches found — not an error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "No matches found", nil
		}
		if text != "" {
			return "", fmt.Errorf("rg failed: %s", text)
		}
		return "", fmt.Errorf("rg failed: %w", err)
	}

	if text == "" {
		return "No matches found", nil
	}

	return text, nil
}

func buildRgArgs(params grepParams) []string {
	var args []string

	switch params.OutputMode {
	case "files_with_matches", "":
		args = append(args, "--files-with-matches")
	case "count":
		args = append(args, "--count")
	case "content":
		args = append(args, "--line-number")
		if params.Context > 0 {
			args = append(args, "-C", strconv.Itoa(params.Context))
		}
	}

	if params.CaseInsensitive {
		args = append(args, "-i")
	}

	if params.Glob != "" {
		args = append(args, "--glob", params.Glob)
	}

	if params.FileType != "" {
		args = append(args, "--type", params.FileType)
	}

	args = append(args, "--", params.Pattern)

	if params.Path != "" {
		args = append(args, params.Path)
	}

	return args
}
