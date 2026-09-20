package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FsFileEdit edits a file by replacing a line range with new content.
// Accepts: file_path (required), start_line (required, 1-indexed),
// new_content (required), end_line (optional, defaults to start_line).
// Replaces lines [start_line, end_line] inclusive.
func FsFileEdit(args map[string]string) string {
	filePath := args["file_path"]
	newContent := args["new_content"]

	if filePath == "" {
		return "[error] file_path not provided"
	}
	startStr := args["start_line"]
	if startStr == "" {
		return "[error] start_line not provided"
	}
	start, err := strconv.Atoi(startStr)
	if err != nil || start < 1 {
		return "[error] start_line must be a positive integer"
	}

	endStr := args["end_line"]
	end := start
	if endStr != "" {
		end, err = strconv.Atoi(endStr)
		if err != nil || end < start {
			return "[error] end_line must be >= start_line"
		}
	}

	abs, err := resolvePath(filePath)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}
	if currentMission != nil {
		currentMission.Log("FsFileEdit: file=%s -> abs=%s, start=%d, end=%d", filePath, abs, start, end)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("[error] read: %v", err)
	}
	lines := strings.Split(string(data), "\n")

	if start > len(lines) {
		return fmt.Sprintf("[error] start_line %d exceeds file length (%d lines)", start, len(lines))
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
		return fmt.Sprintf("[error] write: %v", err)
	}

	deleted := endIdx - startIdx
	inserted := len(newLines)
	if inserted == 0 || (inserted == 1 && newLines[0] == "") {
		return fmt.Sprintf("edited %s: deleted %d lines", filePath, deleted)
	}
	return fmt.Sprintf("edited %s: deleted %d lines, inserted %d lines", filePath, deleted, inserted)
}

// FsRead reads a file with optional offset and limit (1-indexed lines).
// Accepts: path (required), offset (optional, line to start from, default 1),
// limit (optional, max lines to read, default 2000).
func FsRead(args map[string]string) string {
	path := args["path"]
	if path == "" {
		return "[error] path not provided"
	}

	abs, err := resolvePath(path)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}
	if currentMission != nil {
		currentMission.Log("FsRead: path=%s -> abs=%s, offset=%s, limit=%s", path, abs, args["offset"], args["limit"])
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("[error] read: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	offset := 1
	if offStr := args["offset"]; offStr != "" {
		off, err := strconv.Atoi(offStr)
		if err != nil || off < 1 {
			return "[error] offset must be a positive integer"
		}
		offset = off
	}

	limit := 2000
	if limStr := args["limit"]; limStr != "" {
		lim, err := strconv.Atoi(limStr)
		if err != nil || lim < 1 {
			return "[error] limit must be a positive integer"
		}
		limit = lim
	}

	if offset > totalLines {
		return fmt.Sprintf("[error] offset %d exceeds file length (%d lines)", offset, totalLines)
	}

	startIdx := offset - 1
	endIdx := startIdx + limit
	if endIdx > totalLines {
		endIdx = totalLines
	}

	result := strings.Join(lines[startIdx:endIdx], "\n")
	remaining := totalLines - endIdx
	if remaining > 0 {
		result += fmt.Sprintf("\n[%d more lines in file. Use offset=%d to continue.]", remaining, endIdx+1)
	}

	return result
}

// FsWrite overwrites a file with new content. Creates parent directories if needed.
// Accepts: file_path (required), content (required).
func FsWrite(args map[string]string) string {
	filePath := args["file_path"]
	content := args["content"]

	if filePath == "" {
		return "[error] file_path not provided"
	}

	abs, err := resolvePath(filePath)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}
	if currentMission != nil {
		currentMission.Log("FsWrite: file=%s -> abs=%s, len=%d", filePath, abs, len(content))
	}

	// Create parent directories if needed
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return fmt.Sprintf("[error] mkdir: %v", err)
	}

	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return fmt.Sprintf("[error] write: %v", err)
	}

	lines := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		lines++
	}
	return fmt.Sprintf("wrote %s (%d lines, %d bytes)", filePath, lines, len(content))
}

// FsEdit replaces an exact text match in a file with new text.
// Accepts: file_path (required), old_text (required), new_text (required).
// The old_text must appear exactly once in the file (ambiguous matches are rejected).
func FsEdit(args map[string]string) string {
	filePath := args["file_path"]
	oldText := args["old_text"]
	newText := args["new_text"]

	if filePath == "" {
		return "[error] file_path not provided"
	}
	if oldText == "" {
		return "[error] old_text not provided"
	}

	abs, err := resolvePath(filePath)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}
	if currentMission != nil {
		currentMission.Log("FsEdit: file=%s, old_text_len=%d, new_text_len=%d", filePath, len(oldText), len(newText))
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("[error] read: %v", err)
	}

	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		return fmt.Sprintf("[error] old_text not found in %s", filePath)
	}
	if count > 1 {
		return fmt.Sprintf("[error] old_text found %d times in %s — must be unique", count, filePath)
	}

	updated := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(abs, []byte(updated), 0644); err != nil {
		return fmt.Sprintf("[error] write: %v", err)
	}

	deletedLines := strings.Count(oldText, "\n")
	insertedLines := strings.Count(newText, "\n")
	if deletedLines == 0 && insertedLines == 0 {
		return fmt.Sprintf("edited %s: replaced %d chars with %d chars", filePath, len(oldText), len(newText))
	}
	return fmt.Sprintf("edited %s: replaced %d lines with %d lines", filePath, deletedLines+1, insertedLines+1)
}

// FsInsertAt inserts new content before a given line number.
// Accepts: file_path (required), line (required, 1-indexed position to insert before),
// new_content (required).
// If line > file_length, appends to end of file.
func FsInsertAt(args map[string]string) string {
	filePath := args["file_path"]
	newContent := args["new_content"]
	lineStr := args["line"]

	if filePath == "" {
		return "[error] file_path not provided"
	}
	if newContent == "" {
		return "[error] new_content not provided"
	}
	if lineStr == "" {
		return "[error] line not provided"
	}

	line, err := strconv.Atoi(lineStr)
	if err != nil || line < 1 {
		return "[error] line must be a positive integer"
	}

	abs, err := resolvePath(filePath)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}
	if currentMission != nil {
		currentMission.Log("FsInsertAt: file=%s -> abs=%s, line=%d", filePath, abs, line)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("[error] read: %v", err)
	}
	lines := strings.Split(string(data), "\n")

	insertIdx := line - 1
	if insertIdx > len(lines) {
		insertIdx = len(lines)
	}

	newLines := strings.Split(newContent, "\n")

	result := make([]string, 0, len(lines)+len(newLines))
	result = append(result, lines[:insertIdx]...)
	result = append(result, newLines...)
	result = append(result, lines[insertIdx:]...)

	if err := os.WriteFile(abs, []byte(strings.Join(result, "\n")), 0644); err != nil {
		return fmt.Sprintf("[error] write: %v", err)
	}

	inserted := len(newLines)
	if inserted == 1 && newLines[0] == "" {
		return fmt.Sprintf("edited %s: no change", filePath)
	}
	return fmt.Sprintf("edited %s: inserted %d lines at line %d", filePath, inserted, line)
}
