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

	// Note: Vector tables are created via database migrations, not at runtime

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
	select {
	case LongJobStatusCh <- LoadedFileRAGStatus:
	default:
		r.logger.Warn("LongJobStatusCh channel is full or closed, dropping status message", "message", LoadedFileRAGStatus)
		// Channel is full or closed, ignore the message to prevent panic
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
	if len(paragraphs) < r.cfg.RAGBatchSize && len(paragraphs) > 0 {
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
		doneCh    = make(chan struct{})
		wg        = new(sync.WaitGroup)
	)

	defer close(doneCh)
	defer close(errCh)
	defer close(batchCh)

	// Fill input channel with batches
	ctn := 0
	totalParagraphs := len(paragraphs)
	for {
		if right > totalParagraphs {
			batchCh <- map[int][]string{left: paragraphs[left:]}
			break
		}
		batchCh <- map[int][]string{left: paragraphs[left:right]}
		left, right = right, right+r.cfg.RAGBatchSize
		ctn++
	}

	finishedBatchesMsg := fmt.Sprintf("finished batching batches#: %d; paragraphs: %d; sentences: %d\n", ctn+1, len(paragraphs), len(sents))
	r.logger.Debug(finishedBatchesMsg)
	select {
	case LongJobStatusCh <- finishedBatchesMsg:
	default:
		r.logger.Warn("LongJobStatusCh channel is full or closed, dropping status message", "message", finishedBatchesMsg)
		// Channel is full or closed, ignore the message to prevent panic
	}

	// Start worker goroutines with WaitGroup
	wg.Add(int(r.cfg.RAGWorkers))
	for w := 0; w < int(r.cfg.RAGWorkers); w++ {
		go func(workerID int) {
			defer wg.Done()
			r.batchToVectorAsync(workerID, batchCh, vectorCh, errCh, doneCh, path.Base(fpath))
		}(w)
	}

	// Close batchCh to signal workers no more data is coming
	close(batchCh)

	// Wait for all workers to finish, then close vectorCh
	go func() {
		wg.Wait()
		close(vectorCh)
	}()

	// Check for errors from workers - this will block until an error occurs or all workers finish
	select {
	case err := <-errCh:
		if err != nil {
			r.logger.Error("error during RAG processing", "error", err)
			return err
		}
	default:
		// No immediate error, continue
	}

	// Write vectors to storage - this will block until vectorCh is closed
	return r.writeVectors(vectorCh, errCh)
}

func (r *RAG) writeVectors(vectorCh chan []models.VectorRow, errCh chan error) error {
	// Use a select to handle both vectorCh and errCh
	for {
		select {
		case err := <-errCh:
			if err != nil {
				r.logger.Error("error during RAG processing in writeVectors", "error", err)
				return err
			}
		case batch, ok := <-vectorCh:
			if !ok {
				r.logger.Debug("vector channel closed, finished writing vectors")
				select {
				case LongJobStatusCh <- FinishedRAGStatus:
				default:
					r.logger.Warn("LongJobStatusCh channel is full or closed, dropping status message", "message", FinishedRAGStatus)
				}
				return nil
			}
			for _, vector := range batch {
				if err := r.storage.WriteVector(&vector); err != nil {
					r.logger.Error("failed to write vector to DB", "error", err, "slug", vector.Slug)
					select {
					case LongJobStatusCh <- ErrRAGStatus:
					default:
						r.logger.Warn("LongJobStatusCh channel is full or closed, dropping status message", "message", ErrRAGStatus)
					}
					return err
				}
			}
			r.logger.Debug("wrote batch to db", "size", len(batch))
		}
	}
}

func (r *RAG) batchToVectorAsync(id int, inputCh <-chan map[int][]string,
	vectorCh chan<- []models.VectorRow, errCh chan error, doneCh <-chan struct{}, filename string) {
	var err error

	defer func() {
		if err != nil {
			select {
			case errCh <- err:
			default:
				r.logger.Warn("errCh channel full or closed, skipping error propagation", "worker", id, "error", err)
			}
		}
	}()

	for {
		select {
		case <-doneCh:
			r.logger.Debug("worker received done signal", "worker", id)
			return
		case linesMap, ok := <-inputCh:
			if !ok {
				r.logger.Debug("input channel closed, worker exiting", "worker", id)
				return
			}
			for leftI, lines := range linesMap {
				select {
				case <-doneCh:
					return
				default:
				}
				if err := r.fetchEmb(lines, errCh, vectorCh, fmt.Sprintf("%s_%d", filename, leftI), filename); err != nil {
					r.logger.Error("error fetching embeddings", "error", err, "worker", id)
					return
				}
			}
			r.logger.Debug("processed batch", "worker#", id)
			statusMsg := fmt.Sprintf("converted to vector; worker#: %d", id)
			select {
			case LongJobStatusCh <- statusMsg:
			default:
				r.logger.Warn("LongJobStatusCh channel full or closed, dropping status message", "message", statusMsg)
			}
		}
	}
}

func (r *RAG) fetchEmb(lines []string, errCh chan error, vectorCh chan<- []models.VectorRow, slug, filename string) error {
	// Filter out empty lines before sending to embedder
	nonEmptyLines := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			nonEmptyLines = append(nonEmptyLines, trimmed)
		}
	}

	// Skip if no non-empty lines
	if len(nonEmptyLines) == 0 {
		// Send empty result but don't error
		vectorCh <- []models.VectorRow{}
		return nil
	}

	embeddings, err := r.embedder.EmbedSlice(nonEmptyLines)
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

	if len(embeddings) != len(nonEmptyLines) {
		err := errors.New("mismatch between number of lines and embeddings returned")
		r.logger.Error("embedding mismatch", "err", err.Error())
		errCh <- err
		return err
	}

	// Create a VectorRow for each line in the batch
	vectors := make([]models.VectorRow, len(nonEmptyLines))
	for i, line := range nonEmptyLines {
		vectors[i] = models.VectorRow{
			Embeddings: embeddings[i],
			RawText:    line,
			Slug:       fmt.Sprintf("%s_%d", slug, i),
			FileName:   filename,
		}
	}

	vectorCh <- vectors
	return nil
}

func (r *RAG) LineToVector(line string) ([]float32, error) {
	return r.embedder.Embed(line)
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
