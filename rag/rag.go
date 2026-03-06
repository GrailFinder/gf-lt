package rag

import (
	"errors"
	"fmt"
	"gf-lt/config"
	"gf-lt/models"
	"gf-lt/storage"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

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
	logger      *slog.Logger
	store       storage.FullRepo
	cfg         *config.Config
	embedder    Embedder
	storage     *VectorStorage
	mu          sync.Mutex
	fallbackMsg string
	idleTimer   *time.Timer
	idleTimeout time.Duration
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
	r.mu.Lock()
	defer r.mu.Unlock()
	fileText, err := ExtractText(fpath)
	if err != nil {
		return err
	}
	r.logger.Debug("rag: loaded file", "fp", fpath)
	select {
	case LongJobStatusCh <- LoadedFileRAGStatus:
	default:
		r.logger.Warn("LongJobStatusCh channel is full or closed, dropping status message", "message", LoadedFileRAGStatus)
	}
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
	r.resetIdleTimer()
	select {
	case LongJobStatusCh <- FinishedRAGStatus:
	default:
		r.logger.Warn("LongJobStatusCh channel is full or closed, dropping status message", "message", FinishedRAGStatus)
	}
	return nil
}

func (r *RAG) LineToVector(line string) ([]float32, error) {
	r.resetIdleTimer()
	return r.embedder.Embed(line)
}

func (r *RAG) SearchEmb(emb *models.EmbeddingResp, limit int) ([]models.VectorRow, error) {
	r.resetIdleTimer()
	return r.storage.SearchClosest(emb.Embedding, limit)
}

func (r *RAG) SearchKeyword(query string, limit int) ([]models.VectorRow, error) {
	r.resetIdleTimer()
	sanitized := sanitizeFTSQuery(query)
	return r.storage.SearchKeyword(sanitized, limit)
}

func (r *RAG) ListLoaded() ([]string, error) {
	return r.storage.ListFiles()
}

func (r *RAG) RemoveFile(filename string) error {
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
	topResults, err := r.SearchEmb(embResp, 1)
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
		results, err := r.SearchEmb(embResp, limit*2) // Get more candidates
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
	kwResults, err := r.SearchKeyword(refined, limit*2)
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
