package pngmeta

import (
	"encoding/binary"
	"errors"
	"hash"
	"hash/crc32"
	"io"
)

var (
	ErrCRC32Mismatch = errors.New("crc32 mismatch")
	ErrNotPNG        = errors.New("not png")
	ErrBadLength     = errors.New("bad length")
)

type PngChunk struct {
	typ         string
	length      int32
	r           io.Reader
	realR       io.Reader
	checksummer hash.Hash32
}

func (c *PngChunk) Read(p []byte) (int, error) {
	return io.TeeReader(c.r, c.checksummer).Read(p)
}

func (c *PngChunk) Close() error {
	var crc32 uint32
	if err := binary.Read(c.realR, binary.BigEndian, &crc32); err != nil {
		return err
	}
	if crc32 != c.checksummer.Sum32() {
		return ErrCRC32Mismatch
	}
	return nil
}

func (c *PngChunk) Type() string {
	return c.typ
}

type Reader struct {
	r io.Reader
}

func NewPNGStepReader(r io.Reader) (*Reader, error) {
	expectedHeader := make([]byte, len(header))
	if _, err := io.ReadFull(r, expectedHeader); err != nil {
		return nil, err
	}
	if string(expectedHeader) != header {
		return nil, ErrNotPNG
	}
	return &Reader{r}, nil
}

func (r *Reader) Next() (*PngChunk, error) {
	var length int32
	if err := binary.Read(r.r, binary.BigEndian, &length); err != nil {
		return nil, err
	}
	if length < 0 {
		return nil, ErrBadLength
	}
	var rawTyp [4]byte
	if _, err := io.ReadFull(r.r, rawTyp[:]); err != nil {
		return nil, err
	}
	typ := string(rawTyp[:])
	checksummer := crc32.NewIEEE()
	checksummer.Write([]byte(typ))
	return &PngChunk{typ, length, io.LimitReader(r.r, int64(length)), r.r, checksummer}, nil
}
