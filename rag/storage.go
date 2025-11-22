package rag

import (
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

// CreateTables creates the necessary tables for vector storage
func (vs *VectorStorage) CreateTables() error {
	// Create tables for common embedding dimensions
	embeddingSizes := []int{384, 768, 1024, 1536, 2048, 3072, 4096, 5120}
	// Pre-allocate queries slice: each embedding size needs 1 table + 3 indexes = 4 queries per size
	queries := make([]string, 0, len(embeddingSizes)*4)

	// Generate table creation queries for each embedding size
	for _, size := range embeddingSizes {
		tableName := fmt.Sprintf("embeddings_%d", size)
		queries = append(queries,
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				embeddings BLOB NOT NULL,
				slug TEXT NOT NULL,
				raw_text TEXT NOT NULL,
				filename TEXT NOT NULL,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			)`, tableName),
		)
	}

	// Add indexes for all supported sizes
	for _, size := range embeddingSizes {
		tableName := fmt.Sprintf("embeddings_%d", size)
		queries = append(queries,
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_filename ON %s(filename)`, tableName, tableName),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_slug ON %s(slug)`, tableName, tableName),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_created_at ON %s(created_at)`, tableName, tableName),
		)
	}

	for _, query := range queries {
		if _, err := vs.sqlxDB.Exec(query); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}
	return nil
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

	// Serialize the embeddings to binary
	serializedEmbeddings := SerializeVector(row.Embeddings)

	query := fmt.Sprintf(
		"INSERT INTO %s (embeddings, slug, raw_text, filename) VALUES (?, ?, ?, ?)",
		tableName,
	)

	if _, err := vs.sqlxDB.Exec(query, serializedEmbeddings, row.Slug, row.RawText, row.FileName); err != nil {
		vs.logger.Error("failed to write vector", "error", err, "slug", row.Slug)
		return err
	}

	return nil
}

// getTableName determines which table to use based on embedding size
func (vs *VectorStorage) getTableName(emb []float32) (string, error) {
	size := len(emb)

	// Check if we support this embedding size
	supportedSizes := map[int]bool{
		384:   true,
		768:   true,
		1024:  true,
		1536:  true,
		2048:  true,
		3072:  true,
		4096:  true,
		5120:  true,
	}

	if supportedSizes[size] {
		return fmt.Sprintf("embeddings_%d", size), nil
	}

	return "", fmt.Errorf("no table for embedding size of %d", size)
}

// SearchClosest finds vectors closest to the query vector using efficient cosine similarity calculation
func (vs *VectorStorage) SearchClosest(query []float32) ([]models.VectorRow, error) {
	tableName, err := vs.getTableName(query)
	if err != nil {
		return nil, err
	}

	// For better performance, instead of loading all vectors at once,
	// we'll implement batching and potentially add L2 distance-based pre-filtering
	// since cosine similarity is related to L2 distance for normalized vectors

	querySQL := "SELECT embeddings, slug, raw_text, filename FROM " + tableName
	rows, err := vs.sqlxDB.Query(querySQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Use a min-heap or simple slice to keep track of top 3 closest vectors
	type SearchResult struct {
		vector   models.VectorRow
		distance float32
	}

	var topResults []SearchResult

	// Process vectors one by one to avoid loading everything into memory
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

		// Calculate cosine similarity (returns value between -1 and 1, where 1 is most similar)
		similarity := cosineSimilarity(query, storedEmbeddings)
		distance := 1 - similarity // Convert to distance where 0 is most similar

		result := SearchResult{
			vector: models.VectorRow{
				Embeddings: storedEmbeddings,
				Slug:       slug,
				RawText:    rawText,
				FileName:   fileName,
			},
			distance: distance,
		}

		// Add to top results and maintain only top 3
		topResults = append(topResults, result)

		// Sort and keep only top 3
		sort.Slice(topResults, func(i, j int) bool {
			return topResults[i].distance < topResults[j].distance
		})

		if len(topResults) > 3 {
			topResults = topResults[:3] // Keep only closest 3
		}
	}

	// Convert back to VectorRow slice
	results := make([]models.VectorRow, 0, len(topResults))
	for _, result := range topResults {
		result.vector.Distance = result.distance
		results = append(results, result.vector)
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

