package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/amarbel-llc/moxy/internal/embedding"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/server"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/transport"
)

type manServer struct {
	mu        sync.Mutex
	embedder  *embedding.Embedder
	index     *embedding.Index
	modelName string
	modelCfg  ModelConfig
}

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
	{
		URITemplate: "man://search/{query}",
		Name:        "Semantic man page search",
		Description: "Search for man pages by natural language query. Returns ranked results with page names and scores. Use query parameter top_k to control result count (default 10).",
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
	{
		URITemplate: "man://search/{query}",
		Name:        "Semantic man page search",
		Description: "Search for man pages by natural language query. Returns ranked results with page names and scores. Use query parameter top_k to control result count (default 10).",
		MimeType:    "text/plain",
	},
}

// ResourceProvider (base interface)

func (m *manServer) ListResources(_ context.Context) ([]protocol.Resource, error) {
	return nil, nil
}

func (m *manServer) ReadResource(_ context.Context, uri string) (*protocol.ResourceReadResult, error) {
	// Check for search URI first
	if query, topK, ok := parseSearchURI(uri); ok {
		text, err := m.handleSearch(query, topK)
		if err != nil {
			return nil, err
		}
		return &protocol.ResourceReadResult{
			Contents: []protocol.ResourceContent{
				{URI: uri, MimeType: "text/plain", Text: text},
			},
		}, nil
	}

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

// Search

func (m *manServer) handleSearch(query string, topK int) (string, error) {
	if err := m.ensureSearchReady(); err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	queryText := m.modelCfg.QueryPrefix + query
	queryEmb, err := m.embedder.Embed(queryText)
	if err != nil {
		return "", fmt.Errorf("embedding query: %w", err)
	}

	results := m.index.Search(queryEmb, topK)

	var b strings.Builder
	fmt.Fprintf(&b, "Search results for %q (%d matches):\n\n", query, len(results))
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s (score: %.4f)\n", i+1, r.Page, r.Score)
	}

	return b.String(), nil
}

func (m *manServer) ensureSearchReady() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.embedder != nil && m.index != nil {
		return nil
	}

	if m.modelCfg.Path == "" {
		name, cfg, err := loadActiveModel()
		if err != nil {
			return err
		}
		m.modelName = name
		m.modelCfg = cfg
	}

	if m.embedder == nil {
		emb, err := embedding.NewEmbedder(m.modelCfg.Path)
		if err != nil {
			return fmt.Errorf("loading embedding model: %w", err)
		}
		m.embedder = emb
	}

	if m.index == nil {
		idx, err := m.loadOrBuildIndex()
		if err != nil {
			return fmt.Errorf("building search index: %w", err)
		}
		m.index = idx
	}

	return nil
}

func (m *manServer) loadOrBuildIndex() (*embedding.Index, error) {
	cacheDir := indexCacheDirForModel(m.modelName)

	idx, err := embedding.LoadIndex(cacheDir)
	if err == nil {
		fmt.Fprintf(os.Stderr, "maneater: loaded search index (%d entries) from %s\n", len(idx.Entries), cacheDir)
		return idx, nil
	}

	fmt.Fprintf(os.Stderr, "maneater: building search index...\n")

	ensureTldrCache()

	pages, err := listManPages()
	if err != nil {
		return nil, err
	}

	idx = embedding.NewIndex(m.embedder.EmbeddingDim())
	tldrCount := 0

	for i, page := range pages {
		synopsis := extractSynopsis(page)
		if synopsis != "" {
			docText := m.modelCfg.DocumentPrefix + synopsis
			emb, err := m.embedder.Embed(docText)
			if err != nil {
				fmt.Fprintf(os.Stderr, "maneater: skipping %s synopsis: %v\n", page, err)
			} else {
				idx.Add(page, emb)
			}
		}

		tldr := extractTldr(page)
		if tldr != "" {
			docText := m.modelCfg.DocumentPrefix + tldr
			emb, err := m.embedder.Embed(docText)
			if err != nil {
				fmt.Fprintf(os.Stderr, "maneater: skipping %s tldr: %v\n", page, err)
			} else {
				idx.Add(page, emb)
				tldrCount++
			}
		}

		if (i+1)%100 == 0 {
			fmt.Fprintf(os.Stderr, "maneater: indexed %d / %d pages\n", i+1, len(pages))
		}
	}

	fmt.Fprintf(os.Stderr, "maneater: indexed %d entries (%d pages, %d with tldr)\n", len(idx.Entries), len(pages), tldrCount)

	if err := idx.Save(cacheDir); err != nil {
		fmt.Fprintf(os.Stderr, "maneater: warning: could not cache index: %v\n", err)
	}

	return idx, nil
}

func indexCacheDirForModel(modelName string) string {
	var base string
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		base = filepath.Join(xdg, "moxy", "man-index")
	} else {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache", "moxy", "man-index")
	}
	return filepath.Join(base, modelName)
}

func listManPages() ([]string, error) {
	manpath, err := resolveManpath()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var pages []string

	for _, dir := range manpath {
		man1 := filepath.Join(dir, "man1")
		entries, err := os.ReadDir(man1)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			// Strip .1, .1.gz, etc.
			name = strings.TrimSuffix(name, ".gz")
			if ext := filepath.Ext(name); ext == ".1" {
				name = strings.TrimSuffix(name, ext)
			} else {
				continue
			}
			if name != "" && !seen[name] {
				seen[name] = true
				pages = append(pages, name)
			}
		}
	}

	sort.Strings(pages)
	return pages, nil
}

func resolveManpath() ([]string, error) {
	// manpath(1) resolves MANPATH, /etc/man.conf, and platform defaults
	cmd := exec.Command("manpath")
	out, err := cmd.Output()
	if err != nil {
		// Fallback: common default
		return []string{"/usr/share/man", "/usr/local/share/man"}, nil
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, fmt.Errorf("manpath returned empty")
	}
	return strings.Split(raw, ":"), nil
}

// extractSynopsis extracts NAME+SYNOPSIS+DESCRIPTION content from a man page,
// truncated to 500 chars. Returns empty string on failure.
func extractSynopsis(page string) string {
	sourcePath, err := locateSource("", page)
	if err != nil {
		return ""
	}

	markdown, err := renderMarkdown(sourcePath)
	if err != nil {
		return ""
	}

	sections := splitSections(markdown)

	var synopsis strings.Builder
	for _, s := range sections {
		upper := strings.ToUpper(strings.TrimSpace(s.Name))
		if upper == "NAME" || upper == "SYNOPSIS" || upper == "DESCRIPTION" {
			synopsis.WriteString(s.Content)
			synopsis.WriteString("\n")
		}
	}

	text := synopsis.String()
	if len(text) > 500 {
		text = text[:500]
	}

	return strings.TrimSpace(text)
}

// extractTldr reads the raw tldr markdown for a page and extracts the
// description and example descriptions, truncated to 500 chars.
// Returns empty string if no tldr page exists.
func extractTldr(page string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	cacheBase := filepath.Join(home, ".cache", "tldr", "pages")
	var content []byte
	// Prefer osx-specific pages, fall back to common
	for _, platform := range []string{"osx", "common"} {
		path := filepath.Join(cacheBase, platform, page+".md")
		data, err := os.ReadFile(path)
		if err == nil {
			content = data
			break
		}
	}
	if content == nil {
		return ""
	}

	var b strings.Builder
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			// Page name header
			b.WriteString(line[2:])
			b.WriteString(" - ")
		} else if strings.HasPrefix(line, "> ") {
			text := line[2:]
			// Skip "More information:" and "See also:" lines
			if strings.HasPrefix(text, "More information:") {
				continue
			}
			b.WriteString(text)
			b.WriteString(" ")
		} else if strings.HasPrefix(line, "- ") {
			// Example description
			b.WriteString(line[2:])
			b.WriteString(" ")
		}
		// Skip code blocks (lines starting with `) and blank lines
	}

	text := strings.TrimSpace(b.String())
	if len(text) > 500 {
		text = text[:500]
	}
	return text
}

// ensureTldrCache runs tldr -u if the cache directory doesn't exist.
func ensureTldrCache() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	cacheDir := filepath.Join(home, ".cache", "tldr", "pages")
	if _, err := os.Stat(cacheDir); err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "maneater: updating tldr cache...\n")
	cmd := exec.Command("tldr", "-u")
	cmd.Stderr = os.Stderr
	cmd.Run()
}

// parseSearchURI checks if uri is man://search/{query}[?top_k=N]
// and returns the query, top_k, and whether it matched.
func parseSearchURI(uri string) (query string, topK int, ok bool) {
	path := strings.TrimPrefix(uri, "man://")

	// Split query params
	queryPart := ""
	if idx := strings.Index(path, "?"); idx >= 0 {
		queryPart = path[idx+1:]
		path = path[:idx]
	}

	if !strings.HasPrefix(path, "search/") {
		return "", 0, false
	}

	query = strings.TrimPrefix(path, "search/")
	if decoded, err := url.PathUnescape(query); err == nil {
		query = decoded
	}

	if query == "" {
		return "", 0, false
	}

	topK = 10
	if queryPart != "" {
		if params, err := url.ParseQuery(queryPart); err == nil {
			if v := params.Get("top_k"); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					topK = n
				}
			}
		}
	}

	return query, topK, true
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
	flag.Parse()

	if flag.NArg() < 1 {
		printUsage()
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "serve":
		if flag.NArg() < 2 || flag.Arg(1) != "mcp" {
			fmt.Fprintf(os.Stderr, "usage: maneater serve mcp\n")
			os.Exit(1)
		}
		runServeMCP()
	case "index":
		runIndex()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "usage: maneater <command>\n\n")
	fmt.Fprintf(os.Stderr, "commands:\n")
	fmt.Fprintf(os.Stderr, "  serve mcp    run as MCP server\n")
	fmt.Fprintf(os.Stderr, "  index        build/rebuild search index\n")
}

func runServeMCP() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	t := transport.NewStdio(os.Stdin, os.Stdout)
	m := &manServer{}

	srv, err := server.New(t, server.Options{
		ServerName:    "maneater",
		ServerVersion: "0.4.0",
		Instructions:  "Unix man page server with progressive disclosure and semantic search. Use man://{page} for a table of contents, man://{page}/{section_name} to read a specific section, man://search/{query} to find pages by natural language.",
		Resources:     m,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "maneater: %v\n", err)
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "maneater: %v\n", err)
		os.Exit(1)
	}
}

func runIndex() {
	modelName, modelCfg, err := loadActiveModel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "maneater: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Using model %q from %s\n", modelName, modelCfg.Path)

	emb, err := embedding.NewEmbedder(modelCfg.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "maneater: loading model: %v\n", err)
		os.Exit(1)
	}
	defer emb.Close()

	fmt.Println("Updating tldr cache...")
	ensureTldrCache()

	fmt.Println("Listing man pages...")
	pages, err := listManPages()
	if err != nil {
		fmt.Fprintf(os.Stderr, "maneater: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Found %d man pages\n", len(pages))

	idx := embedding.NewIndex(emb.EmbeddingDim())
	tldrCount := 0

	for i, page := range pages {
		synopsis := extractSynopsis(page)
		if synopsis != "" {
			docText := modelCfg.DocumentPrefix + synopsis
			vec, err := emb.Embed(docText)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  skipping %s synopsis: %v\n", page, err)
			} else {
				idx.Add(page, vec)
			}
		}

		tldr := extractTldr(page)
		if tldr != "" {
			docText := modelCfg.DocumentPrefix + tldr
			vec, err := emb.Embed(docText)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  skipping %s tldr: %v\n", page, err)
			} else {
				idx.Add(page, vec)
				tldrCount++
			}
		}

		if (i+1)%100 == 0 || i+1 == len(pages) {
			fmt.Printf("Indexed %d / %d pages\n", i+1, len(pages))
		}
	}

	cacheDir := indexCacheDirForModel(modelName)
	if err := idx.Save(cacheDir); err != nil {
		fmt.Fprintf(os.Stderr, "maneater: saving index: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Done: %d entries (%d pages, %d with tldr) saved to %s\n", len(idx.Entries), len(pages), tldrCount, cacheDir)
}
