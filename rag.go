package main

import (
	"bytes"
	"context"
	"elefant/models"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/neurosnap/sentences/english"
)

func loadRAG(fpath string) error {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}
	fileText := string(data)
	tokenizer, err := english.NewSentenceTokenizer(nil)
	if err != nil {
		return err
	}
	sentences := tokenizer.Tokenize(fileText)
	sents := make([]string, len(sentences))
	for i, s := range sentences {
		sents[i] = s.Text
	}
	var (
		// TODO: to config
		workers   = 5
		batchSize = 200
		//
		left     = 0
		right    = batchSize
		batchCh  = make(chan map[int][]string)
		vectorCh = make(chan []models.VectorRow)
		errCh    = make(chan error)
	)
	if len(sents) < batchSize {
		batchSize = len(sents)
	}
	// fill input channel
	for {
		if right > len(sents) {
			batchCh <- map[int][]string{left: sents[left:]}
			break
		}
		batchCh <- map[int][]string{left: sents[left:right]}
		left, right = right, right+batchSize
	}
	// TODO: cancel complains, replace ctx with done chan
	ctx, cancel := context.WithCancel(context.Background())
	for w := 0; w < workers; w++ {
		go batchToVectorHFAsync(ctx, cancel, len(sents), batchCh, vectorCh, errCh)
	}
	// write to db
	return writeVectors(vectorCh)
}

func writeVectors(vectorCh <-chan []models.VectorRow) error {
	for batch := range vectorCh {
		for _, vector := range batch {
			if err := store.WriteVector(&vector); err != nil {
				return err
			}
		}
	}
	return nil
}

func batchToVectorHFAsync(ctx context.Context, close context.CancelFunc, limit int,
	inputCh <-chan map[int][]string, vectorCh chan<- []models.VectorRow, errCh chan error) {
	for {
		select {
		case linesMap := <-inputCh:
			for leftI, v := range linesMap {
				FecthEmbHF(v, errCh, vectorCh, fmt.Sprintf("test_%d", leftI))
				if leftI+200 >= limit { // last batch
					close()
					return
				}
			}
		case <-ctx.Done():
			logger.Error("got ctx done")
			return
		case err := <-errCh:
			logger.Error("got an error", "error", err)
			close()
			return
		}
	}
}

func FecthEmbHF(lines []string, errCh chan error, vectorCh chan<- []models.VectorRow, slug string) {
	payload, err := json.Marshal(
		map[string]any{"inputs": lines, "options": map[string]bool{"wait_for_model": true}},
	)
	if err != nil {
		logger.Error("failed to marshal payload", "err:", err.Error())
		errCh <- err
		return
	}
	req, err := http.NewRequest("POST", cfg.EmbedURL, bytes.NewReader(payload))
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", cfg.HFToken))
	resp, err := httpClient.Do(req)
	// nolint
	// resp, err := httpClient.Post(cfg.EmbedURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		logger.Error("failed to embedd line", "err:", err.Error())
		errCh <- err
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		logger.Error("non 200 resp", "code", resp.StatusCode)
		errCh <- err
		return
	}
	emb := [][]float32{}
	if err := json.NewDecoder(resp.Body).Decode(&emb); err != nil {
		logger.Error("failed to embedd line", "err:", err.Error())
		errCh <- err
		return
	}
	if len(emb) == 0 {
		logger.Error("empty emb")
		err = errors.New("empty emb")
		errCh <- err
		return
	}
	vectors := make([]models.VectorRow, len(emb))
	for i, e := range emb {
		vector := models.VectorRow{
			Embeddings: e,
			RawText:    lines[i],
			Slug:       slug,
		}
		vectors[i] = vector
	}
	vectorCh <- vectors
}

func batchToVectorHF(lines []string) ([][]float32, error) {
	payload, err := json.Marshal(
		map[string]any{"inputs": lines, "options": map[string]bool{"wait_for_model": true}},
	)
	if err != nil {
		logger.Error("failed to marshal payload", "err:", err.Error())
		return nil, err
	}
	req, err := http.NewRequest("POST", cfg.EmbedURL, bytes.NewReader(payload))
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", cfg.HFToken))
	resp, err := httpClient.Do(req)
	// nolint
	// resp, err := httpClient.Post(cfg.EmbedURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		logger.Error("failed to embedd line", "err:", err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		logger.Error("non 200 resp", "code", resp.StatusCode)
		return nil, err
	}
	emb := [][]float32{}
	if err := json.NewDecoder(resp.Body).Decode(&emb); err != nil {
		logger.Error("failed to embedd line", "err:", err.Error())
		return nil, err
	}
	if len(emb) == 0 {
		logger.Error("empty emb")
		err = errors.New("empty emb")
		return nil, err
	}
	return emb, nil
}

func lineToVector(line string) (*models.EmbeddingResp, error) {
	payload, err := json.Marshal(map[string]string{"content": line})
	if err != nil {
		logger.Error("failed to marshal payload", "err:", err.Error())
		return nil, err
	}
	// nolint
	resp, err := httpClient.Post(cfg.EmbedURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		logger.Error("failed to embedd line", "err:", err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		logger.Error("non 200 resp", "code", resp.StatusCode)
		return nil, err
	}
	emb := models.EmbeddingResp{}
	if err := json.NewDecoder(resp.Body).Decode(&emb); err != nil {
		logger.Error("failed to embedd line", "err:", err.Error())
		return nil, err
	}
	if len(emb.Embedding) == 0 {
		logger.Error("empty emb")
		err = errors.New("empty emb")
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
	return store.SearchClosest(emb.Embedding)
}
