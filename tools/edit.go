package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
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
		if normalized := normalizeOLDJSON(oldLinesJSON); normalized != "" {
			if err2 := json.Unmarshal([]byte(normalized), &r); err2 == nil {
				goto parsed
			}
		}
		return "[error] invalid old_lines JSON: " + err.Error()
	}
parsed:

	abs, err := resolvePath(filePath)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}
	if currentMission != nil {
		currentMission.Log("FsFileEdit: file=%s -> abs=%s", filePath, abs)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("[error] read: %v", err)
	}
	lines := strings.Split(string(data), "\n")

	if r.Start < 1 {
		return "[error] start line must be >= 1"
	}
	if r.Start > len(lines)+1 {
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

var jsonKeyRE = regexp.MustCompile(`(\w+)\s*:`)

func normalizeOLDJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Check if it's already valid JSON (with quoted keys)
	if json.Unmarshal([]byte(s), &struct{}{}) == nil {
		return s
	}
	// Try wrapping unquoted keys in quotes: {start: 5, end: 20} -> {"start": 5, "end": 20}
	normalized := jsonKeyRE.ReplaceAllString(s, `"$1":`)
	if json.Unmarshal([]byte(normalized), &struct{}{}) == nil {
		return normalized
	}
	// Try single-quoted keys: {'start': 5} -> {"start": 5}
	normalized = strings.ReplaceAll(s, "'", `"`)
	normalized = jsonKeyRE.ReplaceAllString(normalized, `"$1":`)
	if json.Unmarshal([]byte(normalized), &struct{}{}) == nil {
		return normalized
	}
	return ""
}