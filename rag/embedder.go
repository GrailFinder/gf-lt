package rag

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"gf-lt/config"
	"gf-lt/models"
	"log/slog"
	"net/http"
)

// Embedder defines the interface for embedding text
type Embedder interface {
	Embed(text string) ([]float32, error)
	EmbedSlice(lines []string) ([][]float32, error)
}

// APIEmbedder implements embedder using an API (like Hugging Face, OpenAI, etc.)
type APIEmbedder struct {
	logger *slog.Logger
	client *http.Client
	cfg    *config.Config
}

func NewAPIEmbedder(l *slog.Logger, cfg *config.Config) *APIEmbedder {
	return &APIEmbedder{
		logger: l,
		client: &http.Client{},
		cfg:    cfg,
	}
}

func (a *APIEmbedder) Embed(text string) ([]float32, error) {
	payload, err := json.Marshal(
		map[string]any{"input": text, "encoding_format": "float"},
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
	embResp := &models.LCPEmbedResp{}
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		a.logger.Error("failed to decode embedding response", "err", err.Error())
		return nil, err
	}
	if len(embResp.Data) == 0 || len(embResp.Data[0].Embedding) == 0 {
		err = errors.New("empty embedding response")
		a.logger.Error("empty embedding response")
		return nil, err
	}
	return embResp.Data[0].Embedding, nil
}

func (a *APIEmbedder) EmbedSlice(lines []string) ([][]float32, error) {
	payload, err := json.Marshal(
		map[string]any{"input": lines, "encoding_format": "float"},
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
	embResp := &models.LCPEmbedResp{}
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		a.logger.Error("failed to decode embedding response", "err", err.Error())
		return nil, err
	}
	if len(embResp.Data) == 0 {
		err = errors.New("empty embedding response")
		a.logger.Error("empty embedding response")
		return nil, err
	}

	// Collect all embeddings from the response
	embeddings := make([][]float32, len(embResp.Data))
	for i := range embResp.Data {
		if len(embResp.Data[i].Embedding) == 0 {
			err = fmt.Errorf("empty embedding at index %d", i)
			a.logger.Error("empty embedding", "index", i)
			return nil, err
		}
		embeddings[i] = embResp.Data[i].Embedding
	}

	// Sort embeddings by index to match the order of input lines
	// API responses may not be in order
	for _, data := range embResp.Data {
		if data.Index >= len(embeddings) || data.Index < 0 {
			err = fmt.Errorf("invalid embedding index %d", data.Index)
			a.logger.Error("invalid embedding index", "index", data.Index)
			return nil, err
		}
		embeddings[data.Index] = data.Embedding
	}

	return embeddings, nil
}

// TODO: ONNXEmbedder implementation would go here
// This would require:
// 1. Loading ONNX models locally
// 2. Using a Go ONNX runtime (like gorgonia/onnx or similar)
// 3. Converting text to embeddings without external API calls
//
// For now, we'll focus on the API implementation which is already working in the current system,
// and can be extended later when we have ONNX runtime integration
