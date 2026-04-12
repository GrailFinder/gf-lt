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

func TestUnixGlobExpansion(t *testing.T) {
	tmpDir := filepath.Join(cfg.FilePickerDir, "test_glob_tmp")
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file3.log"), []byte("content3"), 0644)

	tests := []struct {
		name    string
		cmd     string
		wantErr bool
		check   func(string) bool
	}{
		{
			name:    "ls glob txt files",
			cmd:     "ls " + tmpDir + "/*.txt",
			wantErr: false,
			check:   func(r string) bool { return strings.Contains(r, "file1.txt") && strings.Contains(r, "file2.txt") },
		},
		{
			name:    "cat glob txt files",
			cmd:     "cat " + tmpDir + "/*.txt",
			wantErr: false,
			check:   func(r string) bool { return strings.Contains(r, "content1") && strings.Contains(r, "content2") },
		},
		{
			name:    "ls glob no matches",
			cmd:     "ls " + tmpDir + "/*.nonexistent",
			wantErr: false,
			check:   func(r string) bool { return strings.Contains(r, "no such file") || strings.Contains(r, "(empty") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExecChain(tt.cmd)
			if tt.wantErr && result == "" {
				t.Errorf("expected error for %q, got empty", tt.cmd)
			}
			if !tt.check(result) {
				t.Errorf("check failed for %q, got %q", tt.cmd, result)
			}
		})
	}
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
		{
			name: "cat via shell with glob",
			cmd:  "cat " + tmpDir + "/*.txt",
			check: func(r string) bool {
				return strings.Contains(r, "file a content") && strings.Contains(r, "file b content")
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

func TestUnixGlobWithFileOps(t *testing.T) {
	tests := []struct {
		name  string
		cmd   string
		setup func() string
		check func(string) bool
	}{
		{
			name: "rm glob txt files",
			cmd:  "rm {dir}/*.txt",
			setup: func() string {
				tmpDir := filepath.Join(cfg.FilePickerDir, "test_rm_glob")
				os.MkdirAll(tmpDir, 0755)
				os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("content"), 0644)
				os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("content"), 0644)
				return tmpDir
			},
			check: func(r string) bool { return !strings.Contains(r, "[error]") },
		},
		{
			name: "cp glob to dest",
			cmd:  "cp {dir}/*.txt {dir}/dest/",
			setup: func() string {
				tmpDir := filepath.Join(cfg.FilePickerDir, "test_cp_glob")
				os.MkdirAll(tmpDir, 0755)
				os.MkdirAll(filepath.Join(tmpDir, "dest"), 0755)
				os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("content a"), 0644)
				os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("content b"), 0644)
				return tmpDir
			},
			check: func(r string) bool { return !strings.Contains(r, "[error]") },
		},
		{
			name: "mv glob to dest",
			cmd:  "mv {dir}/*.log {dir}/dest/",
			setup: func() string {
				tmpDir := filepath.Join(cfg.FilePickerDir, "test_mv_glob")
				os.MkdirAll(tmpDir, 0755)
				os.MkdirAll(filepath.Join(tmpDir, "dest"), 0755)
				os.WriteFile(filepath.Join(tmpDir, "c.log"), []byte("content c"), 0644)
				return tmpDir
			},
			check: func(r string) bool { return !strings.Contains(r, "[error]") },
		},
		{
			name: "ls with flags and glob",
			cmd:  "ls -la {dir}/*.txt",
			setup: func() string {
				tmpDir := filepath.Join(cfg.FilePickerDir, "test_ls_glob")
				os.MkdirAll(tmpDir, 0755)
				os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("content"), 0644)
				os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("content"), 0644)
				return tmpDir
			},
			check: func(r string) bool { return strings.Contains(r, "a.txt") || strings.Contains(r, "b.txt") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := tt.setup()
			defer os.RemoveAll(tmpDir)
			cmd := strings.ReplaceAll(tt.cmd, "{dir}", tmpDir)
			result := ExecChain(cmd)
			if !tt.check(result) {
				t.Errorf("check failed for %q, got %q", cmd, result)
			}
		})
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
