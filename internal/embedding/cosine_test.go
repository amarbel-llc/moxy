package embedding

import (
	"math"
	"testing"
)

func TestCosineSimilarityIdentical(t *testing.T) {
	a := []float32{1, 2, 3}
	score := CosineSimilarity(a, a)
	if math.Abs(score-1.0) > 1e-6 {
		t.Errorf("identical vectors: got %f, want 1.0", score)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	score := CosineSimilarity(a, b)
	if math.Abs(score) > 1e-6 {
		t.Errorf("orthogonal vectors: got %f, want 0.0", score)
	}
}

func TestCosineSimilarityOpposite(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	score := CosineSimilarity(a, b)
	if math.Abs(score+1.0) > 1e-6 {
		t.Errorf("opposite vectors: got %f, want -1.0", score)
	}
}

func TestCosineSimilarityEmpty(t *testing.T) {
	score := CosineSimilarity(nil, nil)
	if score != 0 {
		t.Errorf("empty vectors: got %f, want 0.0", score)
	}
}

func TestCosineSimilarityMismatchedLengths(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{1, 2, 3}
	score := CosineSimilarity(a, b)
	if score != 0 {
		t.Errorf("mismatched lengths: got %f, want 0.0", score)
	}
}

func TestCosineSimilarityZeroVector(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	score := CosineSimilarity(a, b)
	if score != 0 {
		t.Errorf("zero vector: got %f, want 0.0", score)
	}
}
