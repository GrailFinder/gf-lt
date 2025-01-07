package rag

import (
	"bytes"
	"elefant/config"
	"elefant/models"
	"elefant/storage"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/neurosnap/sentences/english"
)

type RAG struct {
	logger *slog.Logger
	store  storage.FullRepo
	cfg    *config.Config
}

func New(l *slog.Logger, s storage.FullRepo, cfg *config.Config) *RAG {
	return &RAG{
		logger: l,
		store:  s,
		cfg:    cfg,
	}
}

func (r *RAG) LoadRAG(fpath string) error {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}
	r.logger.Info("rag: loaded file", "fp", fpath)
	fileText := string(data)
	tokenizer, err := english.NewSentenceTokenizer(nil)
	if err != nil {
		return err
	}
	sentences := tokenizer.Tokenize(fileText)
	sents := make([]string, len(sentences))
	r.logger.Info("rag: sentences", "#", len(sents))
	for i, s := range sentences {
		sents[i] = s.Text
	}
	// TODO: maybe better to decide batch size based on sentences len
	var (
		// TODO: to config
		workers   = 5
		batchSize = 200
		maxChSize = 1000
		//
		left     = 0
		right    = batchSize
		batchCh  = make(chan map[int][]string, maxChSize)
		vectorCh = make(chan []models.VectorRow, maxChSize)
		errCh    = make(chan error, 1)
		doneCh   = make(chan bool, 1)
	)
	if len(sents) < batchSize {
		batchSize = len(sents)
	}
	// fill input channel
	ctn := 0
	for {
		if right > len(sents) {
			batchCh <- map[int][]string{left: sents[left:]}
			break
		}
		batchCh <- map[int][]string{left: sents[left:right]}
		left, right = right, right+batchSize
		ctn++
	}
	r.logger.Info("finished batching", "batches#", len(batchCh))
	for w := 0; w < workers; w++ {
		go r.batchToVectorHFAsync(len(sents), batchCh, vectorCh, errCh, doneCh)
	}
	// write to db
	return r.writeVectors(vectorCh, doneCh)
}

func (r *RAG) writeVectors(vectorCh <-chan []models.VectorRow, doneCh <-chan bool) error {
	for {
		select {
		case batch := <-vectorCh:
			for _, vector := range batch {
				if err := r.store.WriteVector(&vector); err != nil {
					r.logger.Error("failed to write vector", "error", err, "slug", vector.Slug)
					continue // a duplicate is not critical
					// return err
				}
			}
			r.logger.Info("wrote batch to db", "size", len(batch))
		case <-doneCh:
			r.logger.Info("rag finished")
			return nil
		}
	}
}

func (r *RAG) batchToVectorHFAsync(limit int, inputCh <-chan map[int][]string,
	vectorCh chan<- []models.VectorRow, errCh chan error, doneCh chan bool) {
	r.logger.Info("to vector batches", "batches#", len(inputCh))
	for {
		select {
		case linesMap := <-inputCh:
			// r.logger.Info("batch from ch")
			for leftI, v := range linesMap {
				// r.logger.Info("fetching", "index", leftI)
				r.fecthEmbHF(v, errCh, vectorCh, fmt.Sprintf("test_%d", leftI))
				if leftI+200 >= limit { // last batch
					doneCh <- true
					return
				}
				// r.logger.Info("done feitching", "index", leftI)
			}
		case <-doneCh:
			r.logger.Info("got done")
			close(errCh)
			close(doneCh)
			return
		case err := <-errCh:
			r.logger.Error("got an error", "error", err)
			return
		}
	}
}

func (r *RAG) fecthEmbHF(lines []string, errCh chan error, vectorCh chan<- []models.VectorRow, slug string) {
	payload, err := json.Marshal(
		map[string]any{"inputs": lines, "options": map[string]bool{"wait_for_model": true}},
	)
	if err != nil {
		r.logger.Error("failed to marshal payload", "err:", err.Error())
		errCh <- err
		return
	}
	req, err := http.NewRequest("POST", r.cfg.EmbedURL, bytes.NewReader(payload))
	if err != nil {
		r.logger.Error("failed to create new req", "err:", err.Error())
		errCh <- err
		return
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.cfg.HFToken))
	resp, err := http.DefaultClient.Do(req)
	// nolint
	// resp, err := httpClient.Post(cfg.EmbedURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		r.logger.Error("failed to embedd line", "err:", err.Error())
		errCh <- err
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		r.logger.Error("non 200 resp", "code", resp.StatusCode)
		return
		// err = fmt.Errorf("non 200 resp; url: %s; code %d", r.cfg.EmbedURL, resp.StatusCode)
		// errCh <- err
		// return
	}
	emb := [][]float32{}
	if err := json.NewDecoder(resp.Body).Decode(&emb); err != nil {
		r.logger.Error("failed to embedd line", "err:", err.Error())
		errCh <- err
		return
	}
	if len(emb) == 0 {
		r.logger.Error("empty emb")
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

func (r *RAG) LineToVector(line string) ([]float32, error) {
	// payload, err := json.Marshal(map[string]string{"content": line})
	lines := []string{line}
	payload, err := json.Marshal(
		map[string]any{"inputs": lines, "options": map[string]bool{"wait_for_model": true}},
	)
	if err != nil {
		r.logger.Error("failed to marshal payload", "err:", err.Error())
		return nil, err
	}
	// nolint
	req, err := http.NewRequest("POST", r.cfg.EmbedURL, bytes.NewReader(payload))
	if err != nil {
		r.logger.Error("failed to create new req", "err:", err.Error())
		return nil, err
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", r.cfg.HFToken))
	resp, err := http.DefaultClient.Do(req)
	// resp, err := req.Post(r.cfg.EmbedURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		r.logger.Error("failed to embedd line", "err:", err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = fmt.Errorf("non 200 resp; code: %v\n", resp.StatusCode)
		r.logger.Error(err.Error())
		return nil, err
	}
	// emb := models.EmbeddingResp{}
	emb := [][]float32{}
	if err := json.NewDecoder(resp.Body).Decode(&emb); err != nil {
		r.logger.Error("failed to embedd line", "err:", err.Error())
		return nil, err
	}
	if len(emb) == 0 || len(emb[0]) == 0 {
		r.logger.Error("empty emb")
		err = errors.New("empty emb")
		return nil, err
	}
	return emb[0], nil
}

// func (r *RAG) saveLine(topic, line string, emb *models.EmbeddingResp) error {
// 	row := &models.VectorRow{
// 		Embeddings: emb.Embedding,
// 		Slug:       topic,
// 		RawText:    line,
// 	}
// 	return r.store.WriteVector(row)
// }

func (r *RAG) SearchEmb(emb *models.EmbeddingResp) ([]models.VectorRow, error) {
	return r.store.SearchClosest(emb.Embedding)
}
