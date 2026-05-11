package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// FsFileEdit edits a file by replacing a line range with new content.
// Line range: {start, end} where start <= end for replace, start > end for insert-only.
func FsFileEdit(args map[string]string) string {
	filePath := args["file_path"]
	oldLinesJSON := args["old_lines"]
	newContent := args["new_content"]

	if filePath == "" {
		return "[error] file_path not provided"
	}
	if oldLinesJSON == "" {
		return "[error] old_lines not provided"
	}

	var r struct{ Start, End int }
	if err := json.Unmarshal([]byte(oldLinesJSON), &r); err != nil {
		return "[error] invalid old_lines JSON: " + err.Error()
	}

	abs, err := resolvePath(filePath)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("[error] read: %v", err)
	}
	lines := strings.Split(string(data), "\n")

	if r.Start < 1 {
		return "[error] start line must be >= 1"
	}
	if r.Start > len(lines) {
		return fmt.Sprintf("[error] start line %d exceeds file length (%d lines)", r.Start, len(lines))
	}

	startIdx := r.Start - 1

	if r.End >= r.Start {
		endIdx := r.End
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
	} else {
		newLines := strings.Split(newContent, "\n")

		result := make([]string, 0, len(lines)+len(newLines))
		result = append(result, lines[:startIdx]...)
		result = append(result, newLines...)
		result = append(result, lines[startIdx:]...)

		if err := os.WriteFile(abs, []byte(strings.Join(result, "\n")), 0644); err != nil {
			return fmt.Sprintf("[error] write: %v", err)
		}

		inserted := len(newLines)
		if inserted == 1 && newLines[0] == "" {
			return fmt.Sprintf("edited %s: no change", filePath)
		}
		return fmt.Sprintf("edited %s: inserted %d lines at line %d", filePath, inserted, r.Start)
	}
}