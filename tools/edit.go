package tools

import (
	"fmt"
	"gf-lt/models"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FsEditLines replaces an inclusive line range with new content.
//
// The addressing mode is the point: the caller names lines, not text. That is
// what makes it the right tool for multi-line content, where quoting the
// existing text back exactly is expensive and error-prone. FsEditText is the
// mirror image, for the case where the caller can quote the text.
//
// Accepts: file_path (required), start_line (required, 1-indexed),
// new_content (required), end_line (optional, defaults to start_line).
// A start_line past the end of the file appends.
func FsEditLines(args map[string]string) (string, error) {
	filePath := args["file_path"]
	newContent := args["new_content"]

	if filePath == "" {
		return "", models.InvalidArgs("file_path is required", "")
	}
	startStr := args["start_line"]
	if startStr == "" {
		return "", models.InvalidArgs("start_line is required", "")
	}
	start, err := strconv.Atoi(startStr)
	if err != nil || start < 1 {
		return "", models.InvalidArgs("start_line must be a positive integer", "")
	}

	endStr := args["end_line"]
	end := start
	if endStr != "" {
		end, err = strconv.Atoi(endStr)
		if err != nil || end < start {
			return "", models.InvalidArgs("end_line must be >= start_line", "")
		}
	}

	abs, err := resolvePath(filePath)
	if err != nil {
		return "", models.Internal("could not resolve path", err)
	}
	if currentMission != nil {
		currentMission.Log("FsEditLines: file=%s -> abs=%s, start=%d, end=%d", filePath, abs, start, end)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", readError(filePath, err)
	}
	lines := strings.Split(string(data), "\n")

	if start > len(lines) {
		return "", models.InvalidArgs(fmt.Sprintf("start_line %d exceeds file length (%d lines)", start, len(lines)), "use stat or wc -l to get the line count first")
	}

	startIdx := start - 1
	endIdx := end
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	newLines := strings.Split(newContent, "\n")

	result := make([]string, 0, len(lines)-endIdx+startIdx+len(newLines))
	result = append(result, lines[:startIdx]...)
	result = append(result, newLines...)
	result = append(result, lines[endIdx:]...)

	if err := os.WriteFile(abs, []byte(strings.Join(result, "\n")), 0644); err != nil {
		return "", models.Internal("could not write "+filePath, err)
	}

	inserted := len(newLines)
	// A file ending in a newline splits into a trailing empty element, so
	// appending at start_line == len(lines) consumes that phantom line. Counting
	// it as a deletion is a false statement in a tool result - the same class of
	// thing the error convention work removed.
	phantom := lines[len(lines)-1] == ""
	deleted := endIdx - startIdx
	appended := startIdx >= len(lines) || (phantom && endIdx == len(lines))
	if appended && phantom {
		deleted--
	}
	switch {
	case deleted <= 0:
		return fmt.Sprintf("edited %s: appended %d lines at end of file", filePath, inserted), nil
	case inserted == 0 || (inserted == 1 && newLines[0] == ""):
		return fmt.Sprintf("edited %s: deleted %d lines", filePath, deleted), nil
	case appended:
		return fmt.Sprintf("edited %s: deleted %d lines, appended %d lines", filePath, deleted, inserted), nil
	}
	return fmt.Sprintf("edited %s: deleted %d lines, inserted %d lines", filePath, deleted, inserted), nil
}

// FsRead reads a file with optional offset and limit (1-indexed lines).
// Accepts: path (required), offset (optional, line to start from, default 1),
// limit (optional, max lines to read, default 2000).
//
// The response is always prefixed with a header naming the effective offset and
// limit, so a truncated read is distinguishable at a glance from a complete one
// - previously the limit was only visible from the trailer, and only when
// truncation actually happened.
func FsRead(args map[string]string) (string, error) {
	path := args["path"]
	if path == "" {
		return "", models.InvalidArgs("path is required", "")
	}

	abs, err := resolvePath(path)
	if err != nil {
		return "", models.Internal("could not resolve path", err)
	}
	if currentMission != nil {
		currentMission.Log("FsRead: path=%s -> abs=%s, offset=%s, limit=%s", path, abs, args["offset"], args["limit"])
	}

	// If it's an image, delegate to view_img (returns multimodal_content JSON)
	if IsImageFile(abs) {
		return FsViewImg([]string{abs}, "")
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", readError(path, err)
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	offset := 1
	if offStr := args["offset"]; offStr != "" {
		off, err := strconv.Atoi(offStr)
		if err != nil || off < 1 {
			return "", models.InvalidArgs("offset must be a positive integer", "")
		}
		offset = off
	}

	limit := 2000
	if limStr := args["limit"]; limStr != "" {
		lim, err := strconv.Atoi(limStr)
		if err != nil || lim < 1 {
			return "", models.InvalidArgs("limit must be a positive integer", "")
		}
		limit = lim
	}

	if offset > totalLines {
		return "", models.InvalidArgs(fmt.Sprintf("offset %d exceeds file length (%d lines)", offset, totalLines), "the file ends at line %d; read the tail with offset")
	}

	startIdx := offset - 1
	endIdx := startIdx + limit
	if endIdx > totalLines {
		endIdx = totalLines
	}

	result := strings.Join(lines[startIdx:endIdx], "\n")
	remaining := totalLines - endIdx
	header := fmt.Sprintf("[%s: lines %d-%d of %d (limit %d)]\n",
		path, offset, endIdx, totalLines, limit)
	if remaining > 0 {
		header += fmt.Sprintf("[truncated: %d more lines. Continue with offset=%d]\n", remaining, endIdx+1)
	}
	return header + result, nil
}

// maxStatReadBytes bounds the read used to count a file's lines for the
// before/after report. Above this the report falls back to bytes, which is still
// enough to tell the model it just destroyed something.
const maxStatReadBytes = 1 << 20

// FsWrite overwrites a file with new content. Creates parent directories if needed.
// Accepts: file_path (required), content (required), overwrite (optional),
// truncate (optional).
//
// `write` is the only mutation whose arguments say nothing about what survives.
// edit_lines replaces a named range, so a mistake is reversible; edit_text needs
// the old text, so a stale edit usually fails. Here, `write(path, content)`
// carries no reference to the previous content at all, and that asymmetry is why
// it gets two guards the others do not need:
//
//  1. The result reports what was there before, because the most common agent
//     failure here is not a stale write - it is reaching for `write` and
//     replacing a 400-line file with a 3-line stub. "was 412 lines, now 3" is
//     visible in the tool result; nothing about the call is wrong.
//
//  2. Overwriting a non-empty file requires overwrite="true". Same shape as the
//     truncate opt-in below: the destructive act has to be spelled.
//
// Note this is deliberately not a "you must read the file first" precondition.
// That would test the agent's process rather than the world - a read fifty turns
// ago satisfies it and leaves the actual hazard untouched - and a passing
// preflight reads to the model as "this write is verified", which suppresses the
// re-read that would actually have caught the problem. For real staleness
// detection the right shape is an opt-in claim about the world (expect_sha256),
// not a gate on the agent.
func FsWrite(args map[string]string) (string, error) {
	filePath := args["file_path"]
	content := args["content"]

	if filePath == "" {
		return "", models.InvalidArgs("file_path is required", "")
	}

	overwrite, err := boolArg(args, "overwrite")
	if err != nil {
		return "", models.InvalidArgs("overwrite must be \"true\" or \"false\"", "")
	}
	truncateOK, err := boolArg(args, "truncate")
	if err != nil {
		return "", models.InvalidArgs("truncate must be \"true\" or \"false\"", "")
	}

	if content == "" && !truncateOK {
		return "", &models.ToolError{
			Code: models.CodeConflict,
			Msg:  "refusing to truncate " + filePath + " to 0 bytes: content is empty",
			Hint: "to clear a file deliberately, pass truncate=\"true\" alongside empty content; to delete it, use the bash tool: rm " + filePath,
		}
	}

	abs, err := resolvePath(filePath)
	if err != nil {
		return "", models.Internal("could not resolve path", err)
	}
	if currentMission != nil {
		currentMission.Log("FsWrite: file=%s -> abs=%s, len=%d", filePath, abs, len(content))
	}

	// What is there now, before it is gone.
	previous, existed := describeTarget(abs)

	if existed && previous.lines > 0 && !overwrite {
		return "", &models.ToolError{
			Code: models.CodeConflict,
			Msg: fmt.Sprintf("refusing to overwrite %s (%s) with write; pass overwrite=\"true\" to replace it",
				filePath, previous.size()),
			Hint: "to change part of a file, edit_text (quote the old text) or edit_lines (name a line range) instead - both keep the rest of the file",
		}
	}

	// Create parent directories if needed
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return "", models.Internal("mkdir failed", err)
	}

	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return "", models.Internal("could not write "+filePath, err)
	}

	lines := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		lines++
	}
	if content == "" {
		return fmt.Sprintf("truncated %s to 0 bytes (explicit truncate=true, was %s)", filePath, previous.size()), nil
	}
	if !existed {
		return fmt.Sprintf("created %s (%d lines, %d bytes)", filePath, lines, len(content)), nil
	}
	if previous.lines == 0 {
		return fmt.Sprintf("wrote %s (%d lines, %d bytes; was empty)", filePath, lines, len(content)), nil
	}
	return fmt.Sprintf("wrote %s (%d lines, %d bytes; was %d lines, %d bytes)",
		filePath, lines, len(content), previous.lines, previous.bytes), nil
}

// boolArg reads an optional tri-state flag. An absent or empty value is false.
func boolArg(args map[string]string, name string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(args[name])) {
	case "", "false", "no", "0":
		return false, nil
	case "true", "yes", "1":
		return true, nil
	}
	return false, fmt.Errorf("%s must be true or false", name)
}

type targetSize struct {
	lines int
	bytes int64
}

func (t targetSize) size() string {
	// A description of what the file held before this write replaced it.
	if t.lines > 0 {
		return fmt.Sprintf("%d lines, %d bytes", t.lines, t.bytes)
	}
	return fmt.Sprintf("%d bytes", t.bytes)
}

// describeTarget reports what a file currently holds, for the before/after line
// in a write result. A file too large to read cheaply is described in bytes, which
// is still enough to show that something was destroyed.
func describeTarget(abs string) (targetSize, bool) {
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return targetSize{}, false
	}
	out := targetSize{bytes: info.Size()}
	if info.Size() <= maxStatReadBytes {
		if data, readErr := os.ReadFile(abs); readErr == nil {
			out.lines = strings.Count(string(data), "\n")
			if out.lines == 0 && len(data) > 0 {
				out.lines = 1
			}
		}
	}
	return out, true
}

// FsEditText replaces an exact text match with new text.
//
// The caller quotes the existing text, which is why this is the right tool for
// small, distinctive changes and the wrong one for anything long: the model has
// to reproduce the old text byte-for-byte. FsEditLines is the mirror image.
//
// The old_text must appear exactly once in the file; ambiguous matches are
// rejected rather than guessed. That guarantee is why this is not merged into
// FsEditLines - merged, it would become conditional, and a conditional safety
// property is one a model learns to route around.
//
// Accepts: file_path (required), old_text (required), new_text (required).
func FsEditText(args map[string]string) (string, error) {
	filePath := args["file_path"]
	oldText := args["old_text"]
	newText := args["new_text"]

	if filePath == "" {
		return "", models.InvalidArgs("file_path is required", "")
	}
	if oldText == "" {
		return "", models.InvalidArgs("old_text is required", "")
	}

	abs, err := resolvePath(filePath)
	if err != nil {
		return "", models.Internal("could not resolve path", err)
	}
	if currentMission != nil {
		currentMission.Log("FsEditText: file=%s, old_text_len=%d, new_text_len=%d", filePath, len(oldText), len(newText))
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", readError(filePath, err)
	}

	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		return "", models.NotFound(fmt.Sprintf("old_text not found in %s", filePath), "read the file to get the exact current text, whitespace included")
	}
	if count > 1 {
		return "", models.Conflict(fmt.Sprintf("old_text found %d times in %s; it must match exactly one location", count, filePath), "include more surrounding context in old_text to make the match unique, or use edit_lines with a line range instead")
	}

	updated := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(abs, []byte(updated), 0644); err != nil {
		return "", models.Internal("could not write "+filePath, err)
	}

	deletedLines := strings.Count(oldText, "\n")
	insertedLines := strings.Count(newText, "\n")
	if deletedLines == 0 && insertedLines == 0 {
		return fmt.Sprintf("edited %s: replaced %d chars with %d chars", filePath, len(oldText), len(newText)), nil
	}
	return fmt.Sprintf("edited %s: replaced %d lines with %d lines", filePath, deletedLines+1, insertedLines+1), nil
}
