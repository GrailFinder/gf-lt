package rag

import (
	"testing"

	"gf-lt/models"
)

// SearchKeyword must return distances on the same scale as SearchClosest
// (1 - cosine similarity, roughly [0,2]) so that RerankResults can compare
// vector hits and keyword hits directly. Before normalization, FTS5 bm25 was
// stored raw: negative and unbounded, which made every keyword hit outrank
// every vector hit regardless of semantic quality.
func TestNormalizeBM25DistancesBand(t *testing.T) {
	rows := []models.VectorRow{
		{Slug: "strong", Distance: -12.0},
		{Slug: "medium", Distance: -6.0},
		{Slug: "weak", Distance: -1.2},
	}
	normalizeBM25Distances(rows)

	for _, r := range rows {
		if r.Distance < 0 || r.Distance >= 1 {
			t.Errorf("%s: distance %v outside [0,1)", r.Slug, r.Distance)
		}
	}
	// The best hit in the set becomes a perfect match.
	if rows[0].Distance != 0 {
		t.Errorf("best hit distance = %v, want 0", rows[0].Distance)
	}
	// Monotone in relevance: more negative bm25 must stay closer.
	for i := 1; i < len(rows); i++ {
		if rows[i].Distance < rows[i-1].Distance {
			t.Errorf("not monotone: %s(%v) sorted before %s(%v)",
				rows[i-1].Slug, rows[i-1].Distance, rows[i].Slug, rows[i].Distance)
		}
	}
}

func TestNormalizeBM25DistancesDegenerate(t *testing.T) {
	cases := []struct {
		name string
		in   []float32
	}{
		{"all zero", []float32{0, 0}},
		{"all positive", []float32{1, 2}},
		{"single zero", []float32{0}},
	}
	for _, tc := range cases {
		rows := make([]models.VectorRow, len(tc.in))
		for i, d := range tc.in {
			rows[i] = models.VectorRow{Slug: string(rune('a' + i)), Distance: d}
		}
		normalizeBM25Distances(rows)
		// No relevance signal must not read as a perfect match.
		for _, r := range rows {
			if r.Distance != 1 {
				t.Errorf("%s: %s distance = %v, want 1", tc.name, r.Slug, r.Distance)
			}
		}
	}
}

func TestNormalizeBM25DistancesEmpty(t *testing.T) {
	normalizeBM25Distances(nil) // must not panic
	normalizeBM25Distances([]models.VectorRow{})
}
