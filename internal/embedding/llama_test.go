package embedding

import (
	"os"
	"testing"
)

func TestEmbedProducesNonZeroOutput(t *testing.T) {
	modelPath := os.Getenv("MANPAGE_MODEL_PATH")
	if modelPath == "" {
		t.Skip("MANPAGE_MODEL_PATH not set")
	}

	emb, err := NewEmbedder(modelPath)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	defer emb.Close()

	queryPrefix, _ := testPrefixes()
	vec, err := emb.Embed(queryPrefix + "list files in a directory")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(vec) == 0 {
		t.Fatal("embedding has zero length")
	}

	t.Logf("embedding dim: %d", len(vec))
	t.Logf("first 10 values: %v", vec[:10])

	allZero := true
	for _, v := range vec {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("embedding is all zeros")
	}
}

func TestEmbedSimilarQueriesMoreSimilar(t *testing.T) {
	modelPath := os.Getenv("MANPAGE_MODEL_PATH")
	if modelPath == "" {
		t.Skip("MANPAGE_MODEL_PATH not set")
	}

	emb, err := NewEmbedder(modelPath)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	defer emb.Close()

	queryPrefix, docPrefix := testPrefixes()

	a, err := emb.Embed(queryPrefix + "list files")
	if err != nil {
		t.Fatalf("Embed a: %v", err)
	}

	b, err := emb.Embed(docPrefix + "ls - list directory contents")
	if err != nil {
		t.Fatalf("Embed b: %v", err)
	}

	c, err := emb.Embed(docPrefix + "gcc - GNU C compiler")
	if err != nil {
		t.Fatalf("Embed c: %v", err)
	}

	simAB := CosineSimilarity(a, b)
	simAC := CosineSimilarity(a, c)

	t.Logf("similarity(list files, ls): %.4f", simAB)
	t.Logf("similarity(list files, gcc): %.4f", simAC)

	if simAB <= simAC {
		t.Errorf("expected 'list files' closer to 'ls' than 'gcc', got %.4f <= %.4f", simAB, simAC)
	}
}
