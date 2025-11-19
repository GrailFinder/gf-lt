package storage

import (
	"encoding/binary"
	"fmt"
	"gf-lt/models"
	"unsafe"

	"github.com/jmoiron/sqlx"
)

type VectorRepo interface {
	WriteVector(*models.VectorRow) error
	SearchClosest(q []float32) ([]models.VectorRow, error)
	ListFiles() ([]string, error)
	RemoveEmbByFileName(filename string) error
	DB() *sqlx.DB
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

var (
	vecTableName5120 = "embeddings_5120"
	vecTableName384  = "embeddings_384"
)

func fetchTableName(emb []float32) (string, error) {
	switch len(emb) {
	case 5120:
		return vecTableName5120, nil
	case 384:
		return vecTableName384, nil
	default:
		return "", fmt.Errorf("no table for the size of %d", len(emb))
	}
}

func (p ProviderSQL) WriteVector(row *models.VectorRow) error {
	tableName, err := fetchTableName(row.Embeddings)
	if err != nil {
		return err
	}

	serializedEmbeddings := SerializeVector(row.Embeddings)

	query := fmt.Sprintf("INSERT INTO %s(embeddings, slug, raw_text, filename) VALUES (?, ?, ?, ?)", tableName)
	_, err = p.db.Exec(query, serializedEmbeddings, row.Slug, row.RawText, row.FileName)

	return err
}

func (p ProviderSQL) SearchClosest(q []float32) ([]models.VectorRow, error) {
	tableName, err := fetchTableName(q)
	if err != nil {
		return nil, err
	}

	querySQL := "SELECT embeddings, slug, raw_text, filename FROM " + tableName
	rows, err := p.db.Query(querySQL)
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
			embeddingsBlob []byte
			slug, rawText, fileName string
		)

		if err := rows.Scan(&embeddingsBlob, &slug, &rawText, &fileName); err != nil {
			continue
		}

		storedEmbeddings := DeserializeVector(embeddingsBlob)

		// Calculate cosine similarity (returns value between -1 and 1, where 1 is most similar)
		similarity := cosineSimilarity(q, storedEmbeddings)
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

		// Add to top results and maintain only top results
		topResults = append(topResults, result)

		// Sort and keep only top results
		// We'll keep the top 3 closest vectors
		if len(topResults) > 3 {
			// Simple sort and truncate to maintain only 3 best matches
			for i := 0; i < len(topResults); i++ {
				for j := i + 1; j < len(topResults); j++ {
					if topResults[i].distance > topResults[j].distance {
						topResults[i], topResults[j] = topResults[j], topResults[i]
					}
				}
			}
			topResults = topResults[:3]
		}
	}

	// Convert back to VectorRow slice
	results := make([]models.VectorRow, len(topResults))
	for i, result := range topResults {
		result.vector.Distance = result.distance
		results[i] = result.vector
	}

	return results, nil
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

func (p ProviderSQL) ListFiles() ([]string, error) {
	fileLists := make([][]string, 0)

	// Query both tables and combine results
	for _, table := range []string{vecTableName384, vecTableName5120} {
		query := "SELECT DISTINCT filename FROM " + table
		rows, err := p.db.Query(query)
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

func (p ProviderSQL) RemoveEmbByFileName(filename string) error {
	var errors []string

	for _, table := range []string{vecTableName384, vecTableName5120} {
		query := fmt.Sprintf("DELETE FROM %s WHERE filename = ?", table)
		if _, err := p.db.Exec(query, filename); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors occurred: %v", errors)
	}

	return nil
}
