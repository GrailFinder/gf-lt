package storage

import (
	"elefant/models"
	"errors"
	"fmt"
	"log"
	"unsafe"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/ncruces"
)

type VectorRepo interface {
	WriteVector(*models.VectorRow) error
	SearchClosest(q []float32) (*models.VectorRow, error)
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
		p.logger.Error("failed exec a stmt", "error", err)
		return err
	}
	return nil
}

func decodeUnsafe(bs []byte) []float32 {
	return unsafe.Slice((*float32)(unsafe.Pointer(&bs[0])), len(bs)/4)
}

func (p ProviderSQL) SearchClosest(q []float32) (*models.VectorRow, error) {
	stmt, _, err := p.s3Conn.Prepare(
		fmt.Sprintf(`SELECT
			id,
			distance,
			embedding,
			slug,
			raw_text
		FROM %s
		WHERE embedding MATCH ?
		ORDER BY distance
		LIMIT 4
	`, vecTableName))
	if err != nil {
		log.Fatal(err)
	}
	query, err := sqlite_vec.SerializeFloat32(q[:])
	if err != nil {
		log.Fatal(err)
	}
	if err := stmt.BindBlob(1, query); err != nil {
		p.logger.Error("failed to bind", "error", err)
		return nil, err
	}
	resp := make([]models.VectorRow, 4)
	i := 0
	for stmt.Step() {
		resp[i].ID = uint32(stmt.ColumnInt64(0))
		resp[i].Distance = float32(stmt.ColumnFloat(1))
		emb := stmt.ColumnRawText(2)
		resp[i].Embeddings = decodeUnsafe(emb)
		resp[i].Slug = stmt.ColumnText(3)
		resp[i].RawText = stmt.ColumnText(4)
		i++
	}
	if err := stmt.Err(); err != nil {
		log.Fatal(err)
	}
	err = stmt.Close()
	if err != nil {
		log.Fatal(err)
	}
	return nil, nil
}
