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

func TestUnixCatMultipleFiles(t *testing.T) {
	tmpDir := filepath.Join(cfg.FilePickerDir, "test_cat_multi")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("file a content\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("file b content\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "c.txt"), []byte("file c content\n"), 0644)

	tests := []struct {
		name  string
		cmd   string
		check func(string) bool
	}{
		{
			name: "cat multiple files with paths",
			cmd:  "cat " + tmpDir + "/a.txt " + tmpDir + "/b.txt",
			check: func(r string) bool {
				return strings.Contains(r, "file a content") && strings.Contains(r, "file b content")
			},
		},
		{
			name: "cat three files",
			cmd:  "cat " + tmpDir + "/a.txt " + tmpDir + "/b.txt " + tmpDir + "/c.txt",
			check: func(r string) bool {
				return strings.Contains(r, "file a content") && strings.Contains(r, "file b content") && strings.Contains(r, "file c content")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExecChain(tt.cmd)
			if !tt.check(result) {
				t.Errorf("check failed for %q, got %q", tt.cmd, result)
			}
		})
	}
}

func TestUnixGrepPatternQuoting(t *testing.T) {
	tmpDir := filepath.Join(cfg.FilePickerDir, "test_grep_quote")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "animals.txt"), []byte("dog\ncat\nbird\nfish\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "colors.txt"), []byte("red\nblue\ngreen\n"), 0644)

	tests := []struct {
		name  string
		cmd   string
		check func(string) bool
	}{
		{
			name:  "grep with double quotes OR pattern",
			cmd:   "grep -E \"dog|cat\" " + tmpDir + "/animals.txt",
			check: func(r string) bool { return strings.Contains(r, "dog") && strings.Contains(r, "cat") },
		},
		{
			name:  "grep with single quotes OR pattern",
			cmd:   "grep -E 'dog|cat' " + tmpDir + "/animals.txt",
			check: func(r string) bool { return strings.Contains(r, "dog") && strings.Contains(r, "cat") },
		},
		{
			name:  "grep case insensitive with quotes",
			cmd:   "grep -iE \"DOG|CAT\" " + tmpDir + "/animals.txt",
			check: func(r string) bool { return strings.Contains(r, "dog") && strings.Contains(r, "cat") },
		},
		{
			name:  "grep piped from cat",
			cmd:   "cat " + tmpDir + "/animals.txt | grep -E \"dog|cat\"",
			check: func(r string) bool { return strings.Contains(r, "dog") && strings.Contains(r, "cat") },
		},
		{
			name: "grep with complex pattern",
			cmd:  "grep -E \"red|blue|green\" " + tmpDir + "/colors.txt",
			check: func(r string) bool {
				return strings.Contains(r, "red") && strings.Contains(r, "blue") && strings.Contains(r, "green")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExecChain(tt.cmd)
			if !tt.check(result) {
				t.Errorf("check failed for %q, got %q", tt.cmd, result)
			}
		})
	}
}

func TestUnixForLoop(t *testing.T) {
	tmpDir := filepath.Join(cfg.FilePickerDir, "test_forloop")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "dog.txt"), []byte("I have a dog\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "cat.txt"), []byte("I have a cat\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "red.txt"), []byte("red color\n"), 0644)

	result := ExecChain("cd " + tmpDir + " && for f in *.txt; do echo \"file: $f\"; done")
	if result == "" {
		t.Error("empty result from for loop execution")
	}
	if strings.Contains(result, "file:") {
		t.Logf("for loop is supported: %s", result)
	} else {
		t.Logf("for loops not supported (expected): %s", result)
	}
}

func TestUnixComplexPiping(t *testing.T) {
	tmpDir := filepath.Join(cfg.FilePickerDir, "test_pipe_complex")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "data.txt"), []byte("apple\nbanana\nAPPLE\ncherry\nbanana\n"), 0644)

	tests := []struct {
		name  string
		cmd   string
		check func(string) bool
	}{
		{
			name:  "cat | grep -i | sort",
			cmd:   "cat " + tmpDir + "/data.txt | grep -i apple | sort",
			check: func(r string) bool { return strings.Contains(r, "apple") && !strings.Contains(r, "banana") },
		},
		{
			name:  "ls | wc -l",
			cmd:   "ls " + tmpDir + " | wc -l",
			check: func(r string) bool { return strings.TrimSpace(r) == "1" },
		},
		{
			name:  "echo > file && cat file",
			cmd:   "echo 'hello world' > " + tmpDir + "/out.txt && cat " + tmpDir + "/out.txt",
			check: func(r string) bool { return strings.Contains(r, "hello world") },
		},
		{
			name:  "grep file | head -2",
			cmd:   "grep a " + tmpDir + "/data.txt | head -2",
			check: func(r string) bool { return strings.Contains(r, "apple") || strings.Contains(r, "banana") },
		},
		{
			name:  "cat | grep | wc -l",
			cmd:   "cat " + tmpDir + "/data.txt | grep -i apple | wc -l",
			check: func(r string) bool { return strings.TrimSpace(r) == "2" },
		},
		{
			name:  "ls | grep txt | head -1",
			cmd:   "ls " + tmpDir + " | grep txt | head -1",
			check: func(r string) bool { return strings.Contains(r, "data.txt") },
		},
		{
			name:  "echo | sed replacement",
			cmd:   "echo 'hello world' | sed 's/world/universe/'",
			check: func(r string) bool { return strings.Contains(r, "hello universe") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExecChain(tt.cmd)
			if !tt.check(result) {
				t.Errorf("check failed for %q, got %q", tt.cmd, result)
			}
		})
	}
}
