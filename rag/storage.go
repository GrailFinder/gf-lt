package rag

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"gf-lt/models"
	"gf-lt/storage"
	"log/slog"
	"sort"
	"strings"
	"unsafe"

	"github.com/jmoiron/sqlx"
)

// VectorStorage handles storing and retrieving vectors from SQLite
type VectorStorage struct {
	logger *slog.Logger
	sqlxDB *sqlx.DB
	store  storage.FullRepo
}

func NewVectorStorage(logger *slog.Logger, store storage.FullRepo) *VectorStorage {
	return &VectorStorage{
		logger: logger,
		sqlxDB: store.DB(), // Use the new DB() method
		store:  store,
	}
}

// SerializeVector converts []float32 to binary blob
func SerializeVector(vec []float32) []byte {
	buf := make([]byte, len(vec)*4) // 4 bytes per float32
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], mathFloat32bits(v))
	}
	return buf
}

// DeserializeVector converts binary blob back to []float32
func DeserializeVector(data []byte) []float32 {
	count := len(data) / 4
	vec := make([]float32, count)
	for i := 0; i < count; i++ {
		vec[i] = mathBitsToFloat32(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vec
}

// mathFloat32bits and mathBitsToFloat32 are helpers to convert between float32 and uint32
func mathFloat32bits(f float32) uint32 {
	return binary.LittleEndian.Uint32((*(*[4]byte)(unsafe.Pointer(&f)))[:4])
}

func mathBitsToFloat32(b uint32) float32 {
	return *(*float32)(unsafe.Pointer(&b))
}

// WriteVector stores an embedding vector in the database
func (vs *VectorStorage) WriteVector(row *models.VectorRow) error {
	tableName, err := vs.getTableName(row.Embeddings)
	if err != nil {
		return err
	}
	embeddingSize := len(row.Embeddings)
	// Start transaction
	tx, err := vs.sqlxDB.Beginx()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Serialize the embeddings to binary
	serializedEmbeddings := SerializeVector(row.Embeddings)
	query := fmt.Sprintf(
		"INSERT INTO %s (embeddings, slug, raw_text, filename) VALUES (?, ?, ?, ?)",
		tableName,
	)
	if _, err := tx.Exec(query, serializedEmbeddings, row.Slug, row.RawText, row.FileName); err != nil {
		vs.logger.Error("failed to write vector", "error", err, "slug", row.Slug)
		return err
	}
	// Insert into FTS table
	ftsQuery := `INSERT INTO fts_embeddings (slug, raw_text, filename, embedding_size) VALUES (?, ?, ?, ?)`
	if _, err := tx.Exec(ftsQuery, row.Slug, row.RawText, row.FileName, embeddingSize); err != nil {
		vs.logger.Error("failed to write to FTS table", "error", err, "slug", row.Slug)
		return err
	}
	err = tx.Commit()
	if err != nil {
		vs.logger.Error("failed to commit transaction", "error", err)
		return err
	}
	return nil
}

// WriteVectors stores multiple embedding vectors in a single transaction
func (vs *VectorStorage) WriteVectors(rows []*models.VectorRow) error {
	if len(rows) == 0 {
		return nil
	}
	// SQLite has limit of 999 parameters per statement, each row uses 4 parameters
	const maxBatchSize = 200 // 200 * 4 = 800 < 999
	if len(rows) > maxBatchSize {
		// Process in chunks
		for i := 0; i < len(rows); i += maxBatchSize {
			end := i + maxBatchSize
			if end > len(rows) {
				end = len(rows)
			}
			if err := vs.WriteVectors(rows[i:end]); err != nil {
				return err
			}
		}
		return nil
	}
	// All rows should have same embedding size (same model)
	firstSize := len(rows[0].Embeddings)
	for i, row := range rows {
		if len(row.Embeddings) != firstSize {
			return fmt.Errorf("embedding size mismatch: row %d has size %d, expected %d", i, len(row.Embeddings), firstSize)
		}
	}
	tableName, err := vs.getTableName(rows[0].Embeddings)
	if err != nil {
		return err
	}
	// Start transaction
	tx, err := vs.sqlxDB.Beginx()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Build batch insert for embeddings table
	embeddingPlaceholders := make([]string, 0, len(rows))
	embeddingArgs := make([]any, 0, len(rows)*4)
	for _, row := range rows {
		embeddingPlaceholders = append(embeddingPlaceholders, "(?, ?, ?, ?)")
		embeddingArgs = append(embeddingArgs, SerializeVector(row.Embeddings), row.Slug, row.RawText, row.FileName)
	}
	embeddingQuery := fmt.Sprintf(
		"INSERT INTO %s (embeddings, slug, raw_text, filename) VALUES %s",
		tableName,
		strings.Join(embeddingPlaceholders, ", "),
	)
	if _, err := tx.Exec(embeddingQuery, embeddingArgs...); err != nil {
		vs.logger.Error("failed to write vectors batch", "error", err, "batch_size", len(rows))
		return err
	}
	// Build batch insert for FTS table
	ftsPlaceholders := make([]string, 0, len(rows))
	ftsArgs := make([]any, 0, len(rows)*4)
	embeddingSize := len(rows[0].Embeddings)
	for _, row := range rows {
		ftsPlaceholders = append(ftsPlaceholders, "(?, ?, ?, ?)")
		ftsArgs = append(ftsArgs, row.Slug, row.RawText, row.FileName, embeddingSize)
	}
	ftsQuery := "INSERT INTO fts_embeddings (slug, raw_text, filename, embedding_size) VALUES " +
		strings.Join(ftsPlaceholders, ", ")
	if _, err := tx.Exec(ftsQuery, ftsArgs...); err != nil {
		vs.logger.Error("failed to write FTS batch", "error", err, "batch_size", len(rows))
		return err
	}
	err = tx.Commit()
	if err != nil {
		vs.logger.Error("failed to commit transaction", "error", err)
		return err
	}
	vs.logger.Debug("wrote vectors batch", "batch_size", len(rows))
	return nil
}

// getTableName determines which table to use based on embedding size
func (vs *VectorStorage) getTableName(emb []float32) (string, error) {
	size := len(emb)

	// Check if we support this embedding size
	supportedSizes := map[int]bool{
		384:  true,
		768:  true,
		1024: true,
		1536: true,
		2048: true,
		3072: true,
		4096: true,
		5120: true,
	}
	if supportedSizes[size] {
		return fmt.Sprintf("embeddings_%d", size), nil
	}
	return "", fmt.Errorf("no table for embedding size of %d", size)
}

// SearchClosest finds vectors closest to the query vector using efficient cosine similarity calculation
func (vs *VectorStorage) SearchClosest(query []float32, limit int) ([]models.VectorRow, error) {
	if limit <= 0 {
		limit = 10
	}
	tableName, err := vs.getTableName(query)
	if err != nil {
		return nil, err
	}
	querySQL := "SELECT embeddings, slug, raw_text, filename FROM " + tableName
	rows, err := vs.sqlxDB.Query(querySQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type SearchResult struct {
		vector   models.VectorRow
		distance float32
	}
	var topResults []SearchResult
	for rows.Next() {
		var (
			embeddingsBlob          []byte
			slug, rawText, fileName string
		)

		if err := rows.Scan(&embeddingsBlob, &slug, &rawText, &fileName); err != nil {
			vs.logger.Error("failed to scan row", "error", err)
			continue
		}
		storedEmbeddings := DeserializeVector(embeddingsBlob)
		similarity := cosineSimilarity(query, storedEmbeddings)
		distance := 1 - similarity

		result := SearchResult{
			vector: models.VectorRow{
				Embeddings: storedEmbeddings,
				Slug:       slug,
				RawText:    rawText,
				FileName:   fileName,
			},
			distance: distance,
		}

		topResults = append(topResults, result)
		sort.Slice(topResults, func(i, j int) bool {
			return topResults[i].distance < topResults[j].distance
		})
		if len(topResults) > limit {
			topResults = topResults[:limit]
		}
	}
	results := make([]models.VectorRow, 0, len(topResults))
	for _, result := range topResults {
		result.vector.Distance = result.distance
		results = append(results, result.vector)
	}
	return results, nil
}

// GetVectorBySlug retrieves a vector row by its slug
func (vs *VectorStorage) GetVectorBySlug(slug string) (*models.VectorRow, error) {
	embeddingSizes := []int{384, 768, 1024, 1536, 2048, 3072, 4096, 5120}
	for _, size := range embeddingSizes {
		table := fmt.Sprintf("embeddings_%d", size)
		query := fmt.Sprintf("SELECT embeddings, slug, raw_text, filename FROM %s WHERE slug = ?", table)
		row := vs.sqlxDB.QueryRow(query, slug)
		var (
			embeddingsBlob                   []byte
			retrievedSlug, rawText, fileName string
		)
		if err := row.Scan(&embeddingsBlob, &retrievedSlug, &rawText, &fileName); err != nil {
			// No row in this table, continue to next size
			continue
		}
		storedEmbeddings := DeserializeVector(embeddingsBlob)
		return &models.VectorRow{
			Embeddings: storedEmbeddings,
			Slug:       retrievedSlug,
			RawText:    rawText,
			FileName:   fileName,
		}, nil
	}
	return nil, fmt.Errorf("vector with slug %s not found", slug)
}

// SearchKeyword performs full-text search using FTS5
func (vs *VectorStorage) SearchKeyword(query string, limit int) ([]models.VectorRow, error) {
	// Use FTS5 bm25 ranking. bm25 returns negative values where more negative is better.
	// We'll order by bm25 (ascending) and limit.
	ftsQuery := `SELECT slug, raw_text, filename, bm25(fts_embeddings) as score 
				 FROM fts_embeddings 
				 WHERE fts_embeddings MATCH ? 
				 ORDER BY score 
				 LIMIT ?`

	// Try original query first
	rows, err := vs.sqlxDB.Query(ftsQuery, query, limit)
	if err != nil {
		return nil, fmt.Errorf("FTS search failed: %w", err)
	}
	results, err := vs.scanRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	// If no results and query contains multiple terms, try OR fallback
	if len(results) == 0 && strings.Contains(query, " ") && !strings.Contains(strings.ToUpper(query), " OR ") {
		// Build OR query: term1 OR term2 OR term3
		terms := strings.Fields(query)
		if len(terms) > 1 {
			orQuery := strings.Join(terms, " OR ")
			rows, err := vs.sqlxDB.Query(ftsQuery, orQuery, limit)
			if err != nil {
				// Return original empty results rather than error
				return results, nil
			}
			orResults, err := vs.scanRows(rows)
			rows.Close()
			if err == nil {
				results = orResults
			}
		}
	}
	return results, nil
}

// scanRows converts SQL rows to VectorRow slice
func (vs *VectorStorage) scanRows(rows *sql.Rows) ([]models.VectorRow, error) {
	var results []models.VectorRow
	for rows.Next() {
		var slug, rawText, fileName string
		var score float64
		if err := rows.Scan(&slug, &rawText, &fileName, &score); err != nil {
			vs.logger.Error("failed to scan FTS row", "error", err)
			continue
		}
		// Convert BM25 score to distance-like metric (lower is better)
		// BM25 is negative, more negative is better. We'll normalize to positive distance.
		distance := float32(-score) // Make positive (since score is negative)
		if distance < 0 {
			distance = 0
		}
		results = append(results, models.VectorRow{
			Slug:     slug,
			RawText:  rawText,
			FileName: fileName,
			Distance: distance,
		})
	}
	return results, nil
}

// ListFiles returns a list of all loaded files
func (vs *VectorStorage) ListFiles() ([]string, error) {
	fileLists := make([][]string, 0)
	// Query all supported tables and combine results
	embeddingSizes := []int{384, 768, 1024, 1536, 2048, 3072, 4096, 5120}
	for _, size := range embeddingSizes {
		table := fmt.Sprintf("embeddings_%d", size)
		query := "SELECT DISTINCT filename FROM " + table
		rows, err := vs.sqlxDB.Query(query)
		if err != nil {
			// Continue if one table doesn't exist
			continue
		}

		var files []string
		for rows.Next() {
			var filename string
			if err := rows.Scan(&filename); err != nil {
				continue
			}
			files = append(files, filename)
		}
		rows.Close()

		fileLists = append(fileLists, files)
	}

	// Combine and deduplicate
	fileSet := make(map[string]bool)
	var allFiles []string
	for _, files := range fileLists {
		for _, file := range files {
			if !fileSet[file] {
				fileSet[file] = true
				allFiles = append(allFiles, file)
			}
		}
	}
	return allFiles, nil
}

// RemoveEmbByFileName removes all embeddings associated with a specific filename
func (vs *VectorStorage) RemoveEmbByFileName(filename string) error {
	var errors []string
	// Delete from FTS table first
	if _, err := vs.sqlxDB.Exec("DELETE FROM fts_embeddings WHERE filename = ?", filename); err != nil {
		errors = append(errors, err.Error())
	}
	embeddingSizes := []int{384, 768, 1024, 1536, 2048, 3072, 4096, 5120}
	for _, size := range embeddingSizes {
		table := fmt.Sprintf("embeddings_%d", size)
		query := fmt.Sprintf("DELETE FROM %s WHERE filename = ?", table)
		if _, err := vs.sqlxDB.Exec(query, filename); err != nil {
			errors = append(errors, err.Error())
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("errors occurred: %s", strings.Join(errors, "; "))
	}
	return nil
}

// cosineSimilarity calculates the cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0.0
	}
	var dotProduct, normA, normB float32
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return dotProduct / (sqrt(normA) * sqrt(normB))
}

// sqrt returns the square root of a float32
func sqrt(f float32) float32 {
	// A simple implementation of square root using Newton's method
	if f == 0 {
		return 0
	}
	guess := f / 2
	for i := 0; i < 10; i++ { // 10 iterations should be enough for good precision
		guess = (guess + f/guess) / 2
	}
	return guess
}
