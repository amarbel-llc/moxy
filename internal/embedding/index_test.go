package embedding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexSearchRanking(t *testing.T) {
	idx := NewIndex(3)
	idx.Add("similar", []float32{1, 2, 3})
	idx.Add("orthogonal", []float32{0, 0, 1})
	idx.Add("opposite", []float32{-1, -2, -3})

	results := idx.Search([]float32{1, 2, 3}, 3)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[0].Page != "similar" {
		t.Errorf("top result: got %q, want %q", results[0].Page, "similar")
	}
	if results[2].Page != "opposite" {
		t.Errorf("bottom result: got %q, want %q", results[2].Page, "opposite")
	}
}

func TestIndexSearchTopK(t *testing.T) {
	idx := NewIndex(2)
	idx.Add("a", []float32{1, 0})
	idx.Add("b", []float32{0, 1})
	idx.Add("c", []float32{1, 1})

	results := idx.Search([]float32{1, 0}, 1)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Page != "a" {
		t.Errorf("top result: got %q, want %q", results[0].Page, "a")
	}
}

func TestIndexSaveLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "index")

	idx := NewIndex(3)
	idx.Add("page-a", []float32{0.1, 0.2, 0.3})
	idx.Add("page-b", []float32{0.4, 0.5, 0.6})

	if err := idx.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadIndex(dir)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if loaded.Dim != 3 {
		t.Errorf("Dim: got %d, want 3", loaded.Dim)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("Entries: got %d, want 2", len(loaded.Entries))
	}
	if loaded.Entries[0].Page != "page-a" {
		t.Errorf("first page: got %q, want %q", loaded.Entries[0].Page, "page-a")
	}
	if len(loaded.Entries[0].Embedding) != 3 {
		t.Errorf("embedding dim: got %d, want 3", len(loaded.Entries[0].Embedding))
	}
}

func TestLoadIndexMissing(t *testing.T) {
	_, err := LoadIndex(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Error("expected error for missing index")
	}
}

func TestIndexSearchDeduplicatesByPageName(t *testing.T) {
	idx := NewIndex(2)
	// Two entries for "sed" with different embeddings
	idx.Add("sed", []float32{0.5, 0.5}) // weaker match
	idx.Add("sed", []float32{1, 0})     // stronger match for query [1,0]
	idx.Add("ls", []float32{0, 1})      // different page

	results := idx.Search([]float32{1, 0}, 10)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (deduped)", len(results))
	}
	if results[0].Page != "sed" {
		t.Errorf("top result: got %q, want %q", results[0].Page, "sed")
	}
	// The score should be from the stronger match [1,0], not the weaker [0.5,0.5]
	if results[0].Score < 0.99 {
		t.Errorf("sed score %.4f too low — dedup should keep the best match", results[0].Score)
	}
}

func TestIndexSearchEmpty(t *testing.T) {
	idx := NewIndex(3)
	results := idx.Search([]float32{1, 2, 3}, 5)
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestIndexSaveCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	idx := NewIndex(2)
	idx.Add("test", []float32{1, 0})

	if err := idx.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "pages.txt")); err != nil {
		t.Errorf("pages.txt not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "embeddings.jsonl")); err != nil {
		t.Errorf("embeddings.jsonl not created: %v", err)
	}
}
