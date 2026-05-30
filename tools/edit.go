package tools

import (
	"fmt"
	"os"
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
