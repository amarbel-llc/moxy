package embedding

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Entry struct {
	Page      string
	Embedding []float32
}

type Index struct {
	Entries []Entry
	Dim     int
}

type Result struct {
	Page  string
	Score float64
}

func NewIndex(dim int) *Index {
	return &Index{Dim: dim}
}

func (idx *Index) Add(page string, embedding []float32) {
	idx.Entries = append(idx.Entries, Entry{
		Page:      page,
		Embedding: embedding,
	})
}

func (idx *Index) Search(query []float32, topK int) []Result {
	results := make([]Result, 0, len(idx.Entries))
	for _, e := range idx.Entries {
		score := CosineSimilarity(query, e.Embedding)
		results = append(results, Result{Page: e.Page, Score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if topK > 0 && topK < len(results) {
		results = results[:topK]
	}

	return results
}

func (idx *Index) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating index dir: %w", err)
	}

	pagesPath := filepath.Join(dir, "pages.txt")
	embPath := filepath.Join(dir, "embeddings.jsonl")

	pf, err := os.Create(pagesPath)
	if err != nil {
		return fmt.Errorf("creating pages.txt: %w", err)
	}
	defer pf.Close()

	ef, err := os.Create(embPath)
	if err != nil {
		return fmt.Errorf("creating embeddings.jsonl: %w", err)
	}
	defer ef.Close()

	for _, e := range idx.Entries {
		fmt.Fprintln(pf, e.Page)

		data, err := json.Marshal(e.Embedding)
		if err != nil {
			return fmt.Errorf("marshaling embedding for %s: %w", e.Page, err)
		}
		fmt.Fprintln(ef, string(data))
	}

	return nil
}

func LoadIndex(dir string) (*Index, error) {
	pagesPath := filepath.Join(dir, "pages.txt")
	embPath := filepath.Join(dir, "embeddings.jsonl")

	pf, err := os.Open(pagesPath)
	if err != nil {
		return nil, fmt.Errorf("opening pages.txt: %w", err)
	}
	defer pf.Close()

	ef, err := os.Open(embPath)
	if err != nil {
		return nil, fmt.Errorf("opening embeddings.jsonl: %w", err)
	}
	defer ef.Close()

	var pages []string
	scanner := bufio.NewScanner(pf)
	for scanner.Scan() {
		page := strings.TrimSpace(scanner.Text())
		if page != "" {
			pages = append(pages, page)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading pages.txt: %w", err)
	}

	var embeddings [][]float32
	embScanner := bufio.NewScanner(ef)
	embScanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for long lines
	for embScanner.Scan() {
		var vec []float32
		if err := json.Unmarshal(embScanner.Bytes(), &vec); err != nil {
			return nil, fmt.Errorf("parsing embedding: %w", err)
		}
		embeddings = append(embeddings, vec)
	}
	if err := embScanner.Err(); err != nil {
		return nil, fmt.Errorf("reading embeddings.jsonl: %w", err)
	}

	if len(pages) != len(embeddings) {
		return nil, fmt.Errorf("page count (%d) != embedding count (%d)", len(pages), len(embeddings))
	}

	dim := 0
	if len(embeddings) > 0 {
		dim = len(embeddings[0])
	}

	idx := &Index{Dim: dim}
	for i := range pages {
		idx.Entries = append(idx.Entries, Entry{
			Page:      pages[i],
			Embedding: embeddings[i],
		})
	}

	return idx, nil
}
