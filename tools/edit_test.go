package tools

import (
	"errors"
	"gf-lt/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requireOK fails the test if the tool returned an error.
func requireOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// mustFail fails the test unless the tool returned an error, and returns it as a
// *models.ToolError for classification assertions. Asserting on the error value rather
// than on "[error]" appearing in the text is the point of the new convention.
func mustFail(t *testing.T, out string, err error) *models.ToolError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error, got output: %s", out)
	}
	var te *models.ToolError
	if !errors.As(err, &te) {
		t.Fatalf("expected a *models.ToolError, got %T: %v", err, err)
	}
	return te
}

func TestFsRead(t *testing.T) {
	tmp := filepath.Join(cfg.FilePickerDir, "test_read.txt")
	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10"
	os.WriteFile(tmp, []byte(content), 0644)
	defer os.Remove(tmp)

	// Read whole file
	res, err := FsRead(map[string]string{"path": tmp})
	requireOK(t, err)
	if !strings.Contains(res, "line1") || !strings.Contains(res, "line10") {
		t.Fatal("read whole file missing content:", res)
	}

	// Read with offset
	res, err = FsRead(map[string]string{"path": tmp, "offset": "3"})
	requireOK(t, err)
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
	res, err = FsRead(map[string]string{"path": tmp, "offset": "3", "limit": "2"})
	requireOK(t, err)
	if !strings.Contains(res, "line3\nline4") {
		t.Fatal("read offset+limit wrong:", res)
	}
	if !strings.Contains(res, "6 more lines") {
		t.Fatal("expected truncation hint for offset+limit read")
	}

	// Offset beyond file
	out, err := FsRead(map[string]string{"path": tmp, "offset": "99"})
	te := mustFail(t, out, err)
	if te.Code != models.CodeInvalidArgs {
		t.Errorf("offset past EOF should be invalid_args, got %s", te.Code)
	}
	if !strings.Contains(te.Msg, "exceeds") {
		t.Errorf("expected 'exceeds' in msg, got %q", te.Msg)
	}

	// Truncation hint
	res, err = FsRead(map[string]string{"path": tmp, "limit": "3"})
	requireOK(t, err)
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

	res, err := FsRead(map[string]string{"path": tmp})
	requireOK(t, err)
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
	_, err := FsWrite(map[string]string{
		"file_path": tmp,
		"content":   "{\n  \"one\": \"1\",\n  \"two\": \"2\"\n}\n",
	})
	requireOK(t, err)
	data, _ := os.ReadFile(tmp)
	expected := "{\n  \"one\": \"1\",\n  \"two\": \"2\"\n}\n"
	if string(data) != expected {
		t.Fatalf("write new: got %q, want %q", string(data), expected)
	}

	// Overwriting a non-empty file now needs the opt-in.
	_, err = FsWrite(map[string]string{
		"file_path": tmp,
		"content":   "{\n  \"one\": \"1\"\n}\n",
	})
	if err == nil {
		t.Fatal("overwriting a non-empty file should require overwrite=true")
	}
	_, err = FsWrite(map[string]string{
		"file_path": tmp,
		"content":   "{\n  \"one\": \"1\",\n  \"two\": \"2\",\n  \"three\": \"3\",\n  \"four\": \"4\",\n  \"five\": \"5\"\n}\n",
		"overwrite": "true",
	})
	requireOK(t, err)
	data, _ = os.ReadFile(tmp)
	if !strings.Contains(string(data), "\"five\": \"5\"") {
		t.Fatal("overwrite didn't include new content")
	}
	if strings.Contains(string(data), "\"two\": \"2\"\n}") {
		t.Fatal("overwrite left old tail content")
	}
}

func TestWriteReportsTheBlastRadius(t *testing.T) {
	dir := t.TempDir()
	prev := GetFSRoot()
	SetFSRoot(dir)
	t.Cleanup(func() { SetFSRoot(prev) })

	// A new file: say so, rather than implying it replaced something.
	p := filepath.Join(dir, "new.txt")
	out, err := FsWrite(map[string]string{"file_path": p, "content": "a\nb\n"})
	requireOK(t, err)
	if !strings.Contains(out, "created") {
		t.Errorf("a new file should be reported as created, got %q", out)
	}

	// The common failure: a 200-line file replaced by a 3-line stub. Nothing
	// about the call is wrong, so the result has to carry the fact.
	big := strings.Repeat("line\n", 200)
	p = filepath.Join(dir, "big.txt")
	_, err = FsWrite(map[string]string{"file_path": p, "content": big})
	requireOK(t, err)
	out, err = FsWrite(map[string]string{"file_path": p, "content": "stub\n", "overwrite": "true"})
	requireOK(t, err)
	if !strings.Contains(out, "was 200 lines, 1000 bytes") {
		t.Errorf("overwrite must report what was there before, got %q", out)
	}
	if !strings.HasPrefix(out, "wrote ") {
		t.Errorf("overwrite should read as a write, got %q", out)
	}

	// An existing but empty file is harmless to replace.
	p = filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(p, nil, 0644); err != nil {
		t.Fatal(err)
	}
	out, err = FsWrite(map[string]string{"file_path": p, "content": "x\n"})
	if err != nil {
		t.Fatalf("an empty file should be writable without the opt-in: %v", err)
	}
	if !strings.Contains(out, "was empty") {
		t.Errorf("overwriting an empty file should say so, got %q", out)
	}
}

func TestWriteOverwriteGuardIsSkipableWithTheFlag(t *testing.T) {
	dir := t.TempDir()
	prev := GetFSRoot()
	SetFSRoot(dir)
	t.Cleanup(func() { SetFSRoot(prev) })
	p := filepath.Join(dir, "f.txt")
	_, err := FsWrite(map[string]string{"file_path": p, "content": "original\n"})
	requireOK(t, err)

	out, err := FsWrite(map[string]string{"file_path": p, "content": "new\n"})
	if err == nil {
		t.Fatalf("expected a refusal, got %q", out)
	}
	// The refusal must not have touched the file.
	if data, _ := os.ReadFile(p); string(data) != "original\n" {
		t.Fatalf("a refused overwrite modified the file: %q", data)
	}
	// And it must say how to proceed, because the fix is one argument away.
	if !strings.Contains(err.Error(), "overwrite") {
		t.Errorf("the refusal should name the opt-in, got: %v", err)
	}
	// The hint is model-facing, so assert on the rendered result, not on Error().
	rendered := models.RenderToolResult("write", []byte(out), err)
	if !strings.Contains(string(rendered), "edit_text") {
		t.Errorf("the refusal should point at the right tool for partial edits, got: %s", rendered)
	}
	if !strings.Contains(string(rendered), "hint:") {
		t.Errorf("an agent-fixable refusal should carry a hint, got: %s", rendered)
	}
}

func TestFsEditText(t *testing.T) {
	tmp := filepath.Join(cfg.FilePickerDir, "test_edit.txt")
	defer os.Remove(tmp)
	os.WriteFile(tmp, []byte("hello world\nfoo bar\nhello again\n"), 0644)

	// Unique match
	_, err := FsEditText(map[string]string{
		"file_path": tmp,
		"old_text":  "foo bar",
		"new_text":  "baz qux",
	})
	requireOK(t, err)
	data, _ := os.ReadFile(tmp)
	if !strings.Contains(string(data), "baz qux") {
		t.Fatal("edit didn't apply")
	}

	// Not found -> not_found, and the file is untouched
	out, err := FsEditText(map[string]string{
		"file_path": tmp,
		"old_text":  "not in file",
		"new_text":  "whatever",
	})
	te := mustFail(t, out, err)
	if te.Code != models.CodeNotFound {
		t.Errorf("missing old_text should be not_found, got %s", te.Code)
	}

	// Ambiguous match ("hello" appears twice) -> conflict, with a hint, no mutation
	out, err = FsEditText(map[string]string{
		"file_path": tmp,
		"old_text":  "hello",
		"new_text":  "world",
	})
	te = mustFail(t, out, err)
	if te.Code != models.CodeConflict {
		t.Errorf("ambiguous match should be conflict, got %s", te.Code)
	}
	if te.Hint == "" {
		t.Error("an agent-fixable error should carry a hint")
	}
	data, _ = os.ReadFile(tmp)
	if !strings.Contains(string(data), "hello world") {
		t.Error("a rejected edit must not modify the file")
	}
}

func TestFsWriteNested(t *testing.T) {
	nested := filepath.Join(cfg.FilePickerDir, "test_nested_a_b_c.txt")
	defer os.Remove(nested)
	defer os.Remove(filepath.Join(cfg.FilePickerDir, "test_nested"))

	_, err := FsWrite(map[string]string{
		"file_path": nested,
		"content":   "deep file",
	})
	requireOK(t, err)
	data, err := os.ReadFile(nested)
	if err != nil {
		t.Fatal("nested file not created:", err)
	}
	if string(data) != "deep file" {
		t.Fatal("nested content wrong:", string(data))
	}
}

func TestEditLinesAppendReporting(t *testing.T) {
	dir := t.TempDir()
	prev := GetFSRoot()
	SetFSRoot(dir)
	t.Cleanup(func() { SetFSRoot(prev) })
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Appending at the position just past the last line. The old message said
	// "deleted 1 lines" here, counting the phantom line that a trailing newline
	// splits off - a false statement in a tool result.
	out, err := FsEditLines(map[string]string{
		"file_path": p, "start_line": "4", "end_line": "4", "new_content": "NEW",
	})
	requireOK(t, err)
	if strings.Contains(out, "deleted") {
		t.Errorf("appending deletes nothing, but the result claims otherwise: %q", out)
	}
	if !strings.Contains(out, "appended") {
		t.Errorf("append should be reported as an append: %q", out)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "a\nb\nc\nNEW" {
		t.Errorf("append produced %q", data)
	}

	// A start genuinely beyond the file is refused, not silently clamped.
	if _, err := FsEditLines(map[string]string{
		"file_path": p, "start_line": "99", "new_content": "NEW",
	}); err == nil {
		t.Error("start_line past the end should be refused, not silently clamped")
	}

	// A genuine in-place replacement still reports deletions.
	out, err = FsEditLines(map[string]string{
		"file_path": p, "start_line": "1", "end_line": "2", "new_content": "X",
	})
	requireOK(t, err)
	if !strings.Contains(out, "deleted 2 lines") {
		t.Errorf("in-place replacement should report the deletion: %q", out)
	}
}

// The file-mutation surface is three tools, and the split is by addressing mode:
// write (whole file), edit_text (by quoted text), edit_lines (by line number).
// The two edit tools differ in cost curve and in safety, so they are not merged;
// insert_at was dropped because every recorded use of it was an append, which
// edit_lines already does.
func TestFileMutationRoster(t *testing.T) {
	want := []string{"write", "edit_text", "edit_lines"}
	schemas := map[string]bool{}
	for _, name := range ToolSchemaNames() {
		schemas[name] = true
	}
	for _, name := range want {
		if !schemas[name] {
			t.Errorf("missing file-mutation tool: %s", name)
		}
		if _, ok := lookupVerb(name); !ok {
			t.Errorf("missing bash verb: %s", name)
		}
	}
	// The old names must be gone, in both the schema set and the verb table.
	for _, gone := range []string{"edit", "file_edit", "insert_at"} {
		if schemas[gone] {
			t.Errorf("stale schema name still advertised: %s", gone)
		}
		if _, ok := lookupVerb(gone); ok {
			t.Errorf("stale verb still registered: %s", gone)
		}
		if _, ok := FnMap[gone]; ok {
			t.Errorf("stale handler still registered: %s", gone)
		}
	}
}

func TestEditToolDescriptionsStateTheDecisionRule(t *testing.T) {
	// The re-litigation the issue complained about came from descriptions that
	// both said "replace" without saying when to prefer which.
	text := ToolDescription("edit_text")
	lines := ToolDescription("edit_lines")
	if !strings.Contains(text, "edit_lines") {
		t.Errorf("edit_text should point at edit_lines: %q", text)
	}
	if !strings.Contains(lines, "edit_text") {
		t.Errorf("edit_lines should point at edit_text: %q", lines)
	}
	if !strings.Contains(lines, "appends") {
		t.Errorf("edit_lines should document the append behaviour: %q", lines)
	}
}
