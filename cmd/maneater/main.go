package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/amarbel-llc/moxy/internal/embedding"
	"github.com/amarbel-llc/purse-first/libs/go-mcp/command"
)

type pageSection struct {
	Name      string
	Level     int // 1 = top-level (#), 2 = subsection (##)
	Content   string
	LineCount int
}

// searcher holds state for the embedding-based search pipeline.
type searcher struct {
	mu        sync.Mutex
	embedder  *embedding.Embedder
	index     *embedding.Index
	modelName string
	modelCfg  ModelConfig
	manpath   []string
}

func newApp() *command.App {
	app := command.NewApp("maneater", "Man page search index and semantic search CLI")
	app.Version = "0.6.0"
	app.Description.Long = "Maneater builds and queries a semantic search index over Unix man pages " +
		"using vector embeddings. It extracts synopses and tldr descriptions, embeds " +
		"them with nomic-embed-text-v1.5, and supports ranked search by natural language query."

	app.Examples = []command.Example{
		{
			Description: "Build or rebuild the search index",
			Command:     "maneater index",
		},
		{
			Description: "Search for man pages about listing files",
			Command:     "maneater search list files",
		},
		{
			Description: "Search with custom result count",
			Command:     "maneater search --top-k 20 configure network",
		},
	}

	app.AddCommand(&command.Command{
		Name: "index",
		Description: command.Description{
			Short: "Build or rebuild the search index",
			Long: "Loads the embedding model, scans all man pages on the manpath, " +
				"extracts synopses and tldr descriptions, embeds them, and saves " +
				"the index to the XDG cache directory.",
		},
		RunCLI: func(_ context.Context, _ json.RawMessage) error {
			return runIndex()
		},
	})

	app.AddCommand(&command.Command{
		Name: "search",
		Description: command.Description{
			Short: "Semantic man page search",
			Long: "Search for man pages by natural language query. Returns ranked " +
				"results with page names and cosine similarity scores. Requires a " +
				"pre-built index (maneater index).",
		},
		Params: []command.Param{
			{
				Name:        "top-k",
				Description: "Number of results to return",
				Type:        command.Int,
			},
			{
				Name:        "query",
				Description: "Natural language search query",
				Type:        command.String,
				Required:    true,
			},
		},
		RunCLI: func(_ context.Context, args json.RawMessage) error {
			return runSearch(os.Args[2:])
		},
	})

	return app
}

func main() {
	app := newApp()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := app.RunCLI(ctx, os.Args[1:], command.StubPrompter{}); err != nil {
		fmt.Fprintf(os.Stderr, "maneater: %v\n", err)
		os.Exit(1)
	}
}

func runSearch(args []string) error {
	topK := 10
	var queryParts []string

	for i := 0; i < len(args); i++ {
		if args[i] == "--top-k" && i+1 < len(args) {
			n, err := strconv.Atoi(args[i+1])
			if err != nil {
				return fmt.Errorf("invalid --top-k value: %s", args[i+1])
			}
			topK = n
			i++
		} else {
			queryParts = append(queryParts, args[i])
		}
	}

	query := strings.Join(queryParts, " ")
	if query == "" {
		return fmt.Errorf("usage: maneater search <query> [--top-k N]")
	}

	cfg, err := LoadDefaultManeaterHierarchy()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	manpath, err := resolveManpath(cfg.Manpath, cwd)
	if err != nil {
		return err
	}

	s := &searcher{manpath: manpath}
	result, err := s.handleSearch(query, topK)
	if err != nil {
		return err
	}

	fmt.Print(result)
	return nil
}

func (s *searcher) handleSearch(query string, topK int) (string, error) {
	if err := s.ensureSearchReady(); err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	queryText := s.modelCfg.QueryPrefix + query
	queryEmb, err := s.embedder.Embed(queryText)
	if err != nil {
		return "", fmt.Errorf("embedding query: %w", err)
	}

	results := s.index.Search(queryEmb, topK)

	var b strings.Builder
	fmt.Fprintf(&b, "Search results for %q (%d matches):\n\n", query, len(results))
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s (score: %.4f)\n", i+1, r.Page, r.Score)
	}

	return b.String(), nil
}

func (s *searcher) ensureSearchReady() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.embedder != nil && s.index != nil {
		return nil
	}

	if s.modelCfg.Path == "" {
		name, cfg, err := loadActiveModel()
		if err != nil {
			return err
		}
		s.modelName = name
		s.modelCfg = cfg
	}

	if s.embedder == nil {
		emb, err := embedding.NewEmbedder(s.modelCfg.Path)
		if err != nil {
			return fmt.Errorf("loading embedding model: %w", err)
		}
		s.embedder = emb
	}

	if s.index == nil {
		idx, err := s.loadOrBuildIndex()
		if err != nil {
			return fmt.Errorf("building search index: %w", err)
		}
		s.index = idx
	}

	return nil
}

func (s *searcher) loadOrBuildIndex() (*embedding.Index, error) {
	cacheDir := indexCacheDirForModel(s.modelName)

	idx, err := embedding.LoadIndex(cacheDir)
	if err == nil {
		fmt.Fprintf(os.Stderr, "maneater: loaded search index (%d entries) from %s\n", len(idx.Entries), cacheDir)
		return idx, nil
	}

	fmt.Fprintf(os.Stderr, "maneater: building search index...\n")

	ensureTldrCache()

	pages, err := listManPages(s.manpath)
	if err != nil {
		return nil, err
	}

	idx, stats := buildIndex(s.embedder, s.modelCfg, pages, s.manpath, os.Stderr)

	fmt.Fprintf(os.Stderr, "maneater: indexed %d entries (%d pages, %d with tldr)\n",
		len(idx.Entries), len(pages), stats.tldrCount)

	if err := idx.Save(cacheDir); err != nil {
		fmt.Fprintf(os.Stderr, "maneater: warning: could not cache index: %v\n", err)
	}

	return idx, nil
}

type pageText struct {
	index    int
	page     string
	synopsis string
	tldr     string
}

type indexStats struct {
	tldrCount int
}

func buildIndex(emb *embedding.Embedder, cfg ModelConfig, pages []string, manpath []string, logw *os.File) (*embedding.Index, indexStats) {
	// Extract text concurrently, embed serially.
	// Workers run mandoc|pandoc pipelines in parallel; results are
	// sent in order to the main goroutine for embedding.
	texts := make(chan pageText, 32)

	go func() {
		defer close(texts)

		type indexed struct {
			pt  pageText
			seq int
		}

		workers := 8
		sem := make(chan struct{}, workers)
		results := make(chan indexed, 32)

		go func() {
			defer close(results)
			var wg sync.WaitGroup
			for i, page := range pages {
				wg.Add(1)
				sem <- struct{}{}
				go func(seq int, page string) {
					defer wg.Done()
					defer func() { <-sem }()
					results <- indexed{
						pt: pageText{
							index:    seq,
							page:     page,
							synopsis: extractSynopsis(manpath, page),
							tldr:     extractTldr(page),
						},
						seq: seq,
					}
				}(i, page)
			}
			wg.Wait()
		}()

		// Reorder results to preserve deterministic index order.
		pending := make(map[int]pageText)
		next := 0
		for r := range results {
			pending[r.seq] = r.pt
			for {
				pt, ok := pending[next]
				if !ok {
					break
				}
				delete(pending, next)
				texts <- pt
				next++
			}
		}
	}()

	idx := embedding.NewIndex(emb.EmbeddingDim())
	var stats indexStats

	for pt := range texts {
		if pt.synopsis != "" {
			docText := cfg.DocumentPrefix + pt.synopsis
			vec, err := emb.Embed(docText)
			if err != nil {
				fmt.Fprintf(logw, "maneater: skipping %s synopsis: %v\n", pt.page, err)
			} else {
				idx.Add(pt.page, vec)
			}
		}

		if pt.tldr != "" {
			docText := cfg.DocumentPrefix + pt.tldr
			vec, err := emb.Embed(docText)
			if err != nil {
				fmt.Fprintf(logw, "maneater: skipping %s tldr: %v\n", pt.page, err)
			} else {
				idx.Add(pt.page, vec)
				stats.tldrCount++
			}
		}

		if (pt.index+1)%100 == 0 {
			fmt.Fprintf(logw, "maneater: indexed %d / %d pages\n", pt.index+1, len(pages))
		}
	}

	return idx, stats
}

func indexCacheDirForModel(modelName string) string {
	var base string
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		base = filepath.Join(xdg, "maneater", "man-index")
	} else {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache", "maneater", "man-index")
	}
	return filepath.Join(base, modelName)
}

// indexedSections are the man sections scanned for the search index.
var indexedSections = []string{"1", "2", "3", "4", "5", "6", "7", "8"}

func listManPages(manpath []string) ([]string, error) {
	seen := make(map[string]bool)
	var pages []string

	for _, dir := range manpath {
		for _, sec := range indexedSections {
			manDir := filepath.Join(dir, "man"+sec)
			entries, err := os.ReadDir(manDir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				name = strings.TrimSuffix(name, ".gz")
				if ext := filepath.Ext(name); ext == "."+sec {
					name = strings.TrimSuffix(name, ext)
				} else {
					continue
				}
				key := name + "(" + sec + ")"
				if name != "" && !seen[key] {
					seen[key] = true
					pages = append(pages, key)
				}
			}
		}
	}

	sort.Strings(pages)
	return pages, nil
}

// parsePageKey splits "page(section)" into page and section.
// Returns page and empty section if no parens found.
func parsePageKey(key string) (string, string) {
	if i := strings.LastIndex(key, "("); i >= 0 && strings.HasSuffix(key, ")") {
		return key[:i], key[i+1 : len(key)-1]
	}
	return key, ""
}

// heuristicManDirs are common in-repo locations for man pages, probed in order.
var heuristicManDirs = []string{"man", "doc/man", "share/man"}

func resolveManpath(mpCfg *ManpathConfig, cwd string) ([]string, error) {
	// System manpath via manpath(1).
	systemPaths, err := systemManpath()
	if err != nil {
		return nil, err
	}

	var prepend []string

	// Heuristic: probe common in-repo locations in cwd.
	if mpCfg == nil || !mpCfg.NoAuto {
		for _, rel := range heuristicManDirs {
			candidate := filepath.Join(cwd, rel)
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				prepend = append(prepend, candidate)
			}
		}
	}

	// Config include paths.
	if mpCfg != nil {
		for _, inc := range mpCfg.Include {
			inc = os.ExpandEnv(inc)
			if !filepath.IsAbs(inc) {
				inc = filepath.Join(cwd, inc)
			}
			prepend = append(prepend, inc)
		}
	}

	return append(prepend, systemPaths...), nil
}

func systemManpath() ([]string, error) {
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
func extractSynopsis(manpath []string, page string) string {
	name, section := parsePageKey(page)
	sourcePath, err := locateSource(manpath, section, name)
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
	name, _ := parsePageKey(page)

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	cacheBase := filepath.Join(home, ".cache", "tldr", "pages")
	var content []byte
	// Prefer osx-specific pages, fall back to common
	for _, platform := range []string{"osx", "common"} {
		path := filepath.Join(cacheBase, platform, name+".md")
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

// locateSource finds the roff source file by scanning manpath directories.
// This avoids a dependency on man-db's "man -w" — only mandoc is needed.
func locateSource(manpath []string, section, page string) (string, error) {
	// If section is specified, only search that section dir; otherwise search
	// common sections in priority order.
	sections := []string{"1", "8", "5", "7", "6", "2", "3", "4"}
	if section != "" {
		sections = []string{section}
	}

	for _, dir := range manpath {
		for _, sec := range sections {
			manDir := filepath.Join(dir, "man"+sec)
			for _, ext := range []string{".gz", ""} {
				candidate := filepath.Join(manDir, page+"."+sec+ext)
				if _, err := os.Stat(candidate); err == nil {
					return candidate, nil
				}
			}
		}
	}

	return "", fmt.Errorf("no manual entry for %s", page)
}

// renderMarkdown converts a roff source file to markdown via mandoc and pandoc.
// Pipeline: mandoc -T man <path> | pandoc -f man -t markdown
//
// If the mandoc pipeline fails (e.g. asciidoctor-generated roff that mandoc
// transforms into something pandoc can't parse), falls back to feeding the raw
// roff directly to pandoc.
func renderMarkdown(sourcePath string) (string, error) {
	result, err := renderMarkdownViaMandoc(sourcePath)
	if err == nil {
		return result, nil
	}

	return renderMarkdownDirect(sourcePath)
}

func renderMarkdownViaMandoc(sourcePath string) (string, error) {
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

// renderMarkdownDirect feeds roff source directly to pandoc, decompressing
// gzipped files first.
func renderMarkdownDirect(sourcePath string) (string, error) {
	var reader io.Reader

	f, err := os.Open(sourcePath)
	if err != nil {
		return "", fmt.Errorf("opening source: %w", err)
	}
	defer f.Close()

	if strings.HasSuffix(sourcePath, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", fmt.Errorf("decompressing source: %w", err)
		}
		defer gz.Close()
		reader = gz
	} else {
		reader = f
	}

	pandoc := exec.Command("pandoc", "-f", "man", "-t", "markdown")
	pandoc.Stdin = reader

	var pandocOut, pandocErr bytes.Buffer
	pandoc.Stdout = &pandocOut
	pandoc.Stderr = &pandocErr

	if err := pandoc.Run(); err != nil {
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

func runIndex() error {
	modelName, modelCfg, err := loadActiveModel()
	if err != nil {
		return err
	}

	cfg, err := LoadDefaultManeaterHierarchy()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	manpath, err := resolveManpath(cfg.Manpath, cwd)
	if err != nil {
		return err
	}

	fmt.Printf("Using model %q from %s\n", modelName, modelCfg.Path)

	emb, err := embedding.NewEmbedder(modelCfg.Path)
	if err != nil {
		return fmt.Errorf("loading model: %w", err)
	}
	defer emb.Close()

	fmt.Println("Updating tldr cache...")
	ensureTldrCache()

	fmt.Println("Listing man pages...")
	pages, err := listManPages(manpath)
	if err != nil {
		return err
	}
	fmt.Printf("Found %d man pages\n", len(pages))

	idx, stats := buildIndex(emb, modelCfg, pages, manpath, os.Stderr)

	cacheDir := indexCacheDirForModel(modelName)
	if err := idx.Save(cacheDir); err != nil {
		return fmt.Errorf("saving index: %w", err)
	}

	fmt.Printf("Done: %d entries (%d pages, %d with tldr) saved to %s\n",
		len(idx.Entries), len(pages), stats.tldrCount, cacheDir)
	return nil
}
