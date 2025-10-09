package rag_new

import (
	"bytes"
	"gf-lt/config"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// Embedder defines the interface for embedding text
type Embedder interface {
	Embed(text []string) ([][]float32, error)
	EmbedSingle(text string) ([]float32, error)
}

// APIEmbedder implements embedder using an API (like Hugging Face, OpenAI, etc.)
type APIEmbedder struct {
	logger  *slog.Logger
	client  *http.Client
	cfg     *config.Config
}

func NewAPIEmbedder(l *slog.Logger, cfg *config.Config) *APIEmbedder {
	return &APIEmbedder{
		logger: l,
		client: &http.Client{},
		cfg:    cfg,
	}
}

func (a *APIEmbedder) Embed(text []string) ([][]float32, error) {
	payload, err := json.Marshal(
		map[string]any{"inputs": text, "options": map[string]bool{"wait_for_model": true}},
	)
	if err != nil {
		a.logger.Error("failed to marshal payload", "err", err.Error())
		return nil, err
	}

	req, err := http.NewRequest("POST", a.cfg.EmbedURL, bytes.NewReader(payload))
	if err != nil {
		a.logger.Error("failed to create new req", "err", err.Error())
		return nil, err
	}
	
	if a.cfg.HFToken != "" {
		req.Header.Add("Authorization", "Bearer "+a.cfg.HFToken)
	}
	
	resp, err := a.client.Do(req)
	if err != nil {
		a.logger.Error("failed to embed text", "err", err.Error())
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		err = fmt.Errorf("non 200 response; code: %v", resp.StatusCode)
		a.logger.Error(err.Error())
		return nil, err
	}

	var emb [][]float32
	if err := json.NewDecoder(resp.Body).Decode(&emb); err != nil {
		a.logger.Error("failed to decode embedding response", "err", err.Error())
		return nil, err
	}

	if len(emb) == 0 {
		err = fmt.Errorf("empty embedding response")
		a.logger.Error("empty embedding response")
		return nil, err
	}

	return emb, nil
}

func (a *APIEmbedder) EmbedSingle(text string) ([]float32, error) {
	result, err := a.Embed([]string{text})
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	return result[0], nil
}

// TODO: ONNXEmbedder implementation would go here
// This would require:
// 1. Loading ONNX models locally
// 2. Using a Go ONNX runtime (like gorgonia/onnx or similar)
// 3. Converting text to embeddings without external API calls
//
// For now, we'll focus on the API implementation which is already working in the current system,
// and can be extended later when we have ONNX runtime integration