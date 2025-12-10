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
		wg        = new(sync.WaitGroup)
		lock      = new(sync.Mutex)
	)

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
			r.batchToVectorAsync(lock, workerID, batchCh, vectorCh, errCh, path.Base(fpath))
		}(w)
	}

	// Use a goroutine to close the batchCh when all batches are sent
	go func() {
		wg.Wait()
		close(vectorCh) // Close vectorCh when all workers are done
	}()

	// Check for errors from workers
	// Use a non-blocking check for errors
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
	return r.writeVectors(vectorCh)
}

func (r *RAG) writeVectors(vectorCh chan []models.VectorRow) error {
	for {
		for batch := range vectorCh {
			for _, vector := range batch {
				if err := r.storage.WriteVector(&vector); err != nil {
					r.logger.Error("failed to write vector to DB", "error", err, "slug", vector.Slug)
					select {
					case LongJobStatusCh <- ErrRAGStatus:
					default:
						r.logger.Warn("LongJobStatusCh channel is full or closed, dropping status message", "message", ErrRAGStatus)
						// Channel is full or closed, ignore the message to prevent panic
					}
					return err // Stop the entire RAG operation on DB error
				}
			}
			r.logger.Debug("wrote batch to db", "size", len(batch), "vector_chan_len", len(vectorCh))
			if len(vectorCh) == 0 {
				r.logger.Debug("finished writing vectors")
				select {
				case LongJobStatusCh <- FinishedRAGStatus:
				default:
					r.logger.Warn("LongJobStatusCh channel is full or closed, dropping status message", "message", FinishedRAGStatus)
					// Channel is full or closed, ignore the message to prevent panic
				}
				return nil
			}
		}
	}
}

func (r *RAG) batchToVectorAsync(lock *sync.Mutex, id int, inputCh <-chan map[int][]string,
	vectorCh chan<- []models.VectorRow, errCh chan error, filename string) {
	var err error

	defer func() {
		// For errCh, make sure we only send if there's actually an error and the channel can accept it
		if err != nil {
			select {
			case errCh <- err:
			default:
				// errCh might be full or closed, log but don't panic
				r.logger.Warn("errCh channel full or closed, skipping error propagation", "worker", id, "error", err)
			}
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
		case err = <-errCh:
			r.logger.Error("got an error from error channel", "error", err)
			lock.Unlock()
			return
		default:
			lock.Unlock()
		}

		r.logger.Debug("processed batch", "batches#", len(inputCh), "worker#", id)
		statusMsg := fmt.Sprintf("converted to vector; batches: %d, worker#: %d", len(inputCh), id)
		select {
		case LongJobStatusCh <- statusMsg:
		default:
			r.logger.Warn("LongJobStatusCh channel full or closed, dropping status message", "message", statusMsg)
			// Channel is full or closed, ignore the message to prevent panic
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
