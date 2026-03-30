package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"
)

type manServer struct{}

type pageSection struct {
	Name      string
	Level     int // 1 = top-level (#), 2 = subsection (##)
	Content   string
	LineCount int
}

var templates = []protocol.ResourceTemplate{
	{
		URITemplate: "man://{page}",
		Name:        "Man page TOC",
		Description: "List all sections and subsections of a Unix man page",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "man://{section}/{page}",
		Name:        "Man page TOC (specific section)",
		Description: "List all sections and subsections of a Unix man page by section number and name",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "man://{page}/{section_name}",
		Name:        "Man page section",
		Description: "Read a specific section of a Unix man page",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "man://{section}/{page}/{section_name}",
		Name:        "Man page section (specific section)",
		Description: "Read a specific section of a Unix man page by section number and name",
		MimeType:    "text/plain",
	},
}

var templatesV1 = []protocol.ResourceTemplateV1{
	{
		URITemplate: "man://{page}",
		Name:        "Man page TOC",
		Description: "List all sections and subsections of a Unix man page",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "man://{section}/{page}",
		Name:        "Man page TOC (specific section)",
		Description: "List all sections and subsections of a Unix man page by section number and name",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "man://{page}/{section_name}",
		Name:        "Man page section",
		Description: "Read a specific section of a Unix man page",
		MimeType:    "text/plain",
	},
	{
		URITemplate: "man://{section}/{page}/{section_name}",
		Name:        "Man page section (specific section)",
		Description: "Read a specific section of a Unix man page by section number and name",
		MimeType:    "text/plain",
	},
}

// ResourceProvider (base interface)

func (m *manServer) ListResources(_ context.Context) ([]protocol.Resource, error) {
	return nil, nil
}

func (m *manServer) ReadResource(_ context.Context, uri string) (*protocol.ResourceReadResult, error) {
	manSection, page, sectionName, err := parseManURI(uri)
	if err != nil {
		return nil, err
	}

	sourcePath, err := locateSource(manSection, page)
	if err != nil {
		return nil, err
	}

	markdown, err := renderMarkdown(sourcePath)
	if err != nil {
		return nil, err
	}

	sections := splitSections(markdown)

	var text string
	if sectionName == "" {
		text = formatTOC(page, manSection, sections)
	} else {
		found := false
		for _, s := range sections {
			if strings.EqualFold(s.Name, sectionName) {
				text = s.Content
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("section %q not found in %s", sectionName, page)
		}
	}

	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{
			{URI: uri, MimeType: "text/plain", Text: text},
		},
	}, nil
}

func (m *manServer) ListResourceTemplates(_ context.Context) ([]protocol.ResourceTemplate, error) {
	return templates, nil
}

// ResourceProviderV1 (V1 extensions)

func (m *manServer) ListResourcesV1(_ context.Context, _ string) (*protocol.ResourcesListResultV1, error) {
	return &protocol.ResourcesListResultV1{}, nil
}

func (m *manServer) ListResourceTemplatesV1(_ context.Context, _ string) (*protocol.ResourceTemplatesListResultV1, error) {
	return &protocol.ResourceTemplatesListResultV1{ResourceTemplates: templatesV1}, nil
}

// locateSource uses man -w to find the roff source file path.
func locateSource(section, page string) (string, error) {
	var args []string
	if section != "" {
		args = []string{"-w", section, page}
	} else {
		args = []string{"-w", page}
	}

	cmd := exec.Command("man", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("man -w %s: %w", page, err)
	}

	return strings.TrimSpace(string(out)), nil
}

// renderMarkdown converts a roff source file to markdown via mandoc and pandoc.
// Pipeline: mandoc -T man <path> | pandoc -f man -t markdown
func renderMarkdown(sourcePath string) (string, error) {
	mandoc := exec.Command("mandoc", "-T", "man", sourcePath)
	pandoc := exec.Command("pandoc", "-f", "man", "-t", "markdown")

	var mandocErr bytes.Buffer
	mandoc.Stderr = &mandocErr

	pipe, err := mandoc.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("creating pipe: %w", err)
	}
	pandoc.Stdin = pipe

	var pandocOut, pandocErr bytes.Buffer
	pandoc.Stdout = &pandocOut
	pandoc.Stderr = &pandocErr

	if err := mandoc.Start(); err != nil {
		return "", fmt.Errorf("starting mandoc: %w", err)
	}
	if err := pandoc.Start(); err != nil {
		mandoc.Process.Kill()
		return "", fmt.Errorf("starting pandoc: %w", err)
	}

	mandoc.Wait()

	if err := pandoc.Wait(); err != nil {
		return "", fmt.Errorf("pandoc: %w: %s", err, pandocErr.String())
	}

	return pandocOut.String(), nil
}

// splitSections splits markdown content by # and ## headers into sections.
func splitSections(markdown string) []pageSection {
	lines := strings.Split(markdown, "\n")
	var sections []pageSection

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		var name string
		var level int

		if strings.HasPrefix(line, "## ") {
			name = strings.TrimPrefix(line, "## ")
			level = 2
		} else if strings.HasPrefix(line, "# ") {
			name = strings.TrimPrefix(line, "# ")
			level = 1
		} else {
			if len(sections) > 0 {
				sections[len(sections)-1].Content += line + "\n"
				sections[len(sections)-1].LineCount++
			}
			continue
		}

		sections = append(sections, pageSection{
			Name:  name,
			Level: level,
		})
	}

	// Trim trailing whitespace from each section's content
	for i := range sections {
		sections[i].Content = strings.TrimRight(sections[i].Content, "\n ")
	}

	return sections
}

// formatTOC produces a table of contents listing all sections with line counts.
func formatTOC(page, manSection string, sections []pageSection) string {
	var b strings.Builder

	if manSection != "" {
		fmt.Fprintf(&b, "%s(%s)\n\n", strings.ToUpper(page), manSection)
	} else {
		fmt.Fprintf(&b, "%s\n\n", strings.ToUpper(page))
	}

	for _, s := range sections {
		indent := ""
		if s.Level == 2 {
			indent = "  "
		}
		fmt.Fprintf(&b, "%s%s (%d lines)\n", indent, s.Name, s.LineCount)
	}

	return b.String()
}

// parseManURI extracts man section number, page name, and optional section name.
//
// Examples:
//
//	man://ls            → ("", "ls", "")
//	man://ls/DESCRIPTION → ("", "ls", "DESCRIPTION")
//	man://1/ls          → ("1", "ls", "")
//	man://1/ls/DESCRIPTION → ("1", "ls", "DESCRIPTION")
func parseManURI(uri string) (manSection, page, sectionName string, err error) {
	path := strings.TrimPrefix(uri, "man://")
	if path == "" {
		return "", "", "", fmt.Errorf("empty man page URI")
	}

	parts := strings.SplitN(path, "/", 3)

	switch len(parts) {
	case 1:
		// man://page
		return "", parts[0], "", nil

	case 2:
		if isManSection(parts[0]) {
			// man://1/ls
			return parts[0], parts[1], "", nil
		}
		// man://ls/DESCRIPTION
		return "", parts[0], parts[1], nil

	case 3:
		if isManSection(parts[0]) {
			// man://1/ls/DESCRIPTION
			return parts[0], parts[1], parts[2], nil
		}
		// man://page/section/... — treat as page with section name containing /
		return "", parts[0], parts[1] + "/" + parts[2], nil
	}

	return "", "", "", fmt.Errorf("invalid man page URI: %s", uri)
}

func isManSection(s string) bool {
	return len(s) <= 2 && len(s) > 0 && s[0] >= '1' && s[0] <= '9'
}

var _ server.ResourceProviderV1 = (*manServer)(nil)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	t := transport.NewStdio(os.Stdin, os.Stdout)
	m := &manServer{}

	srv, err := server.New(t, server.Options{
		ServerName:    "manpage",
		ServerVersion: "0.2.0",
		Instructions:  "Unix man page server with progressive disclosure. Use man://{page} for a table of contents, man://{page}/{section_name} to read a specific section.",
		Resources:     m,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "manpage: %v\n", err)
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "manpage: %v\n", err)
		os.Exit(1)
	}
}
