package rag

import (
	"gf-lt/config"
	"gf-lt/storage"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestRealBiblicalQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real embedder test in short mode")
	}
	// Check if the embedder model exists
	modelPath := filepath.Join("..", "onnx", "embedgemma", "model_q4.onnx")
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Skipf("embedder model not found at %s; skipping real embedder test", modelPath)
	}
	tokenizerPath := filepath.Join("..", "onnx", "embedgemma", "tokenizer.json")
	dbPath := filepath.Join("..", "gflt.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skipf("database not found at %s; skipping real embedder test", dbPath)
	}
	cfg := &config.Config{
		EmbedModelPath:     modelPath,
		EmbedTokenizerPath: tokenizerPath,
		EmbedDims:          768,
		RAGWordLimit:       250,
		RAGOverlapWords:    25,
		RAGBatchSize:       1,
	}
	logger := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
	store := storage.NewProviderSQL(dbPath, logger)
	if store == nil {
		t.Fatal("failed to create storage provider")
	}
	rag, err := New(logger, store, cfg)
	if err != nil {
		t.Fatalf("failed to create RAG instance: %v", err)
	}
	t.Cleanup(func() { rag.Destroy() })
	query := "bald prophet and two she bears"
	results, err := rag.Search(query, 30)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	found := false
	for i, row := range results {
		if row.Slug == "kjv_bible.epub_1786_0" {
			found = true
			t.Logf("target chunk found at rank %d", i+1)
			break
		}
	}
	if !found {
		t.Errorf("target chunk not found in search results for query %q", query)
		t.Logf("results slugs:")
		for i, r := range results {
			t.Logf("%d: %s", i+1, r.Slug)
		}
	}
}

func TestRealQueryVariations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real embedder test in short mode")
	}
	modelPath := filepath.Join("..", "onnx", "embedgemma", "model_q4.onnx")
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Skipf("embedder model not found at %s; skipping real embedder test", modelPath)
	}
	tokenizerPath := filepath.Join("..", "onnx", "embedgemma", "tokenizer.json")
	dbPath := filepath.Join("..", "gflt.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Skipf("database not found at %s; skipping real embedder test", dbPath)
	}
	cfg := &config.Config{
		EmbedModelPath:     modelPath,
		EmbedTokenizerPath: tokenizerPath,
		EmbedDims:          768,
		RAGWordLimit:       250,
		RAGOverlapWords:    25,
		RAGBatchSize:       1,
	}
	logger := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
	store := storage.NewProviderSQL(dbPath, logger)
	if store == nil {
		t.Fatal("failed to create storage provider")
	}
	rag, err := New(logger, store, cfg)
	if err != nil {
		t.Fatalf("failed to create RAG instance: %v", err)
	}
	t.Cleanup(func() { rag.Destroy() })
	tests := []struct {
		name  string
		query string
	}{
		{"she bears", "she bears"},
		{"bald head", "bald head"},
		{"two she bears out of the wood", "two she bears out of the wood"},
		{"bald prophet", "bald prophet"},
		{"go up thou bald head", "\"go up thou bald head\""},
		{"two she bears", "\"two she bears\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := rag.Search(tt.query, 10)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			found := false
			for _, row := range results {
				if row.Slug == "kjv_bible.epub_1786_0" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("target chunk not found for query %q", tt.query)
				for i, r := range results {
					t.Logf("%d: %s", i+1, r.Slug)
				}
			}
		})
	}
}
