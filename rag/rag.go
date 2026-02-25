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

	for _, stopWord := range stopWords {
		wordPattern := `\b` + stopWord + `\b`
		re := regexp.MustCompile(wordPattern)
		query = re.ReplaceAllString(query, "")
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

		if isImportant || len(word) > 3 {
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

	for i := range scored {
		if !seen[scored[i].row.Slug] {
			seen[scored[i].row.Slug] = true
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
		contextBuilder.WriteString(fmt.Sprintf("[Source %d: %s]\n", i+1, row.FileName))
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

	topResults, err := r.SearchEmb(embResp)
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
		finalAnswer.WriteString(fmt.Sprintf("- From %s: %s\n", row.FileName, truncateString(row.RawText, 200)))
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

	allResults := make([]models.VectorRow, 0)
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

		results, err := r.SearchEmb(embResp)
		if err != nil {
			r.logger.Error("failed to search embeddings", "error", err, "query", q)
			continue
		}

		for _, row := range results {
			if !seen[row.Slug] {
				seen[row.Slug] = true
				allResults = append(allResults, row)
			}
		}
	}

	reranked := r.RerankResults(allResults, query)

	if len(reranked) > limit {
		reranked = reranked[:limit]
	}

	return reranked, nil
}

var (
	ragInstance *RAG
	ragOnce     sync.Once
)

func Init(c *config.Config, l *slog.Logger, s storage.FullRepo) error {
	ragOnce.Do(func() {
		if c == nil || l == nil || s == nil {
			return
		}
		ragInstance = New(l, s, c)
	})
	return nil
}

func GetInstance() *RAG {
	return ragInstance
}
