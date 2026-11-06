package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
)

const manURIPrefix = "man://"

// ManPageSubdir is where, under a moxin's directory, its rendered pages live:
// one <name>.<section>.txt file per page, rendered to plain text (moxy#501).
const ManPageSubdir = "share/moxy/man"

const manPageMime = "text/plain"

var manPageFile = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_+-]*(?:\.[A-Za-z0-9_+-]+)*\.[1-9][a-z]*)\.txt$`)

// manPageProvider serves man://<name>.<section>: the pages the served moxins
// ship. The index is built from directory listings; a read looks the name up
// in it and never joins caller input onto a path, so it opens only files the
// moxins installed.
type manPageProvider struct {
	mu    sync.RWMutex
	pages map[string]string // page name -> file
}

func newManPageProvider(dirs []string) *manPageProvider {
	m := &manPageProvider{}
	m.setDirs(dirs)
	return m
}

// setDirs rebuilds the index. A page name shipped by two directories keeps
// the first one's file; a directory that does not exist contributes nothing.
func (m *manPageProvider) setDirs(dirs []string) {
	pages := map[string]string{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			match := manPageFile.FindStringSubmatch(e.Name())
			if match == nil {
				continue
			}
			if _, taken := pages[match[1]]; taken {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
				continue
			}
			pages[match[1]] = path
		}
	}
	m.mu.Lock()
	m.pages = pages
	m.mu.Unlock()
}

func (m *manPageProvider) names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.pages))
	for name := range m.pages {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (m *manPageProvider) ReadResource(_ context.Context, uri string) (*protocol.ResourceReadResult, error) {
	name := strings.TrimPrefix(uri, manURIPrefix)
	m.mu.RLock()
	path, ok := m.pages[name]
	m.mu.RUnlock()
	if !ok {
		return m.missing(uri, name)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", uri, err)
	}
	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{{URI: uri, MimeType: manPageMime, Text: string(body)}},
	}, nil
}

// missing answers a name outside the index with the pages that are in it.
func (m *manPageProvider) missing(uri, name string) (*protocol.ResourceReadResult, error) {
	pages := m.names()
	uris := make([]string, len(pages))
	for i, p := range pages {
		uris[i] = manURIPrefix + p
	}
	data, _ := json.Marshal(struct {
		Error string   `json:"error"`
		Pages []string `json:"pages"`
	}{
		Error: fmt.Sprintf("no page %q among the served moxins' pages", name),
		Pages: uris,
	})
	return &protocol.ResourceReadResult{
		Contents: []protocol.ResourceContent{{URI: uri, MimeType: "application/json", Text: string(data)}},
	}, nil
}

func (m *manPageProvider) ListResources(_ context.Context) []protocol.ResourceV1 {
	var out []protocol.ResourceV1
	for _, name := range m.names() {
		out = append(out, protocol.ResourceV1{
			URI:      manURIPrefix + name,
			Name:     name,
			MimeType: manPageMime,
		})
	}
	return out
}

func (m *manPageProvider) ListResourceTemplates(_ context.Context) []protocol.ResourceTemplateV1 {
	return []protocol.ResourceTemplateV1{{
		URITemplate: "man://{page}",
		Name:        "Manual page",
		Description: "A manual page a served moxin ships, e.g. man://toolbox-wc.1, rendered as plain text",
		MimeType:    manPageMime,
	}}
}
