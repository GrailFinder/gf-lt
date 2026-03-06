package rag

import (
	"context"
	"errors"
	"fmt"
	"gf-lt/config"
	"gf-lt/models"
	"gf-lt/storage"
	"log/slog"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/neurosnap/sentences/english"
)

const (
	// batchTimeout is the maximum time allowed for embedding a single batch
	batchTimeout = 2 * time.Minute
)

var (
	// Status messages for TUI integration
	LongJobStatusCh     = make(chan string, 100) // Increased buffer size for parallel batch updates
	FinishedRAGStatus   = "finished loading RAG file; press Enter"
	LoadedFileRAGStatus = "loaded file"
	ErrRAGStatus        = "some error occurred; failed to transfer data to vector db"
)

type RAG struct {
	logger      *slog.Logger
	store       storage.FullRepo
	cfg         *config.Config
	embedder    Embedder
	storage     *VectorStorage
	mu          sync.RWMutex
	idleMu      sync.Mutex
	fallbackMsg string
	idleTimer   *time.Timer
	idleTimeout time.Duration
}

// batchTask represents a single batch to be embedded
type batchTask struct {
	batchIndex   int
	paragraphs   []string
	filename     string
	totalBatches int
}

// batchResult represents the result of embedding a batch
type batchResult struct {
	batchIndex int
	embeddings [][]float32
	paragraphs []string
	filename   string
}

// sendStatusNonBlocking sends a status message without blocking
func (r *RAG) sendStatusNonBlocking(status string) {
	select {
	case LongJobStatusCh <- status:
	default:
		r.logger.Warn("LongJobStatusCh channel is full or closed, dropping status message", "message", status)
	}
}

func New(l *slog.Logger, s storage.FullRepo, cfg *config.Config) (*RAG, error) {
	var embedder Embedder
	var fallbackMsg string
	if cfg.EmbedModelPath != "" && cfg.EmbedTokenizerPath != "" {
		emb, err := NewONNXEmbedder(cfg.EmbedModelPath, cfg.EmbedTokenizerPath, cfg.EmbedDims, l)
		if err != nil {
			l.Error("failed to create ONNX embedder, falling back to API", "error", err)
			fallbackMsg = err.Error()
			embedder = NewAPIEmbedder(l, cfg)
		} else {
			embedder = emb
			l.Info("using ONNX embedder", "model", cfg.EmbedModelPath, "dims", cfg.EmbedDims)
		}
	} else {
		embedder = NewAPIEmbedder(l, cfg)
		l.Info("using API embedder", "url", cfg.EmbedURL)
	}
	rag := &RAG{
		logger:      l,
		store:       s,
		cfg:         cfg,
		embedder:    embedder,
		storage:     NewVectorStorage(l, s),
		fallbackMsg: fallbackMsg,
		idleTimeout: 30 * time.Second,
	}

	// Note: Vector tables are created via database migrations, not at runtime

	return rag, nil
}

func wordCounter(sentence string) int {
	return len(strings.Split(strings.TrimSpace(sentence), " "))
}

func createChunks(sentences []string, wordLimit, overlapWords uint32) []string {
	if len(sentences) == 0 {
		return nil
	}
	if overlapWords >= wordLimit {
		overlapWords = wordLimit / 2
	}
	var chunks []string
	i := 0
	for i < len(sentences) {
		var chunkWords []string
		wordCount := 0
		j := i
		for j < len(sentences) && wordCount <= int(wordLimit) {
			sentence := sentences[j]
			words := strings.Fields(sentence)
			chunkWords = append(chunkWords, sentence)
			wordCount += len(words)
			j++
			// If this sentence alone exceeds limit, still include it and stop
			if wordCount > int(wordLimit) {
				break
			}
		}
		if len(chunkWords) == 0 {
			break
		}
		chunk := strings.Join(chunkWords, " ")
		chunks = append(chunks, chunk)
		if j >= len(sentences) {
			break
		}
		// Move i forward by skipping overlap
		if overlapWords == 0 {
			i = j
			continue
		}
		// Calculate how many sentences to skip to achieve overlapWords
		overlapRemaining := int(overlapWords)
		newI := i
		for newI < j && overlapRemaining > 0 {
			words := len(strings.Fields(sentences[newI]))
			overlapRemaining -= words
			if overlapRemaining >= 0 {
				newI++
			}
		}
		if newI == i {
			newI = j
		}
		i = newI
	}
	return chunks
}

func sanitizeFTSQuery(query string) string {
	// Remove double quotes and other problematic characters for FTS5
	query = strings.ReplaceAll(query, "\"", " ")
	query = strings.ReplaceAll(query, "'", " ")
	query = strings.ReplaceAll(query, ";", " ")
	query = strings.ReplaceAll(query, "\\", " ")
	query = strings.TrimSpace(query)
	if query == "" {
		return "*" // match all
	}
	return query
}

func (r *RAG) LoadRAG(fpath string) error {
	return r.LoadRAGWithContext(context.Background(), fpath)
}

func (r *RAG) LoadRAGWithContext(ctx context.Context, fpath string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	fileText, err := ExtractText(fpath)
	if err != nil {
		return err
	}
	r.logger.Debug("rag: loaded file", "fp", fpath)

	// Send initial status (non-blocking with retry)
	r.sendStatusNonBlocking(LoadedFileRAGStatus)

	tokenizer, err := english.NewSentenceTokenizer(nil)
	if err != nil {
		return err
	}
	sentences := tokenizer.Tokenize(fileText)
	sents := make([]string, len(sentences))
	for i, s := range sentences {
		sents[i] = s.Text
	}

	// Create chunks with overlap
	paragraphs := createChunks(sents, r.cfg.RAGWordLimit, r.cfg.RAGOverlapWords)
	// Adjust batch size if needed
	if len(paragraphs) < r.cfg.RAGBatchSize && len(paragraphs) > 0 {
		r.cfg.RAGBatchSize = len(paragraphs)
	}
	if len(paragraphs) == 0 {
		return errors.New("no valid paragraphs found in file")
	}

	totalBatches := (len(paragraphs) + r.cfg.RAGBatchSize - 1) / r.cfg.RAGBatchSize
	r.logger.Debug("starting parallel embedding", "total_batches", totalBatches, "batch_size", r.cfg.RAGBatchSize)

	// Determine concurrency level
	concurrency := runtime.NumCPU()
	if concurrency > totalBatches {
		concurrency = totalBatches
	}
	if concurrency < 1 {
		concurrency = 1
	}
	// If using ONNX embedder, limit concurrency to 1 due to mutex serialization
	isONNX := false
	if _, isONNX = r.embedder.(*ONNXEmbedder); isONNX {
		concurrency = 1
	}
	embedderType := "API"
	if isONNX {
		embedderType = "ONNX"
	}
	r.logger.Debug("parallel embedding setup",
		"total_batches", totalBatches,
		"concurrency", concurrency,
		"embedder", embedderType,
		"batch_size", r.cfg.RAGBatchSize)

	// Create context with timeout (30 minutes) and cancellation for error handling
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	// Channels for task distribution and results
	taskCh := make(chan batchTask, totalBatches)
	resultCh := make(chan batchResult, totalBatches)
	errorCh := make(chan error, totalBatches)

	// Start worker goroutines
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go r.embeddingWorker(ctx, w, taskCh, resultCh, errorCh, &wg)
	}

	// Close task channel after all tasks are sent (by separate goroutine)
	go func() {
		// Ensure task channel is closed when this goroutine exits
		defer close(taskCh)
		r.logger.Debug("task distributor started", "total_batches", totalBatches)

		for i := 0; i < totalBatches; i++ {
			start := i * r.cfg.RAGBatchSize
			end := start + r.cfg.RAGBatchSize
			if end > len(paragraphs) {
				end = len(paragraphs)
			}
			batch := paragraphs[start:end]

			// Filter empty paragraphs
			nonEmptyBatch := make([]string, 0, len(batch))
			for _, p := range batch {
				if strings.TrimSpace(p) != "" {
					nonEmptyBatch = append(nonEmptyBatch, strings.TrimSpace(p))
				}
			}

			task := batchTask{
				batchIndex:   i,
				paragraphs:   nonEmptyBatch,
				filename:     path.Base(fpath),
				totalBatches: totalBatches,
			}

			select {
			case taskCh <- task:
				r.logger.Debug("task distributor sent batch", "batch", i, "paragraphs", len(nonEmptyBatch))
			case <-ctx.Done():
				r.logger.Debug("task distributor cancelled", "batches_sent", i+1, "total_batches", totalBatches)
				return
			}
		}
		r.logger.Debug("task distributor finished", "batches_sent", totalBatches)
	}()

	// Wait for workers to finish and close result channel
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Process results in order and write to database
	nextExpectedBatch := 0
	resultsBuffer := make(map[int]batchResult)
	filename := path.Base(fpath)
	batchesProcessed := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-errorCh:
			// First error from any worker, cancel everything
			cancel()
			r.logger.Error("embedding worker failed", "error", err)
			r.sendStatusNonBlocking(ErrRAGStatus)
			return fmt.Errorf("embedding failed: %w", err)

		case result, ok := <-resultCh:
			if !ok {
				// All results processed
				resultCh = nil
				r.logger.Debug("result channel closed", "batches_processed", batchesProcessed, "total_batches", totalBatches)
				continue
			}

			// Store result in buffer
			resultsBuffer[result.batchIndex] = result

			// Process buffered results in order
			for {
				if res, exists := resultsBuffer[nextExpectedBatch]; exists {
					// Write this batch to database
					if err := r.writeBatchToStorage(ctx, res, filename); err != nil {
						cancel()
						return err
					}

					batchesProcessed++
					// Send progress update
					statusMsg := fmt.Sprintf("processed batch %d/%d", batchesProcessed, totalBatches)
					r.sendStatusNonBlocking(statusMsg)

					delete(resultsBuffer, nextExpectedBatch)
					nextExpectedBatch++
				} else {
					break
				}
			}

		default:
			// No channels ready, check for deadlock conditions
			if resultCh == nil && nextExpectedBatch < totalBatches {
				// Missing batch results after result channel closed
				r.logger.Error("missing batch results",
					"expected", totalBatches,
					"received", nextExpectedBatch,
					"missing", totalBatches-nextExpectedBatch)

				// Wait a short time for any delayed errors, then cancel
				select {
				case <-time.After(5 * time.Second):
					cancel()
					return fmt.Errorf("missing batch results: expected %d, got %d", totalBatches, nextExpectedBatch)
				case <-ctx.Done():
					return ctx.Err()
				case err := <-errorCh:
					cancel()
					r.logger.Error("embedding worker failed after result channel closed", "error", err)
					r.sendStatusNonBlocking(ErrRAGStatus)
					return fmt.Errorf("embedding failed: %w", err)
				}
			}
			// If we reach here, no deadlock yet, just busy loop prevention
			time.Sleep(100 * time.Millisecond)
		}

		// Check if we're done
		if resultCh == nil && nextExpectedBatch >= totalBatches {
			r.logger.Debug("all batches processed successfully", "total", totalBatches)
			break
		}
	}

	r.logger.Debug("finished writing vectors", "batches", batchesProcessed)
	r.resetIdleTimer()
	r.sendStatusNonBlocking(FinishedRAGStatus)
	return nil
}

// embeddingWorker processes batch embedding tasks
func (r *RAG) embeddingWorker(ctx context.Context, workerID int, taskCh <-chan batchTask, resultCh chan<- batchResult, errorCh chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()
	r.logger.Debug("embedding worker started", "worker", workerID)

	// Panic recovery to ensure worker doesn't crash silently
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("embedding worker panicked", "worker", workerID, "panic", rec)
			// Try to send error, but don't block if channel is full
			select {
			case errorCh <- fmt.Errorf("worker %d panicked: %v", workerID, rec):
			default:
				r.logger.Warn("error channel full, dropping panic error", "worker", workerID)
			}
		}
	}()

	for task := range taskCh {
		select {
		case <-ctx.Done():
			r.logger.Debug("embedding worker cancelled", "worker", workerID)
			return
		default:
		}
		r.logger.Debug("worker processing batch", "worker", workerID, "batch", task.batchIndex, "paragraphs", len(task.paragraphs), "total_batches", task.totalBatches)

		// Skip empty batches
		if len(task.paragraphs) == 0 {
			select {
			case resultCh <- batchResult{
				batchIndex: task.batchIndex,
				embeddings: nil,
				paragraphs: nil,
				filename:   task.filename,
			}:
			case <-ctx.Done():
				r.logger.Debug("embedding worker cancelled while sending empty batch", "worker", workerID)
				return
			}
			r.logger.Debug("worker sent empty batch", "worker", workerID, "batch", task.batchIndex)
			continue
		}

		// Embed with retry for API embedder
		embeddings, err := r.embedWithRetry(ctx, task.paragraphs, 3)
		if err != nil {
			// Try to send error, but don't block indefinitely
			select {
			case errorCh <- fmt.Errorf("worker %d batch %d: %w", workerID, task.batchIndex, err):
			case <-ctx.Done():
				r.logger.Debug("embedding worker cancelled while sending error", "worker", workerID)
			}
			return
		}

		// Send result with context awareness
		select {
		case resultCh <- batchResult{
			batchIndex: task.batchIndex,
			embeddings: embeddings,
			paragraphs: task.paragraphs,
			filename:   task.filename,
		}:
		case <-ctx.Done():
			r.logger.Debug("embedding worker cancelled while sending result", "worker", workerID)
			return
		}
		r.logger.Debug("worker completed batch", "worker", workerID, "batch", task.batchIndex, "embeddings", len(embeddings))
	}
	r.logger.Debug("embedding worker finished", "worker", workerID)
}

// embedWithRetry attempts embedding with exponential backoff for API embedder
func (r *RAG) embedWithRetry(ctx context.Context, paragraphs []string, maxRetries int) ([][]float32, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt*attempt) * time.Second
			if backoff > 10*time.Second {
				backoff = 10 * time.Second
			}

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			r.logger.Debug("retrying embedding", "attempt", attempt, "max_retries", maxRetries)
		}

		embeddings, err := r.embedder.EmbedSlice(paragraphs)
		if err == nil {
			// Validate embedding count
			if len(embeddings) != len(paragraphs) {
				return nil, fmt.Errorf("embedding count mismatch: expected %d, got %d", len(paragraphs), len(embeddings))
			}
			return embeddings, nil
		}

		lastErr = err
		// Only retry for API embedder errors (network/timeout)
		// For ONNX embedder, fail fast
		if _, isAPI := r.embedder.(*APIEmbedder); !isAPI {
			break
		}
	}

	return nil, fmt.Errorf("embedding failed after %d attempts: %w", maxRetries, lastErr)
}

// writeBatchToStorage writes a single batch of vectors to the database
func (r *RAG) writeBatchToStorage(ctx context.Context, result batchResult, filename string) error {
	if len(result.embeddings) == 0 {
		// Empty batch, skip
		return nil
	}

	// Check context before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Build all vectors for batch write
	vectors := make([]*models.VectorRow, 0, len(result.paragraphs))
	for j, text := range result.paragraphs {
		vectors = append(vectors, &models.VectorRow{
			Embeddings: result.embeddings[j],
			RawText:    text,
			Slug:       fmt.Sprintf("%s_%d_%d", filename, result.batchIndex+1, j),
			FileName:   filename,
		})
	}

	// Write all vectors in a single transaction
	if err := r.storage.WriteVectors(vectors); err != nil {
		r.logger.Error("failed to write vectors batch to DB", "error", err, "batch", result.batchIndex+1, "size", len(vectors))
		r.sendStatusNonBlocking(ErrRAGStatus)
		return fmt.Errorf("failed to write vectors batch: %w", err)
	}

	r.logger.Debug("wrote batch to db", "batch", result.batchIndex+1, "size", len(result.paragraphs))
	return nil
}

func (r *RAG) LineToVector(line string) ([]float32, error) {
	r.resetIdleTimer()
	return r.embedder.Embed(line)
}

func (r *RAG) searchEmb(emb *models.EmbeddingResp, limit int) ([]models.VectorRow, error) {
	r.resetIdleTimer()
	return r.storage.SearchClosest(emb.Embedding, limit)
}

func (r *RAG) searchKeyword(query string, limit int) ([]models.VectorRow, error) {
	r.resetIdleTimer()
	sanitized := sanitizeFTSQuery(query)
	return r.storage.SearchKeyword(sanitized, limit)
}

func (r *RAG) ListLoaded() ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.storage.ListFiles()
}

func (r *RAG) RemoveFile(filename string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resetIdleTimer()
	return r.storage.RemoveEmbByFileName(filename)
}

var (
	queryRefinementPattern = regexp.MustCompile(`(?i)(based on my (vector db|vector db|vector database|rags?|past (conversations?|chat|messages?))|from my (files?|documents?|data|information|memory)|search (in|my) (vector db|database|rags?)|rag search for)`)
	importantKeywords      = []string{"project", "architecture", "code", "file", "chat", "conversation", "topic", "summary", "details", "history", "previous", "my", "user", "me"}
	stopWords              = []string{"the", "a", "an", "and", "or", "but", "in", "on", "at", "to", "for", "of", "with", "by", "from", "up", "down", "left", "right"}
)

func (r *RAG) RefineQuery(query string) string {
	original := query
	query = strings.TrimSpace(query)
	if len(query) == 0 {
		return original
	}
	if len(query) <= 3 {
		return original
	}
	query = strings.ToLower(query)
	words := strings.Fields(query)
	if len(words) >= 3 {
		for _, stopWord := range stopWords {
			wordPattern := `\b` + stopWord + `\b`
			re := regexp.MustCompile(wordPattern)
			query = re.ReplaceAllString(query, "")
		}
	}
	query = strings.TrimSpace(query)
	if len(query) < 5 {
		return original
	}
	if queryRefinementPattern.MatchString(original) {
		cleaned := queryRefinementPattern.ReplaceAllString(original, "")
		cleaned = strings.TrimSpace(cleaned)
		if len(cleaned) >= 5 {
			return cleaned
		}
	}
	query = r.extractImportantPhrases(query)
	if len(query) < 5 {
		return original
	}
	return query
}

func (r *RAG) extractImportantPhrases(query string) string {
	words := strings.Fields(query)
	var important []string
	for _, word := range words {
		word = strings.Trim(word, ".,!?;:'\"()[]{}")
		isImportant := false
		for _, kw := range importantKeywords {
			if strings.Contains(strings.ToLower(word), kw) {
				isImportant = true
				break
			}
		}
		if isImportant || len(word) >= 3 {
			important = append(important, word)
		}
	}
	if len(important) == 0 {
		return query
	}
	return strings.Join(important, " ")
}

func (r *RAG) GenerateQueryVariations(query string) []string {
	variations := []string{query}
	if len(query) < 5 {
		return variations
	}
	parts := strings.Fields(query)
	if len(parts) == 0 {
		return variations
	}
	// Get loaded filenames to filter out filename terms
	filenames, err := r.storage.ListFiles()
	if err == nil && len(filenames) > 0 {
		// Convert to lowercase for case-insensitive matching
		lowerFilenames := make([]string, len(filenames))
		for i, f := range filenames {
			lowerFilenames[i] = strings.ToLower(f)
		}
		filteredParts := make([]string, 0, len(parts))
		for _, part := range parts {
			partLower := strings.ToLower(part)
			skip := false
			for _, fn := range lowerFilenames {
				if strings.Contains(fn, partLower) || strings.Contains(partLower, fn) {
					skip = true
					break
				}
			}
			if !skip {
				filteredParts = append(filteredParts, part)
			}
		}
		// If filteredParts not empty and different from original, add filtered query
		if len(filteredParts) > 0 && len(filteredParts) != len(parts) {
			filteredQuery := strings.Join(filteredParts, " ")
			if len(filteredQuery) >= 5 {
				variations = append(variations, filteredQuery)
			}
		}
	}
	if len(parts) >= 2 {
		trimmed := strings.Join(parts[:len(parts)-1], " ")
		if len(trimmed) >= 5 {
			variations = append(variations, trimmed)
		}
	}
	if len(parts) >= 2 {
		trimmed := strings.Join(parts[1:], " ")
		if len(trimmed) >= 5 {
			variations = append(variations, trimmed)
		}
	}
	if !strings.HasSuffix(query, " explanation") {
		variations = append(variations, query+" explanation")
	}
	if !strings.HasPrefix(query, "what is ") {
		variations = append(variations, "what is "+query)
	}
	if !strings.HasSuffix(query, " details") {
		variations = append(variations, query+" details")
	}
	if !strings.HasSuffix(query, " summary") {
		variations = append(variations, query+" summary")
	}
	return variations
}

func (r *RAG) RerankResults(results []models.VectorRow, query string) []models.VectorRow {
	type scoredResult struct {
		row      models.VectorRow
		distance float32
	}
	scored := make([]scoredResult, 0, len(results))
	for i := range results {
		row := results[i]

		score := float32(0)
		rawTextLower := strings.ToLower(row.RawText)
		queryLower := strings.ToLower(query)
		if strings.Contains(rawTextLower, queryLower) {
			score += 10
		}
		queryWords := strings.Fields(queryLower)
		matchCount := 0
		for _, word := range queryWords {
			if len(word) > 2 && strings.Contains(rawTextLower, word) {
				matchCount++
			}
		}
		if len(queryWords) > 0 {
			score += float32(matchCount) / float32(len(queryWords)) * 5
		}
		if row.FileName == "chat" || strings.Contains(strings.ToLower(row.FileName), "conversation") {
			score += 3
		}
		distance := row.Distance - score/100
		scored = append(scored, scoredResult{row: row, distance: distance})
	}
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].distance < scored[j].distance
	})
	unique := make([]models.VectorRow, 0)
	seen := make(map[string]bool)
	fileCounts := make(map[string]int)
	for i := range scored {
		if !seen[scored[i].row.Slug] {
			if fileCounts[scored[i].row.FileName] >= 2 {
				continue
			}
			seen[scored[i].row.Slug] = true
			fileCounts[scored[i].row.FileName]++
			unique = append(unique, scored[i].row)
		}
	}
	if len(unique) > 10 {
		unique = unique[:10]
	}
	return unique
}

func (r *RAG) SynthesizeAnswer(results []models.VectorRow, query string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.resetIdleTimer()
	if len(results) == 0 {
		return "No relevant information found in the vector database.", nil
	}
	var contextBuilder strings.Builder
	contextBuilder.WriteString("User Query: ")
	contextBuilder.WriteString(query)
	contextBuilder.WriteString("\n\nRetrieved Context:\n")
	for i, row := range results {
		fmt.Fprintf(&contextBuilder, "[Source %d: %s]\n", i+1, row.FileName)
		contextBuilder.WriteString(row.RawText)
		contextBuilder.WriteString("\n\n")
	}
	contextBuilder.WriteString("Instructions: ")
	contextBuilder.WriteString("Based on the retrieved context above, provide a concise, coherent answer to the user's query. ")
	contextBuilder.WriteString("Extract only the most relevant information. ")
	contextBuilder.WriteString("If no relevant information is found, state that clearly. ")
	contextBuilder.WriteString("Cite sources by filename when relevant. ")
	contextBuilder.WriteString("Do not include unnecessary preamble or explanations.")
	synthesisPrompt := contextBuilder.String()
	emb, err := r.LineToVector(synthesisPrompt)
	if err != nil {
		r.logger.Error("failed to embed synthesis prompt", "error", err)
		return "", err
	}
	embResp := &models.EmbeddingResp{
		Embedding: emb,
		Index:     0,
	}
	topResults, err := r.searchEmb(embResp, 1)
	if err != nil {
		r.logger.Error("failed to search for synthesis context", "error", err)
		return "", err
	}
	if len(topResults) > 0 && topResults[0].RawText != synthesisPrompt {
		return topResults[0].RawText, nil
	}
	var finalAnswer strings.Builder
	finalAnswer.WriteString("Based on the retrieved context:\n\n")
	for i, row := range results {
		if i >= 5 {
			break
		}
		fmt.Fprintf(&finalAnswer, "- From %s: %s\n", row.FileName, truncateString(row.RawText, 200))
	}
	return finalAnswer.String(), nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (r *RAG) Search(query string, limit int) ([]models.VectorRow, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.resetIdleTimer()
	refined := r.RefineQuery(query)
	variations := r.GenerateQueryVariations(refined)

	// Collect embedding search results from all variations
	var embResults []models.VectorRow
	seen := make(map[string]bool)
	for _, q := range variations {
		emb, err := r.LineToVector(q)
		if err != nil {
			r.logger.Error("failed to embed query variation", "error", err, "query", q)
			continue
		}
		embResp := &models.EmbeddingResp{
			Embedding: emb,
			Index:     0,
		}
		results, err := r.searchEmb(embResp, limit*2) // Get more candidates
		if err != nil {
			r.logger.Error("failed to search embeddings", "error", err, "query", q)
			continue
		}
		for _, row := range results {
			if !seen[row.Slug] {
				seen[row.Slug] = true
				embResults = append(embResults, row)
			}
		}
	}
	// Sort embedding results by distance (lower is better)
	sort.Slice(embResults, func(i, j int) bool {
		return embResults[i].Distance < embResults[j].Distance
	})

	// Perform keyword search
	kwResults, err := r.searchKeyword(refined, limit*2)
	if err != nil {
		r.logger.Warn("keyword search failed, using only embeddings", "error", err)
		kwResults = nil
	}
	// Sort keyword results by distance (already sorted by BM25 score)
	// kwResults already sorted by distance (lower is better)

	// Combine using Reciprocal Rank Fusion (RRF)
	const rrfK = 60
	type scoredRow struct {
		row   models.VectorRow
		score float64
	}
	scoreMap := make(map[string]float64)
	// Add embedding results
	for rank, row := range embResults {
		score := 1.0 / (float64(rank) + rrfK)
		scoreMap[row.Slug] += score
	}
	// Add keyword results
	for rank, row := range kwResults {
		score := 1.0 / (float64(rank) + rrfK)
		scoreMap[row.Slug] += score
		// Ensure row exists in combined results
		if _, exists := seen[row.Slug]; !exists {
			embResults = append(embResults, row)
		}
	}
	// Create slice of scored rows
	scoredRows := make([]scoredRow, 0, len(embResults))
	for _, row := range embResults {
		score := scoreMap[row.Slug]
		scoredRows = append(scoredRows, scoredRow{row: row, score: score})
	}
	// Sort by descending RRF score
	sort.Slice(scoredRows, func(i, j int) bool {
		return scoredRows[i].score > scoredRows[j].score
	})
	// Take top limit
	if len(scoredRows) > limit {
		scoredRows = scoredRows[:limit]
	}
	// Convert back to VectorRow
	finalResults := make([]models.VectorRow, len(scoredRows))
	for i, sr := range scoredRows {
		finalResults[i] = sr.row
	}
	// Apply reranking heuristics
	reranked := r.RerankResults(finalResults, query)
	return reranked, nil
}

var (
	ragInstance *RAG
	ragOnce     sync.Once
)

func (r *RAG) FallbackMessage() string {
	return r.fallbackMsg
}

func Init(c *config.Config, l *slog.Logger, s storage.FullRepo) error {
	var err error
	ragOnce.Do(func() {
		if c == nil || l == nil || s == nil {
			return
		}
		ragInstance, err = New(l, s, c)
	})
	return err
}

func GetInstance() *RAG {
	return ragInstance
}

func (r *RAG) resetIdleTimer() {
	r.idleMu.Lock()
	defer r.idleMu.Unlock()
	if r.idleTimer != nil {
		r.idleTimer.Stop()
	}
	r.idleTimer = time.AfterFunc(r.idleTimeout, func() {
		r.freeONNXMemory()
	})
}

func (r *RAG) freeONNXMemory() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if onnx, ok := r.embedder.(*ONNXEmbedder); ok {
		if err := onnx.Destroy(); err != nil {
			r.logger.Error("failed to free ONNX memory", "error", err)
		} else {
			r.logger.Info("freed ONNX VRAM after idle timeout")
		}
	}
}

func (r *RAG) Destroy() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.idleTimer != nil {
		r.idleTimer.Stop()
		r.idleTimer = nil
	}
	if onnx, ok := r.embedder.(*ONNXEmbedder); ok {
		if err := onnx.Destroy(); err != nil {
			r.logger.Error("failed to destroy ONNX embedder", "error", err)
		}
	}
}
