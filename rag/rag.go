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
	mu       sync.Mutex
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
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}
	r.logger.Debug("rag: loaded file", "fp", fpath)
	select {
	case LongJobStatusCh <- LoadedFileRAGStatus:
	default:
		r.logger.Warn("LongJobStatusCh channel is full or closed, dropping status message", "message", LoadedFileRAGStatus)
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
		if strings.TrimSpace(sents[i]) != "" {
			if par.Len() > 0 {
				par.WriteString(" ")
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
	// Process paragraphs in batches synchronously
	batchCount := 0
	for i := 0; i < len(paragraphs); i += r.cfg.RAGBatchSize {
		end := i + r.cfg.RAGBatchSize
		if end > len(paragraphs) {
			end = len(paragraphs)
		}
		batch := paragraphs[i:end]
		batchCount++
		// Filter empty paragraphs
		nonEmptyBatch := make([]string, 0, len(batch))
		for _, p := range batch {
			if strings.TrimSpace(p) != "" {
				nonEmptyBatch = append(nonEmptyBatch, strings.TrimSpace(p))
			}
		}
		if len(nonEmptyBatch) == 0 {
			continue
		}
		// Embed the batch
		embeddings, err := r.embedder.EmbedSlice(nonEmptyBatch)
		if err != nil {
			r.logger.Error("failed to embed batch", "error", err, "batch", batchCount)
			select {
			case LongJobStatusCh <- ErrRAGStatus:
			default:
				r.logger.Warn("LongJobStatusCh channel full, dropping message")
			}
			return fmt.Errorf("failed to embed batch %d: %w", batchCount, err)
		}
		if len(embeddings) != len(nonEmptyBatch) {
			err := errors.New("embedding count mismatch")
			r.logger.Error("embedding mismatch", "expected", len(nonEmptyBatch), "got", len(embeddings))
			return err
		}
		// Write vectors to storage
		filename := path.Base(fpath)
		for j, text := range nonEmptyBatch {
			vector := models.VectorRow{
				Embeddings: embeddings[j],
				RawText:    text,
				Slug:       fmt.Sprintf("%s_%d_%d", filename, batchCount, j),
				FileName:   filename,
			}
			if err := r.storage.WriteVector(&vector); err != nil {
				r.logger.Error("failed to write vector to DB", "error", err, "slug", vector.Slug)
				select {
				case LongJobStatusCh <- ErrRAGStatus:
				default:
					r.logger.Warn("LongJobStatusCh channel full, dropping message")
				}
				return fmt.Errorf("failed to write vector: %w", err)
			}
		}
		r.logger.Debug("wrote batch to db", "batch", batchCount, "size", len(nonEmptyBatch))
		// Send progress status
		statusMsg := fmt.Sprintf("processed batch %d/%d", batchCount, (len(paragraphs)+r.cfg.RAGBatchSize-1)/r.cfg.RAGBatchSize)
		select {
		case LongJobStatusCh <- statusMsg:
		default:
			r.logger.Warn("LongJobStatusCh channel full, dropping message")
		}
	}
	r.logger.Debug("finished writing vectors", "batches", batchCount)
	select {
	case LongJobStatusCh <- FinishedRAGStatus:
	default:
		r.logger.Warn("LongJobStatusCh channel is full or closed, dropping status message", "message", FinishedRAGStatus)
	}
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
