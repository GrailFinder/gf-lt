package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gf-lt/config"
)

func init() {
	cfg = &config.Config{}
	cwd, _ := os.Getwd()
	if strings.HasSuffix(cwd, "/tools") || strings.HasSuffix(cwd, "\\tools") {
		cwd = filepath.Dir(cwd)
	}
	cfg.FilePickerDir = cwd
}

func TestPiping(t *testing.T) {
	tmpFile := filepath.Join(cfg.FilePickerDir, "test_pipe.txt")
	os.WriteFile(tmpFile, []byte("line3\nline1\nline2"), 0644)
	defer os.Remove(tmpFile)

	tests := []struct {
		name  string
		cmd   string
		check func(string) bool
	}{
		{"ls | head -3", "ls | head -3", func(r string) bool { return r != "" }},
		{"head file", "head -2 " + tmpFile, func(r string) bool { return strings.Contains(r, "line3") }},
		{"tail file", "tail -2 " + tmpFile, func(r string) bool { return strings.Contains(r, "line2") }},
		{"echo | head", "echo a b c | head -2", func(r string) bool { return strings.Contains(r, "a") }},
		{"echo | grep", "echo hello world | grep hello", func(r string) bool { return strings.Contains(r, "hello") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExecChain(tt.cmd)
			if !tt.check(result) {
				t.Errorf("check failed for %q, got %q", tt.name, result)
			}
		})
	}
}

func TestChaining(t *testing.T) {
	tests := []struct {
		name  string
		cmd   string
		check func(string) bool
	}{
		{"ls && echo ok", "ls && echo ok", func(r string) bool { return strings.Contains(r, "ok") }},
		{"ls || echo not run", "ls || echo fallback", func(r string) bool { return !strings.Contains(r, "fallback") }},
		{"false || echo run", "cd /nonexistent123 || echo fallback", func(r string) bool { return strings.Contains(r, "fallback") }},
		{"echo a ; echo b", "echo a ; echo b", func(r string) bool { return strings.Contains(r, "a") && strings.Contains(r, "b") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExecChain(tt.cmd)
			if !tt.check(result) {
				t.Errorf("check failed for %q, got %q", tt.name, result)
			}
		})
	}
}

func TestRedirect(t *testing.T) {
	tmpFile := filepath.Join(cfg.FilePickerDir, "test_redirect.txt")
	os.Remove(tmpFile)
	defer os.Remove(tmpFile)

	// Test echo >
	result1 := ExecChain("echo hello world > " + tmpFile)
	if !strings.Contains(result1, "Wrote") {
		t.Errorf("echo > failed: %q", result1)
	}

	// Test cat
	result2 := ExecChain("cat " + tmpFile)
	if !strings.Contains(result2, "hello") {
		t.Errorf("cat failed: %q", result2)
	}

	// Test echo >>
	result3 := ExecChain("echo more >> " + tmpFile)
	if !strings.Contains(result3, "Appended") {
		t.Errorf("echo >> failed: %q", result3)
	}

	// Test cat after append
	result4 := ExecChain("cat " + tmpFile)
	if !strings.Contains(result4, "hello") || !strings.Contains(result4, "more") {
		t.Errorf("cat after append failed: %q", result4)
	}
}
