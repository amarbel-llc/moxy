package proxy

import (
	"fmt"
	"strings"
)

type ServerSummary struct {
	Name              string `json:"name"`
	Version           string `json:"version,omitempty"`
	Status            string `json:"status"`
	Error             string `json:"error,omitempty"`
	Instructions      string `json:"instructions,omitempty"`
	Tools             int    `json:"tools"`
	Resources         int    `json:"resources"`
	ResourceTemplates int    `json:"resource_templates"`
	Prompts           int    `json:"prompts"`
}

func FormatInstructions(servers []ServerSummary) string {
	var b strings.Builder
	b.WriteString("MCP proxy aggregating tools, resources, and prompts from child servers.\n\n")
	b.WriteString("Tool outputs above the inline-token threshold are streamed into a content-addressable blob store and replaced with a `madder://blobs/<digest>` URI. ")
	b.WriteString("These URIs can be used anywhere a file path is accepted (e.g. jq input) — moxy rewrites them to file descriptors at invocation time. ")
	b.WriteString("Read directly with `madder cat <digest>` if needed.\n\n")
	b.WriteString("Prefer tools marked [perms: always-allow] over those requiring permission prompts, unless the task specifically needs a permissioned tool.")

	if len(servers) == 0 {
		return b.String()
	}

	b.WriteString("\n\nChild servers:\n")

	for _, s := range servers {
		if s.Status == "failed" {
			fmt.Fprintf(&b, "- %s (failed: %s)\n", s.Name, s.Error)
			continue
		}

		fmt.Fprintf(&b, "- %s: %d tools, %d resources, %d resource templates",
			s.Name, s.Tools, s.Resources, s.ResourceTemplates)

		if s.Version != "" {
			fmt.Fprintf(&b, " (%s %s)", s.Name, s.Version)
		}
		b.WriteByte('\n')
	}

	b.WriteString("\nUse moxy://tools/{server} to discover available tools.\n")
	b.WriteString("Use {server}.resource-templates to discover available resource URI patterns.")

	for _, s := range servers {
		if s.Instructions == "" {
			continue
		}
		fmt.Fprintf(&b, "\n\n## %s\n\n%s", s.Name, s.Instructions)
	}

	return b.String()
}

// FormatSystemPromptFragment builds the dynamic system-prompt fragment clown
// fetches at session launch (RFC-0002 §5) and appends to claude's system
// prompt. It surfaces live child-server state the static prompt structurally
// can't express: which children failed to start this session (otherwise
// invisible to the agent until a tool call fails) and a compact roster of the
// connected servers with tool counts. ok is false when there are no child
// servers at all, which the HTTP handler maps to 204 (nothing to add).
func FormatSystemPromptFragment(servers []ServerSummary) (string, bool) {
	var running, failed []ServerSummary
	for _, s := range servers {
		if s.Status == "failed" {
			failed = append(failed, s)
		} else {
			running = append(running, s)
		}
	}
	if len(running) == 0 && len(failed) == 0 {
		return "", false
	}

	var b strings.Builder
	b.WriteString("## moxy child servers (this session)\n")

	if len(failed) > 0 {
		b.WriteString("\n**Failed to start this session — their tools and resources are unavailable:**\n")
		for _, s := range failed {
			if s.Error != "" {
				fmt.Fprintf(&b, "- `%s` — %s\n", s.Name, s.Error)
			} else {
				fmt.Fprintf(&b, "- `%s`\n", s.Name)
			}
		}
	}

	if len(running) > 0 {
		parts := make([]string, 0, len(running))
		for _, s := range running {
			parts = append(parts, fmt.Sprintf("%s (%d tools)", s.Name, s.Tools))
		}
		fmt.Fprintf(&b, "\nConnected: %s.\n", strings.Join(parts, ", "))
	}

	b.WriteString("\nDiscover a server's tools with the `moxy://tools/{server}` resource. ")
	b.WriteString("Large tool outputs are streamed to the madder blob store and returned as ")
	b.WriteString("`madder://blobs/<digest>` URIs, usable anywhere a file path is accepted.\n")

	return b.String(), true
}
