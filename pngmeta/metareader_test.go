package pngmeta

import (
	"bytes"
	"elefant/models"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestReadMeta(t *testing.T) {
	cases := []struct {
		Filename string
	}{
		{
			Filename: "../sysprompts/llama.png",
		},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("test_%d", i), func(t *testing.T) {
			// Call the readMeta function
			pembed, err := extractChar(tc.Filename)
			if err != nil {
				t.Errorf("Expected no error, but got %v", err)
			}
			v, err := pembed.GetDecodedValue()
			if err != nil {
				t.Errorf("Expected no error, but got %v\n", err)
			}
			fmt.Printf("%+v\n", v.Simplify("Adam", tc.Filename))
		})
	}
}

// Test helper: Create a simple PNG image with test shapes
func createTestImage(t *testing.T) string {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	// Fill background with white
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.White)
		}
	}
	// Draw a red square
	for y := 50; y < 150; y++ {
		for x := 50; x < 150; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	// Draw a blue circle
	center := image.Point{100, 100}
	radius := 40
	for y := center.Y - radius; y <= center.Y+radius; y++ {
		for x := center.X - radius; x <= center.X+radius; x++ {
			dx := x - center.X
			dy := y - center.Y
			if dx*dx+dy*dy <= radius*radius {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	// Create temp file
	tmpDir := t.TempDir()
	fpath := filepath.Join(tmpDir, "test-image.png")
	f, err := os.Create(fpath)
	if err != nil {
		t.Fatalf("Error creating temp file: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("Error encoding PNG: %v", err)
	}
	return fpath
}

func TestWriteToPng(t *testing.T) {
	// Create test image
	srcPath := createTestImage(t)
	dstPath := filepath.Join(filepath.Dir(srcPath), "output.png")
	// dstPath := "test.png"
	// Create test metadata
	metadata := &models.CharCardSpec{
		Description: "Test image containing a red square and blue circle on white background",
	}
	// Embed metadata
	if err := WriteToPng(metadata, srcPath, dstPath); err != nil {
		t.Fatalf("WriteToPng failed: %v", err)
	}
	// Verify output file exists
	if _, err := os.Stat(dstPath); os.IsNotExist(err) {
		t.Fatalf("Output file not created: %v", err)
	}
	// Read and verify metadata
	t.Run("VerifyMetadata", func(t *testing.T) {
		data, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatalf("Error reading output file: %v", err)
		}
		// Verify PNG header
		if string(data[:8]) != pngHeader {
			t.Errorf("Invalid PNG header")
		}
		// Extract metadata
		embedded := extractMetadata(t, data)
		if embedded.Description != metadata.Description {
			t.Errorf("Metadata mismatch\nWant: %q\nGot:  %q",
				metadata.Description, embedded.Description)
		}
	})
	// Optional: Add cleanup if needed
	// t.Cleanup(func() {
	// 	os.Remove(dstPath)
	// })
}

// Helper to extract embedded metadata from PNG bytes
func extractMetadata(t *testing.T, data []byte) *models.CharCardSpec {
	r := bytes.NewReader(data[8:]) // Skip PNG header
	for {
		var length uint32
		if err := binary.Read(r, binary.BigEndian, &length); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("Error reading chunk length: %v", err)
		}
		chunkType := make([]byte, 4)
		if _, err := r.Read(chunkType); err != nil {
			t.Fatalf("Error reading chunk type: %v", err)
		}
		// Read chunk data
		chunkData := make([]byte, length)
		if _, err := r.Read(chunkData); err != nil {
			t.Fatalf("Error reading chunk data: %v", err)
		}
		// Read and discard CRC
		if _, err := r.Read(make([]byte, 4)); err != nil {
			t.Fatalf("Error reading CRC: %v", err)
		}
		if string(chunkType) == embType {
			parts := bytes.SplitN(chunkData, []byte{0}, 2)
			if len(parts) != 2 {
				t.Fatalf("Invalid tEXt chunk format")
			}
			decoded, err := base64.StdEncoding.DecodeString(string(parts[1]))
			if err != nil {
				t.Fatalf("Base64 decode error: %v", err)
			}
			var result models.CharCardSpec
			if err := json.Unmarshal(decoded, &result); err != nil {
				t.Fatalf("JSON unmarshal error: %v", err)
			}
			return &result
		}
	}
	t.Fatal("Metadata not found in PNG")
	return nil
}

func readTextChunk(t *testing.T, r io.ReadSeeker) *models.CharCardSpec {
	var length uint32
	binary.Read(r, binary.BigEndian, &length)
	chunkType := make([]byte, 4)
	r.Read(chunkType)
	data := make([]byte, length)
	r.Read(data)
	// Read CRC (but skip validation for test purposes)
	crc := make([]byte, 4)
	r.Read(crc)
	parts := bytes.SplitN(data, []byte{0}, 2) // Split key-value pair
	if len(parts) != 2 {
		t.Fatalf("Invalid tEXt chunk format")
	}
	// key := string(parts[0])
	value := parts[1]
	decoded, err := base64.StdEncoding.DecodeString(string(value))
	if err != nil {
		t.Fatalf("Base64 decode error: %v; value: %s", err, string(value))
	}
	var result models.CharCardSpec
	if err := json.Unmarshal(decoded, &result); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}
	return &result
}
