package main

import (
	"bytes"
	"elefant/models"
	"encoding/json"
)

func lineToVector(line string) (*models.EmbeddingResp, error) {
	payload, err := json.Marshal(map[string]string{"content": line})
	if err != nil {
		logger.Error("failed to marshal payload", "err:", err.Error())
		return nil, err
	}
	resp, err := httpClient.Post(cfg.EmbedURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		logger.Error("failed to embedd line", "err:", err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	emb := models.EmbeddingResp{}
	if err := json.NewDecoder(resp.Body).Decode(&emb); err != nil {
		logger.Error("failed to embedd line", "err:", err.Error())
		return nil, err
	}
	return &emb, nil
}

func saveLine(topic, line string, emb *models.EmbeddingResp) error {
	row := &models.VectorRow{
		Embeddings: emb.Embedding,
		Slug:       topic,
		RawText:    line,
	}
	return store.WriteVector(row)
}

func searchEmb(emb *models.EmbeddingResp) (*models.VectorRow, error) {
	return store.SearchClosest([5120]float32(emb.Embedding))
}
