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
	"path"
	"strings"
	"sync"

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

func wordCounter(sentence string) int {
	return len(strings.Split(sentence, " "))
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
		batchSize = 100
		maxChSize = 1000
		//
		// psize     = 3
		wordLimit = 80
		//
		left     = 0
		right    = batchSize
		batchCh  = make(chan map[int][]string, maxChSize)
		vectorCh = make(chan []models.VectorRow, maxChSize)
		errCh    = make(chan error, 1)
		doneCh   = make(chan bool, 1)
		lock     = new(sync.Mutex)
	)
	defer close(doneCh)
	defer close(errCh)
	defer close(batchCh)
	// group sentences
	paragraphs := []string{}
	par := strings.Builder{}
	for i := 0; i < len(sents); i++ {
		par.WriteString(sents[i])
		if wordCounter(par.String()) > wordLimit {
			paragraphs = append(paragraphs, par.String())
			par.Reset()
		}
	}
	// for i := 0; i < len(sents); i += psize {
	// 	if len(sents) < i+psize {
	// 		paragraphs = append(paragraphs, strings.Join(sents[i:], " "))
	// 		break
	// 	}
	// 	paragraphs = append(paragraphs, strings.Join(sents[i:i+psize], " "))
	// }
	if len(paragraphs) < batchSize {
		batchSize = len(paragraphs)
	}
	// fill input channel
	ctn := 0
	for {
		if right > len(paragraphs) {
			batchCh <- map[int][]string{left: paragraphs[left:]}
			break
		}
		batchCh <- map[int][]string{left: paragraphs[left:right]}
		left, right = right, right+batchSize
		ctn++
	}
	r.logger.Info("finished batching", "batches#", len(batchCh), "paragraphs", len(paragraphs), "sentences", len(sents))
	for w := 0; w < workers; w++ {
		go r.batchToVectorHFAsync(lock, w, batchCh, vectorCh, errCh, doneCh, path.Base(fpath))
	}
	// wait for emb to be done
	<-doneCh
	// write to db
	return r.writeVectors(vectorCh)
}

func (r *RAG) writeVectors(vectorCh chan []models.VectorRow) error {
	for {
		for batch := range vectorCh {
			for _, vector := range batch {
				if err := r.store.WriteVector(&vector); err != nil {
					r.logger.Error("failed to write vector", "error", err, "slug", vector.Slug)
					continue // a duplicate is not critical
					// return err
				}
			}
			r.logger.Info("wrote batch to db", "size", len(batch), "vector_chan_len", len(vectorCh))
			if len(vectorCh) == 0 {
				r.logger.Info("finished writing vectors")
				defer close(vectorCh)
				return nil
			}
		}
	}
}

func (r *RAG) batchToVectorHFAsync(lock *sync.Mutex, id int, inputCh <-chan map[int][]string,
	vectorCh chan<- []models.VectorRow, errCh chan error, doneCh chan bool, filename string) {
	for {
		lock.Lock()
		if len(inputCh) == 0 {
			if len(doneCh) == 0 {
				doneCh <- true
			}
			lock.Unlock()
			return
		}
		select {
		case linesMap := <-inputCh:
			for leftI, v := range linesMap {
				r.fecthEmbHF(v, errCh, vectorCh, fmt.Sprintf("%s_%d", filename, leftI), filename)
				// if leftI+200 >= limit { // last batch
				// 	// doneCh <- true
				// 	return
				// }
			}
			lock.Unlock()
		case err := <-errCh:
			r.logger.Error("got an error", "error", err)
			lock.Unlock()
			return
		}
		r.logger.Info("to vector batches", "batches#", len(inputCh), "worker#", id)
	}
}

func (r *RAG) fecthEmbHF(lines []string, errCh chan error, vectorCh chan<- []models.VectorRow, slug, filename string) {
	payload, err := json.Marshal(
		map[string]any{"inputs": lines, "options": map[string]bool{"wait_for_model": true}},
	)
	if err != nil {
		r.logger.Error("failed to marshal payload", "err:", err.Error())
		errCh <- err
		return
	}
	// nolint
	req, err := http.NewRequest("POST", r.cfg.EmbedURL, bytes.NewReader(payload))
	if err != nil {
		r.logger.Error("failed to create new req", "err:", err.Error())
		errCh <- err
		return
	}
	req.Header.Add("Authorization", "Bearer "+r.cfg.HFToken)
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
			Slug:       fmt.Sprintf("%s_%d", slug, i),
			FileName:   filename,
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
	req.Header.Add("Authorization", "Bearer "+r.cfg.HFToken)
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

func (r *RAG) SearchEmb(emb *models.EmbeddingResp) ([]models.VectorRow, error) {
	return r.store.SearchClosest(emb.Embedding)
}

func (r *RAG) ListLoaded() ([]string, error) {
	return r.store.ListFiles()
}

func (r *RAG) RemoveFile(filename string) error {
	return r.store.RemoveEmbByFileName(filename)
}
