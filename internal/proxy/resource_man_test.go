package proxy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePage(t *testing.T, dir, file, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// moxy#501: man://<name>.<section> reads a page a served moxin ships, from an
// index built once from the moxins' page directories.
func TestManPageProviderReadsIndexedPages(t *testing.T) {
	toolbox := t.TempDir()
	writePage(t, toolbox, "toolbox-wc.1.txt", "TOOLBOX-WC(1)\n")
	writePage(t, toolbox, "toolbox.1.txt", "TOOLBOX(1)\n")
	writePage(t, toolbox, "README", "not a page")
	m := newManPageProvider([]string{toolbox, filepath.Join(t.TempDir(), "missing")})
	ctx := context.Background()

	res, err := m.ReadResource(ctx, "man://toolbox-wc.1")
	if err != nil {
		t.Fatalf("read man://toolbox-wc.1: %v", err)
	}
	if got := res.Contents[0]; got.Text != "TOOLBOX-WC(1)\n" || got.MimeType != "text/plain" || got.URI != "man://toolbox-wc.1" {
		t.Errorf("man://toolbox-wc.1 = %+v", got)
	}

	var listed []string
	for _, r := range m.ListResources(ctx) {
		listed = append(listed, r.URI)
	}
	if strings.Join(listed, " ") != "man://toolbox-wc.1 man://toolbox.1" {
		t.Errorf("listed %v, want the two pages in name order", listed)
	}
	if tmpl := m.ListResourceTemplates(ctx); len(tmpl) != 1 || tmpl[0].URITemplate != "man://{page}" {
		t.Errorf("templates = %+v", tmpl)
	}
}

// A page name is looked up, never joined onto a directory: names outside the
// index, and names shaped like paths, are refused even when the file exists.
func TestManPageProviderRefusesNamesOutsideIndex(t *testing.T) {
	root := t.TempDir()
	pages := filepath.Join(root, "pages")
	writePage(t, pages, "toolbox.1.txt", "page")
	writePage(t, root, "secret.1.txt", "outside the page directory")
	m := newManPageProvider([]string{pages})
	ctx := context.Background()

	for _, uri := range []string{
		"man://secret.1",
		"man://../secret.1",
		"man://" + filepath.Join(root, "secret.1"),
		"man://toolbox.1.txt",
		"man://",
	} {
		res, err := m.ReadResource(ctx, uri)
		if err == nil && strings.Contains(res.Contents[0].Text, "outside the page directory") {
			t.Errorf("%s read a file outside the index", uri)
		}
		if err == nil && !strings.Contains(res.Contents[0].Text, "man://toolbox.1") {
			t.Errorf("%s: a miss should hint at the indexed pages, got %q", uri, res.Contents[0].Text)
		}
	}
}

// Two moxins shipping the same page name: the first directory wins, so the
// index does not depend on map order.
func TestManPageProviderFirstDirectoryWins(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writePage(t, first, "dup.1.txt", "first")
	writePage(t, second, "dup.1.txt", "second")
	m := newManPageProvider([]string{first, second})

	res, err := m.ReadResource(context.Background(), "man://dup.1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Contents[0].Text != "first" {
		t.Errorf("man://dup.1 = %q, want the first directory's page", res.Contents[0].Text)
	}
}
