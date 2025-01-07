package storage

import (
	"elefant/models"
	"errors"
	"fmt"
	"unsafe"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/ncruces"
)

type VectorRepo interface {
	WriteVector(*models.VectorRow) error
	SearchClosest(q []float32) ([]models.VectorRow, error)
}

var (
	vecTableName    = "embeddings"
	vecTableName384 = "embeddings_384"
)

func fetchTableName(emb []float32) (string, error) {
	switch len(emb) {
	case 5120:
		return vecTableName, nil
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
	stmt, _, err := p.s3Conn.Prepare(
		fmt.Sprintf("INSERT INTO %s(embedding, slug, raw_text) VALUES (?, ?, ?)", tableName))
	if err != nil {
		p.logger.Error("failed to prep a stmt", "error", err)
		return err
	}
	defer stmt.Close()
	v, err := sqlite_vec.SerializeFloat32(row.Embeddings)
	if err != nil {
		p.logger.Error("failed to serialize vector",
			"emb-len", len(row.Embeddings), "error", err)
		return err
	}
	if v == nil {
		err = errors.New("empty vector after serialization")
		p.logger.Error("empty vector after serialization",
			"emb-len", len(row.Embeddings), "text", row.RawText, "error", err)
		return err
	}
	if err := stmt.BindBlob(1, v); err != nil {
		p.logger.Error("failed to bind", "error", err)
		return err
	}
	if err := stmt.BindText(2, row.Slug); err != nil {
		p.logger.Error("failed to bind", "error", err)
		return err
	}
	if err := stmt.BindText(3, row.RawText); err != nil {
		p.logger.Error("failed to bind", "error", err)
		return err
	}
	err = stmt.Exec()
	if err != nil {
		return err
	}
	return nil
}

func decodeUnsafe(bs []byte) []float32 {
	return unsafe.Slice((*float32)(unsafe.Pointer(&bs[0])), len(bs)/4)
}

func (p ProviderSQL) SearchClosest(q []float32) ([]models.VectorRow, error) {
	tableName, err := fetchTableName(q)
	if err != nil {
		return nil, err
	}
	stmt, _, err := p.s3Conn.Prepare(
		fmt.Sprintf(`SELECT
			distance,
			embedding,
			slug,
			raw_text
		FROM %s
		WHERE embedding MATCH ?
		ORDER BY distance
		LIMIT 4
	`, tableName))
	if err != nil {
		return nil, err
	}
	query, err := sqlite_vec.SerializeFloat32(q[:])
	if err != nil {
		return nil, err
	}
	if err := stmt.BindBlob(1, query); err != nil {
		p.logger.Error("failed to bind", "error", err)
		return nil, err
	}
	resp := []models.VectorRow{}
	for stmt.Step() {
		res := models.VectorRow{}
		res.Distance = float32(stmt.ColumnFloat(0))
		emb := stmt.ColumnRawText(1)
		res.Embeddings = decodeUnsafe(emb)
		res.Slug = stmt.ColumnText(2)
		res.RawText = stmt.ColumnText(3)
		resp = append(resp, res)
	}
	if err := stmt.Err(); err != nil {
		return nil, err
	}
	err = stmt.Close()
	if err != nil {
		return nil, err
	}
	return resp, nil
}
