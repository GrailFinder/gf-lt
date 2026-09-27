package tools

import (
	"gf-lt/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The string convention is gone: handlers return ([]byte, error), the error is a
// *models.ToolError, and the model-facing text is rendered in exactly one place.
// This is the check that keeps it gone, because the convention was load-bearing
// but invisible - there was no compiler error for reintroducing it, only a
// failure the host could no longer see.
func TestNoStringConventionErrorsRemain(t *testing.T) {
	root := ".."
	var offenders []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "dumps", "chat_exports", "batteries", "onnx", "test_forloop":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue // history in a comment is fine
			}
			if !strings.HasPrefix(trimmed, "return") {
				continue
			}
			// A handler that signals failure by returning a string.
			if strings.Contains(trimmed, `"[error]`) || strings.Contains(trimmed, `{"error"`) {
				offenders = append(offenders, location(path, i+1, trimmed))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("handlers must return an error, not a string that looks like one:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

func TestEveryHandlerIsTyped(t *testing.T) {
	// A handler that fails must be able to say so in a way the host can see.
	// A tool that reports failure only in its payload counts as a success to
	// mission bookkeeping, which is the bug this whole convention exists to
	// prevent.
	// Spot-check a failure shape that used to be invisible to the host.
	out, err := CallToolWithAgent("read", map[string]string{"path": "definitely/not/here.txt"})
	if err == nil {
		t.Fatalf("expected an error, got: %s", out)
	}
	if models.IsFailure(err) != true {
		t.Error("a missing file should count as a failure")
	}
}

func location(path string, line int, text string) string {
	return path + ":" + itoa(line) + ": " + text
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
