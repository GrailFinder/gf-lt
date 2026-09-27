package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFsRead(t *testing.T) {
	tmp := filepath.Join(cfg.FilePickerDir, "test_read.txt")
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10"
	os.WriteFile(tmp, []byte(content), 0644)
	defer os.Remove(tmp)

	// Read whole file
	res := FsRead(map[string]string{"path": tmp})
	if !strings.Contains(res, "line1") || !strings.Contains(res, "line10") {
		t.Fatal("read whole file missing content:", res)
	}

	// Read with offset
	res = FsRead(map[string]string{"path": tmp, "offset": "3"})
	if !strings.Contains(res, "lines 3-10 of 10") {
		t.Error("read should announce the effective line range, got:", res[:60])
	}
	if !strings.Contains(res, "line3") {
		t.Fatal("read offset should start at line3, got:", res)
	}
	if !strings.Contains(res, "line10") {
		t.Fatal("read offset should include to end")
	}

	// Read with offset and limit
	res = FsRead(map[string]string{"path": tmp, "offset": "3", "limit": "2"})
	if !strings.Contains(res, "line3\nline4") {
		t.Fatal("read offset+limit wrong:", res)
	}
	if !strings.Contains(res, "6 more lines") {
		t.Fatal("expected truncation hint for offset+limit read")
	}

	// Offset beyond file
	res = FsRead(map[string]string{"path": tmp, "offset": "99"})
	if !strings.Contains(res, "exceeds") {
		t.Fatal("expected offset error, got:", res)
	}

	// Truncation hint
	res = FsRead(map[string]string{"path": tmp, "limit": "3"})
	if !strings.Contains(res, "7 more lines") {
		t.Fatal("expected truncation hint, got:", res)
	}
}

func TestFsReadImage(t *testing.T) {
	// Create a tiny valid PNG (1x1 red pixel)
	pngBytes := []byte(
		"\x89PNG\r\n\x1a\n" +
			"\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde" +
			"\x00\x00\x00\x0cIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x9a\x0a\x05" +
			"\x00\x00\x00\x00IEND\xaeB`\x82",
	)
	tmp := filepath.Join(cfg.FilePickerDir, "test_read.png")
	os.WriteFile(tmp, pngBytes, 0644)
	defer os.Remove(tmp)

	res := FsRead(map[string]string{"path": tmp})
	if !strings.Contains(res, "multimodal_content") {
		t.Fatal("expected multimodal_content for image, got:", res)
	}
	if !strings.Contains(res, "image_url") {
		t.Fatal("expected image_url in response")
	}
}

func TestFsWrite(t *testing.T) {
	tmp := filepath.Join(cfg.FilePickerDir, "test_write.json")
	defer os.Remove(tmp)

	// Create new file
	res := FsWrite(map[string]string{
		"file_path": tmp,
		"content":   "{\n  \"one\": \"1\",\n  \"two\": \"2\"\n}\n",
	})
	if strings.Contains(res, "[error]") {
		t.Fatal("write new file failed:", res)
	}
	data, _ := os.ReadFile(tmp)
	expected := "{\n  \"one\": \"1\",\n  \"two\": \"2\"\n}\n"
	if string(data) != expected {
		t.Fatalf("write new: got %q, want %q", string(data), expected)
	}

	// Overwrite (the counter.json use case!)
	res = FsWrite(map[string]string{
		"file_path": tmp,
		"content":   "{\n  \"one\": \"1\",\n  \"two\": \"2\",\n  \"three\": \"3\",\n  \"four\": \"4\",\n  \"five\": \"5\"\n}\n",
	})
	if strings.Contains(res, "[error]") {
		t.Fatal("write overwrite failed:", res)
	}
	data, _ = os.ReadFile(tmp)
	if !strings.Contains(string(data), "\"five\": \"5\"") {
		t.Fatal("overwrite didn't include new content")
	}
	if strings.Contains(string(data), "\"two\": \"2\"\n}") {
		t.Fatal("overwrite left old tail content")
	}
}

func TestFsEdit(t *testing.T) {
	tmp := filepath.Join(cfg.FilePickerDir, "test_edit.txt")
	defer os.Remove(tmp)
	os.WriteFile(tmp, []byte("hello world\nfoo bar\nhello again\n"), 0644)

	// Unique match
	res := FsEdit(map[string]string{
		"file_path": tmp,
		"old_text":  "foo bar",
		"new_text":  "baz qux",
	})
	if strings.Contains(res, "[error]") {
		t.Fatal("edit failed:", res)
	}
	data, _ := os.ReadFile(tmp)
	if !strings.Contains(string(data), "baz qux") {
		t.Fatal("edit didn't apply")
	}

	// Not found
	res = FsEdit(map[string]string{
		"file_path": tmp,
		"old_text":  "not in file",
		"new_text":  "whatever",
	})
	if !strings.Contains(res, "not found") {
		t.Fatal("expected 'not found' error, got:", res)
	}

	// Ambiguous match ("hello" appears twice)
	res = FsEdit(map[string]string{
		"file_path": tmp,
		"old_text":  "hello",
		"new_text":  "world",
	})
	if !strings.Contains(res, "unique") {
		t.Fatal("expected 'unique' error, got:", res)
	}
}

func TestFsWriteNested(t *testing.T) {
	nested := filepath.Join(cfg.FilePickerDir, "test_nested_a_b_c.txt")
	defer os.Remove(nested)
	defer os.Remove(filepath.Join(cfg.FilePickerDir, "test_nested"))

	res := FsWrite(map[string]string{
		"file_path": nested,
		"content":   "deep file",
	})
	if strings.Contains(res, "[error]") {
		t.Fatal("write nested failed:", res)
	}
	data, err := os.ReadFile(nested)
	if err != nil {
		t.Fatal("nested file not created:", err)
	}
	if string(data) != "deep file" {
		t.Fatal("nested content wrong:", string(data))
	}
}
