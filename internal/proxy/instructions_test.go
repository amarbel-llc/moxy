package proxy

import (
	"strings"
	"testing"
)

func TestFormatInstructionsSingleRunningServer(t *testing.T) {
	summaries := []ServerSummary{
		{
			Name:              "git",
			Version:           "0.1.0",
			Status:            "running",
			Tools:             15,
			Resources:         6,
			ResourceTemplates: 5,
			Prompts:           2,
		},
	}

	result := FormatInstructions(summaries)

	if !strings.Contains(result, "git") {
		t.Error("expected instructions to contain server name 'git'")
	}
	if !strings.Contains(result, "15 tools") {
		t.Error("expected instructions to contain '15 tools'")
	}
	if !strings.Contains(result, "6 resources") {
		t.Error("expected instructions to contain '6 resources'")
	}
	if !strings.Contains(result, "5 resource templates") {
		t.Error("expected instructions to contain '5 resource templates'")
	}
	if !strings.Contains(result, "0.1.0") {
		t.Error("expected instructions to contain version '0.1.0'")
	}
	if !strings.Contains(result, "moxy://tools/{server}") {
		t.Error("expected instructions to contain discovery hint")
	}
}

func TestFormatInstructionsFailedServer(t *testing.T) {
	summaries := []ServerSummary{
		{
			Name:   "dodder",
			Status: "failed",
			Error:  "child process dodder exited unexpectedly",
		},
	}

	result := FormatInstructions(summaries)

	if !strings.Contains(result, "dodder") {
		t.Error("expected instructions to contain failed server name")
	}
	if !strings.Contains(result, "failed") {
		t.Error("expected instructions to contain 'failed'")
	}
	if !strings.Contains(result, "child process dodder exited unexpectedly") {
		t.Error("expected instructions to contain error message")
	}
}

func TestFormatInstructionsMixedServers(t *testing.T) {
	summaries := []ServerSummary{
		{
			Name:    "git",
			Version: "0.1.0",
			Status:  "running",
			Tools:   15,
		},
		{
			Name:    "jira",
			Version: "1.0.0",
			Status:  "running",
			Tools:   40,
		},
		{
			Name:   "dodder",
			Status: "failed",
			Error:  "exited unexpectedly",
		},
	}

	result := FormatInstructions(summaries)

	if !strings.Contains(result, "git") {
		t.Error("expected 'git' in output")
	}
	if !strings.Contains(result, "jira") {
		t.Error("expected 'jira' in output")
	}
	if !strings.Contains(result, "dodder") {
		t.Error("expected 'dodder' in output")
	}
	if !strings.Contains(result, "40 tools") {
		t.Error("expected '40 tools' for jira")
	}
}

func TestFormatInstructionsEmptyList(t *testing.T) {
	result := FormatInstructions(nil)

	if !strings.Contains(result, "MCP proxy") {
		t.Error("expected header even with no servers")
	}
	if strings.Contains(result, "Child servers:") {
		t.Error("should not contain child servers section when empty")
	}
}

func TestFormatInstructionsWithChildInstructions(t *testing.T) {
	summaries := []ServerSummary{
		{
			Name:         "maneater",
			Version:      "0.4.0",
			Status:       "running",
			Instructions: "Read man pages before executing commands.",
			Tools:        1,
		},
		{
			Name:   "git",
			Status: "running",
			Tools:  15,
		},
	}

	result := FormatInstructions(summaries)

	if !strings.Contains(result, "## maneater") {
		t.Error("expected child instructions section header")
	}
	if !strings.Contains(result, "Read man pages before executing commands.") {
		t.Error("expected child instructions content")
	}
	// Server without instructions should not get a section
	if strings.Contains(result, "## git") {
		t.Error("should not create section for server without instructions")
	}
}

func TestFormatInstructionsNoVersion(t *testing.T) {
	summaries := []ServerSummary{
		{
			Name:   "local",
			Status: "running",
			Tools:  3,
		},
	}

	result := FormatInstructions(summaries)

	// Should not have empty parens or version placeholder
	if strings.Contains(result, "()") {
		t.Error("should not contain empty parens when version is empty")
	}
	if !strings.Contains(result, "3 tools") {
		t.Error("expected '3 tools'")
	}
}
