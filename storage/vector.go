package storage

import (
	"elefant/models"
	"fmt"
	"log"
	"unsafe"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/ncruces"
)

type VectorRepo interface {
	WriteVector(*models.VectorRow) error
	SearchClosest(q [5120]float32) (*models.VectorRow, error)
}

var vecTableName = "embeddings"

func (p ProviderSQL) WriteVector(row *models.VectorRow) error {
	stmt, _, err := p.s3Conn.Prepare(
		fmt.Sprintf("INSERT INTO %s(embedding, slug, raw_text) VALUES (?, ?, ?)", vecTableName))
	defer stmt.Close()
	if err != nil {
		p.logger.Error("failed to prep a stmt", "error", err)
		return err
	}
	v, err := sqlite_vec.SerializeFloat32(row.Embeddings)
	if err != nil {
		p.logger.Error("failed to serialize vector",
			"emb-len", len(row.Embeddings), "error", err)
		return err
	}
	stmt.BindInt(1, int(row.ID))
	stmt.BindBlob(2, v)
	stmt.BindText(3, row.Slug)
	stmt.BindText(4, row.RawText)
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

func (p ProviderSQL) SearchClosest(q [5120]float32) (*models.VectorRow, error) {
	stmt, _, err := p.s3Conn.Prepare(`
		SELECT
			id,
			distance,
			embedding,
			slug,
			raw_text
		FROM vec_items
		WHERE embedding MATCH ?
		ORDER BY distance
		LIMIT 4
	`)
	if err != nil {
		log.Fatal(err)
	}
	query, err := sqlite_vec.SerializeFloat32(q[:])
	if err != nil {
		log.Fatal(err)
	}
	stmt.BindBlob(1, query)
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
