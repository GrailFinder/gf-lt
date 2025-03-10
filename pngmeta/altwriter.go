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

const (
	pngHeader     = "\x89PNG\r\n\x1a\n"
	textChunkType = "tEXt"
)

// WriteToPng embeds the metadata into the specified PNG file and writes the result to outfile.
func WriteToPng(metadata *models.CharCardSpec, sourcePath, outfile string) error {
	pngData, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	jsonData, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	base64Data := base64.StdEncoding.EncodeToString(jsonData)
	embedData := PngEmbed{
		Key:   "elefant", // Replace with appropriate key constant
		Value: base64Data,
	}
	var outputBuffer bytes.Buffer
	if _, err := outputBuffer.Write([]byte(pngHeader)); err != nil {
		return err
	}
	chunks, iend, err := processChunks(pngData[8:])
	if err != nil {
		return err
	}
	for _, chunk := range chunks {
		outputBuffer.Write(chunk)
	}
	newChunk, err := createTextChunk(embedData)
	if err != nil {
		return err
	}
	outputBuffer.Write(newChunk)
	outputBuffer.Write(iend)
	return os.WriteFile(outfile, outputBuffer.Bytes(), 0666)
}

// processChunks extracts non-tEXt chunks and locates the IEND chunk
func processChunks(data []byte) ([][]byte, []byte, error) {
	var (
		chunks    [][]byte
		iendChunk []byte
		reader    = bytes.NewReader(data)
	)
	for {
		var chunkLength uint32
		if err := binary.Read(reader, binary.BigEndian, &chunkLength); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("error reading chunk length: %w", err)
		}
		chunkType := make([]byte, 4)
		if _, err := reader.Read(chunkType); err != nil {
			return nil, nil, fmt.Errorf("error reading chunk type: %w", err)
		}
		chunkData := make([]byte, chunkLength)
		if _, err := reader.Read(chunkData); err != nil {
			return nil, nil, fmt.Errorf("error reading chunk data: %w", err)
		}
		crc := make([]byte, 4)
		if _, err := reader.Read(crc); err != nil {
			return nil, nil, fmt.Errorf("error reading CRC: %w", err)
		}
		fullChunk := bytes.NewBuffer(nil)
		if err := binary.Write(fullChunk, binary.BigEndian, chunkLength); err != nil {
			return nil, nil, fmt.Errorf("error writing chunk length: %w", err)
		}
		if _, err := fullChunk.Write(chunkType); err != nil {
			return nil, nil, fmt.Errorf("error writing chunk type: %w", err)
		}
		if _, err := fullChunk.Write(chunkData); err != nil {
			return nil, nil, fmt.Errorf("error writing chunk data: %w", err)
		}
		if _, err := fullChunk.Write(crc); err != nil {
			return nil, nil, fmt.Errorf("error writing CRC: %w", err)
		}
		switch string(chunkType) {
		case "IEND":
			iendChunk = fullChunk.Bytes()
			return chunks, iendChunk, nil
		case textChunkType:
			continue // Skip existing tEXt chunks
		default:
			chunks = append(chunks, fullChunk.Bytes())
		}
	}
	return nil, nil, errors.New("IEND chunk not found")
}

// createTextChunk generates a valid tEXt chunk with proper CRC
func createTextChunk(embed PngEmbed) ([]byte, error) {
	content := bytes.NewBuffer(nil)
	content.WriteString(embed.Key)
	content.WriteByte(0) // Null separator
	content.WriteString(embed.Value)
	data := content.Bytes()
	crc := crc32.NewIEEE()
	crc.Write([]byte(textChunkType))
	crc.Write(data)
	chunk := bytes.NewBuffer(nil)
	if err := binary.Write(chunk, binary.BigEndian, uint32(len(data))); err != nil {
		return nil, fmt.Errorf("error writing chunk length: %w", err)
	}
	if _, err := chunk.Write([]byte(textChunkType)); err != nil {
		return nil, fmt.Errorf("error writing chunk type: %w", err)
	}
	if _, err := chunk.Write(data); err != nil {
		return nil, fmt.Errorf("error writing chunk data: %w", err)
	}
	if err := binary.Write(chunk, binary.BigEndian, crc.Sum32()); err != nil {
		return nil, fmt.Errorf("error writing CRC: %w", err)
	}
	return chunk.Bytes(), nil
}
