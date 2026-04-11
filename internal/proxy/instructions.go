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
	b.WriteString("Tool results include `moxy.native://results/{session}/{id}` URIs pointing to cached full output. ")
	b.WriteString("These URIs can be used anywhere a file path is accepted (e.g. folio-external read tools, jq input) — ")
	b.WriteString("moxy rewrites them to file descriptors at invocation time.")

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
