package pngmeta

import (
	"bytes"
	"elefant/models"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

type Writer struct {
	w io.Writer
}

func NewPNGWriter(w io.Writer) (*Writer, error) {
	if _, err := io.WriteString(w, writeHeader); err != nil {
		return nil, err
	}
	return &Writer{w}, nil
}

func (w *Writer) WriteChunk(length int32, typ string, r io.Reader) error {
	if err := binary.Write(w.w, binary.BigEndian, length); err != nil {
		return err
	}
	if _, err := w.w.Write([]byte(typ)); err != nil {
		return err
	}
	checksummer := crc32.NewIEEE()
	checksummer.Write([]byte(typ))
	if _, err := io.CopyN(io.MultiWriter(w.w, checksummer), r, int64(length)); err != nil {
		return err
	}
	if err := binary.Write(w.w, binary.BigEndian, checksummer.Sum32()); err != nil {
		return err
	}
	return nil
}

func WriteToPng(c *models.CharCardSpec, fpath, outfile string) error {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}
	jsonData, err := json.Marshal(c)
	if err != nil {
		return err
	}
	// Base64 encode the JSON data
	base64Data := base64.StdEncoding.EncodeToString(jsonData)
	pe := PngEmbed{
		Key:   cKey,
		Value: base64Data,
	}
	w, err := WritetEXtToPngBytes(data, pe)
	if err != nil {
		return err
	}
	return os.WriteFile(outfile, w.Bytes(), 0666)
}

func WritetEXtToPngBytes(inputBytes []byte, pe PngEmbed) (outputBytes bytes.Buffer, err error) {
	if !(string(inputBytes[:8]) == header) {
		return outputBytes, errors.New("wrong file format")
	}
	reader := bytes.NewReader(inputBytes)
	pngr, err := NewPNGStepReader(reader)
	if err != nil {
		return outputBytes, fmt.Errorf("NewReader(): %s", err)
	}
	pngw, err := NewPNGWriter(&outputBytes)
	if err != nil {
		return outputBytes, fmt.Errorf("NewWriter(): %s", err)
	}
	for {
		chunk, err := pngr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return outputBytes, fmt.Errorf("NextChunk(): %s", err)
		}
		if chunk.Type() != embType {
			// IENDChunkType will only appear on the final iteration of a valid PNG
			if chunk.Type() == IEND {
				// This is where we inject tEXtChunkType as the penultimate chunk with the new value
				newtEXtChunk := []byte(fmt.Sprintf(tEXtChunkDataSpecification, pe.Key, pe.Value))
				if err := pngw.WriteChunk(int32(len(newtEXtChunk)), embType, bytes.NewBuffer(newtEXtChunk)); err != nil {
					return outputBytes, fmt.Errorf("WriteChunk(): %s", err)
				}
				// Now we end the buffer with IENDChunkType chunk
				if err := pngw.WriteChunk(chunk.length, chunk.Type(), chunk); err != nil {
					return outputBytes, fmt.Errorf("WriteChunk(): %s", err)
				}
			} else {
				// writes back original chunk to buffer
				if err := pngw.WriteChunk(chunk.length, chunk.Type(), chunk); err != nil {
					return outputBytes, fmt.Errorf("WriteChunk(): %s", err)
				}
			}
		} else {
			if _, err := io.Copy(io.Discard, chunk); err != nil {
				return outputBytes, fmt.Errorf("io.Copy(io.Discard, chunk): %s", err)
			}
		}
		if err := chunk.Close(); err != nil {
			return outputBytes, fmt.Errorf("chunk.Close(): %s", err)
		}
	}
	return outputBytes, nil
}
