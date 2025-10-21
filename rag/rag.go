package rag

import (
	"errors"
	"fmt"
	"gf-lt/config"
	"gf-lt/models"
	"gf-lt/storage"
	"log/slog"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/neurosnap/sentences/english"
)

var (
	// Status messages for TUI integration
	LongJobStatusCh     = make(chan string, 10) // Increased buffer size to prevent blocking
	FinishedRAGStatus   = "finished loading RAG file; press Enter"
	LoadedFileRAGStatus = "loaded file"
	ErrRAGStatus        = "some error occurred; failed to transfer data to vector db"
)

type RAG struct {
	logger   *slog.Logger
	store    storage.FullRepo
	cfg      *config.Config
	embedder Embedder
	storage  *VectorStorage
}

func New(l *slog.Logger, s storage.FullRepo, cfg *config.Config) *RAG {
	// Initialize with API embedder by default, could be configurable later
	embedder := NewAPIEmbedder(l, cfg)

	rag := &RAG{
		logger:   l,
		store:    s,
		cfg:      cfg,
		embedder: embedder,
		storage:  NewVectorStorage(l, s),
	}

	// Create the necessary tables
	if err := rag.storage.CreateTables(); err != nil {
		l.Error("failed to create vector tables", "error", err)
	}

	return rag
}

func wordCounter(sentence string) int {
	return len(strings.Split(strings.TrimSpace(sentence), " "))
}

func (r *RAG) LoadRAG(fpath string) error {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}
	r.logger.Debug("rag: loaded file", "fp", fpath)
	LongJobStatusCh <- LoadedFileRAGStatus

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

	// Group sentences into paragraphs based on word limit
	paragraphs := []string{}
	par := strings.Builder{}
	for i := 0; i < len(sents); i++ {
		// Only add sentences that aren't empty
		if strings.TrimSpace(sents[i]) != "" {
			if par.Len() > 0 {
				par.WriteString(" ") // Add space between sentences
			}
			par.WriteString(sents[i])
		}

		if wordCounter(par.String()) > int(r.cfg.RAGWordLimit) {
			paragraph := strings.TrimSpace(par.String())
			if paragraph != "" {
				paragraphs = append(paragraphs, paragraph)
			}
			par.Reset()
		}
	}

	// Handle any remaining content in the paragraph buffer
	if par.Len() > 0 {
		paragraph := strings.TrimSpace(par.String())
		if paragraph != "" {
			paragraphs = append(paragraphs, paragraph)
		}
	}

	// Adjust batch size if needed
	if len(paragraphs) < int(r.cfg.RAGBatchSize) && len(paragraphs) > 0 {
		r.cfg.RAGBatchSize = len(paragraphs)
	}

	if len(paragraphs) == 0 {
		return errors.New("no valid paragraphs found in file")
	}

	var (
		maxChSize = 100
		left      = 0
		right     = r.cfg.RAGBatchSize
		batchCh   = make(chan map[int][]string, maxChSize)
		vectorCh  = make(chan []models.VectorRow, maxChSize)
		errCh     = make(chan error, 1)
		doneCh    = make(chan bool, 1)
		lock      = new(sync.Mutex)
	)

	defer close(doneCh)
	defer close(errCh)
	defer close(batchCh)

	// Fill input channel with batches
	ctn := 0
	totalParagraphs := len(paragraphs)
	for {
		if int(right) > totalParagraphs {
			batchCh <- map[int][]string{left: paragraphs[left:]}
			break
		}
		batchCh <- map[int][]string{left: paragraphs[left:right]}
		left, right = right, right+r.cfg.RAGBatchSize
		ctn++
	}

	finishedBatchesMsg := fmt.Sprintf("finished batching batches#: %d; paragraphs: %d; sentences: %d\n", ctn+1, len(paragraphs), len(sents))
	r.logger.Debug(finishedBatchesMsg)
	LongJobStatusCh <- finishedBatchesMsg

	// Start worker goroutines
	for w := 0; w < int(r.cfg.RAGWorkers); w++ {
		go r.batchToVectorAsync(lock, w, batchCh, vectorCh, errCh, doneCh, path.Base(fpath))
	}

	// Wait for embedding to be done
	<-doneCh

	// Write vectors to storage
	return r.writeVectors(vectorCh)
}

func (r *RAG) writeVectors(vectorCh chan []models.VectorRow) error {
	for {
		for batch := range vectorCh {
			for _, vector := range batch {
				if err := r.storage.WriteVector(&vector); err != nil {
					r.logger.Error("failed to write vector", "error", err, "slug", vector.Slug)
					LongJobStatusCh <- ErrRAGStatus
					continue // a duplicate is not critical
				}
			}
			r.logger.Debug("wrote batch to db", "size", len(batch), "vector_chan_len", len(vectorCh))
			if len(vectorCh) == 0 {
				r.logger.Debug("finished writing vectors")
				LongJobStatusCh <- FinishedRAGStatus
				return nil
			}
		}
	}
}

func (r *RAG) batchToVectorAsync(lock *sync.Mutex, id int, inputCh <-chan map[int][]string,
	vectorCh chan<- []models.VectorRow, errCh chan error, doneCh chan bool, filename string) {
	defer func() {
		if len(doneCh) == 0 {
			doneCh <- true
		}
	}()

	for {
		lock.Lock()
		if len(inputCh) == 0 {
			lock.Unlock()
			return
		}

		select {
		case linesMap := <-inputCh:
			for leftI, lines := range linesMap {
				if err := r.fetchEmb(lines, errCh, vectorCh, fmt.Sprintf("%s_%d", filename, leftI), filename); err != nil {
					r.logger.Error("error fetching embeddings", "error", err, "worker", id)
					lock.Unlock()
					return
				}
			}
			lock.Unlock()
		case err := <-errCh:
			r.logger.Error("got an error from error channel", "error", err)
			lock.Unlock()
			return
		default:
			lock.Unlock()
		}

		r.logger.Debug("processed batch", "batches#", len(inputCh), "worker#", id)
		LongJobStatusCh <- fmt.Sprintf("converted to vector; batches: %d, worker#: %d", len(inputCh), id)
	}
}

func (r *RAG) fetchEmb(lines []string, errCh chan error, vectorCh chan<- []models.VectorRow, slug, filename string) error {
	embeddings, err := r.embedder.Embed(lines)
	if err != nil {
		r.logger.Error("failed to embed lines", "err", err.Error())
		errCh <- err
		return err
	}

	if len(embeddings) == 0 {
		err := errors.New("no embeddings returned")
		r.logger.Error("empty embeddings")
		errCh <- err
		return err
	}

	vectors := make([]models.VectorRow, len(embeddings))
	for i, emb := range embeddings {
		vector := models.VectorRow{
			Embeddings: emb,
			RawText:    lines[i],
			Slug:       fmt.Sprintf("%s_%d", slug, i),
			FileName:   filename,
		}
		vectors[i] = vector
	}

	vectorCh <- vectors
	return nil
}

func (r *RAG) LineToVector(line string) ([]float32, error) {
	return r.embedder.EmbedSingle(line)
}

func (r *RAG) SearchEmb(emb *models.EmbeddingResp) ([]models.VectorRow, error) {
	return r.storage.SearchClosest(emb.Embedding)
}

func (r *RAG) ListLoaded() ([]string, error) {
	return r.storage.ListFiles()
}

func (r *RAG) RemoveFile(filename string) error {
	return r.storage.RemoveEmbByFileName(filename)
}

