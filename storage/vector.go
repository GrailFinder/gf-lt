package storage

import (
	"gf-lt/models"
	"encoding/binary"
	"fmt"
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
	
	query := fmt.Sprintf("INSERT INTO %s(embedding, slug, raw_text, filename) VALUES (?, ?, ?, ?)", tableName)
	_, err = p.db.Exec(query, serializedEmbeddings, row.Slug, row.RawText, row.FileName)
	
	return err
}



func (p ProviderSQL) SearchClosest(q []float32) ([]models.VectorRow, error) {
	// TODO: This function has been temporarily disabled to avoid deprecated library usage. 
	// In the new RAG implementation, this functionality is now in rag_new package. 
	// For compatibility, return empty result instead of using deprecated vector extension. 
	return []models.VectorRow{}, nil 
}

func (p ProviderSQL) ListFiles() ([]string, error) {
	q := fmt.Sprintf("SELECT filename FROM %s GROUP BY filename", vecTableName384)
	rows, err := p.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	resp := []string{}
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			return nil, err
		}
		resp = append(resp, filename)
	}
	
	if err := rows.Err(); err != nil {
		return nil, err
	}
	
	return resp, nil
}

func (p ProviderSQL) RemoveEmbByFileName(filename string) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE filename = ?", vecTableName384)
	_, err := p.db.Exec(q, filename)
	return err
}
