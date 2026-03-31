package embedding

import (
	"os"
	"testing"
)

// These tests verify search quality using the real embedding model.
// They document expected behavior and known gaps in ranking.

func newTestEmbedder(t *testing.T) *Embedder {
	t.Helper()
	modelPath := os.Getenv("MANPAGE_MODEL_PATH")
	if modelPath == "" {
		t.Skip("MANPAGE_MODEL_PATH not set")
	}
	emb, err := NewEmbedder(modelPath)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	t.Cleanup(emb.Close)
	return emb
}

func embedDoc(t *testing.T, emb *Embedder, text string) []float32 {
	t.Helper()
	vec, err := emb.Embed("search_document: " + text)
	if err != nil {
		t.Fatalf("Embed doc %q: %v", text, err)
	}
	return vec
}

func embedQuery(t *testing.T, emb *Embedder, text string) []float32 {
	t.Helper()
	vec, err := emb.Embed("search_query: " + text)
	if err != nil {
		t.Fatalf("Embed query %q: %v", text, err)
	}
	return vec
}

func TestSearchQualityListFiles(t *testing.T) {
	emb := newTestEmbedder(t)

	query := embedQuery(t, emb, "list files in a directory")
	ls := embedDoc(t, emb, "ls - list directory contents")
	gcc := embedDoc(t, emb, "gcc - GNU C and C++ compiler")

	simLS := CosineSimilarity(query, ls)
	simGCC := CosineSimilarity(query, gcc)

	t.Logf("similarity(list files, ls):  %.4f", simLS)
	t.Logf("similarity(list files, gcc): %.4f", simGCC)

	if simLS <= simGCC {
		t.Errorf("expected ls to rank higher than gcc: %.4f <= %.4f", simLS, simGCC)
	}
}

func TestSearchQualityStreamEditor(t *testing.T) {
	emb := newTestEmbedder(t)

	query := embedQuery(t, emb, "search and replace text in files")
	sed := embedDoc(t, emb, "sed - stream editor for filtering and transforming text")
	ls := embedDoc(t, emb, "ls - list directory contents")

	simSed := CosineSimilarity(query, sed)
	simLS := CosineSimilarity(query, ls)

	t.Logf("similarity(search and replace, sed): %.4f", simSed)
	t.Logf("similarity(search and replace, ls):  %.4f", simLS)

	if simSed <= simLS {
		t.Errorf("expected sed to rank higher than ls: %.4f <= %.4f", simSed, simLS)
	}
}

// TestSearchQualitySedVsTops documents that "sed" ranks below "tops"
// for "search and replace text in files". The synopsis alone doesn't
// mention "search" or "replace" — sed's description says "filtering
// and transforming", which the model scores lower. This is a known
// limitation of synopsis-only indexing.
func TestSearchQualitySedVsTops(t *testing.T) {
	emb := newTestEmbedder(t)

	query := embedQuery(t, emb, "search and replace text in files")
	sed := embedDoc(t, emb, "sed - stream editor for filtering and transforming text")
	tops := embedDoc(t, emb, "tops - perform in-place substitutions on source files")

	simSed := CosineSimilarity(query, sed)
	simTops := CosineSimilarity(query, tops)

	t.Logf("similarity(search and replace, sed):  %.4f", simSed)
	t.Logf("similarity(search and replace, tops): %.4f", simTops)

	// tops describes "substitutions on source files" which is closer
	// to "search and replace text in files" than sed's "filtering and
	// transforming text". This is expected given synopsis-only input.
	if simTops <= simSed {
		t.Skipf("tops no longer outranks sed — model or synopsis may have changed")
	}
}

func TestSearchQualityNetworkDownload(t *testing.T) {
	emb := newTestEmbedder(t)

	query := embedQuery(t, emb, "download files from the internet")
	curl := embedDoc(t, emb, "curl - transfer a URL")
	ls := embedDoc(t, emb, "ls - list directory contents")

	simCurl := CosineSimilarity(query, curl)
	simLS := CosineSimilarity(query, ls)

	t.Logf("similarity(download, curl): %.4f", simCurl)
	t.Logf("similarity(download, ls):   %.4f", simLS)

	if simCurl <= simLS {
		t.Errorf("expected curl to rank higher than ls: %.4f <= %.4f", simCurl, simLS)
	}
}

func TestSearchQualityProcessManagement(t *testing.T) {
	emb := newTestEmbedder(t)

	query := embedQuery(t, emb, "kill a running process")
	kill := embedDoc(t, emb, "kill - terminate or signal a process")
	cat := embedDoc(t, emb, "cat - concatenate and print files")

	simKill := CosineSimilarity(query, kill)
	simCat := CosineSimilarity(query, cat)

	t.Logf("similarity(kill process, kill): %.4f", simKill)
	t.Logf("similarity(kill process, cat):  %.4f", simCat)

	if simKill <= simCat {
		t.Errorf("expected kill to rank higher than cat: %.4f <= %.4f", simKill, simCat)
	}
}
