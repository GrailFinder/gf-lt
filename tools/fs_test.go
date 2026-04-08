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

func TestFsLs(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		check func(string) bool
	}{
		{"no args", []string{}, "", func(r string) bool { return strings.Contains(r, "tools/") }},
		{"long format", []string{"-l"}, "", func(r string) bool { return strings.Contains(r, "f  ") }},
		{"all files", []string{"-a"}, "", func(r string) bool { return strings.Contains(r, ".") || strings.Contains(r, "..") }},
		{"combine flags", []string{"-la"}, "", func(r string) bool { return strings.Contains(r, "f  ") && strings.Contains(r, ".") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FsLs(tt.args, tt.stdin)
			if !tt.check(result) {
				t.Errorf("check failed for %q, got %q", tt.name, result)
			}
		})
	}
}

func TestFsCat(t *testing.T) {
	tmpFile := filepath.Join(cfg.FilePickerDir, "test_cat.txt")
	content := "hello\nworld\n"
	os.WriteFile(tmpFile, []byte(content), 0644)
	defer os.Remove(tmpFile)

	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"file path", []string{tmpFile}, "", "hello\nworld\n"},
		{"stdin fallback", []string{}, "stdin content", "stdin content"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FsCat(tt.args, tt.stdin)
			if result != tt.want && !strings.Contains(result, tt.want) {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

func TestFsHead(t *testing.T) {
	tmpFile := filepath.Join(cfg.FilePickerDir, "test_head.txt")
	content := "line1\nline2\nline3\nline4\nline5\n"
	os.WriteFile(tmpFile, []byte(content), 0644)
	defer os.Remove(tmpFile)

	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"default from stdin", []string{}, "line1\nline2\nline3", "line1\nline2\nline3"},
		{"n from stdin", []string{"-n", "2"}, "line1\nline2\nline3", "line1\nline2"},
		{"numeric n", []string{"-2"}, "line1\nline2\nline3", "line1\nline2"},
		{"file path", []string{tmpFile}, "", "line1\nline2\nline3\nline4\nline5"},
		{"file with n", []string{"-n", "2", tmpFile}, "", "line1\nline2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FsHead(tt.args, tt.stdin)
			if result != tt.want && !strings.Contains(result, tt.want) {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

func TestFsTail(t *testing.T) {
	tmpFile := filepath.Join(cfg.FilePickerDir, "test_tail.txt")
	content := "line1\nline2\nline3\nline4\nline5\n"
	os.WriteFile(tmpFile, []byte(content), 0644)
	defer os.Remove(tmpFile)

	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"default from stdin", []string{}, "line1\nline2\nline3", "line1\nline2\nline3"},
		{"n from stdin", []string{"-n", "2"}, "line1\nline2\nline3", "line2\nline3"},
		{"file path", []string{tmpFile}, "", "line1\nline2\nline3\nline4\nline5"},
		{"file with n", []string{"-n", "3", tmpFile}, "", "line3\nline4\nline5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FsTail(tt.args, tt.stdin)
			if result != tt.want && !strings.Contains(result, tt.want) {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

func TestFsWc(t *testing.T) {
	tmpFile := filepath.Join(cfg.FilePickerDir, "test_wc.txt")
	content := "one two three\nfour five\nsix\n"
	os.WriteFile(tmpFile, []byte(content), 0644)
	defer os.Remove(tmpFile)

	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"default", []string{}, "one two", "1 lines, 2 words, 7 chars"},
		{"lines", []string{"-l"}, "line1\nline2\nline3", "3"},
		{"words", []string{"-w"}, "one two three", "3"},
		{"chars", []string{"-c"}, "abc", "3"},
		{"file lines", []string{"-l", tmpFile}, "", "3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FsWc(tt.args, tt.stdin)
			if !strings.Contains(result, tt.want) {
				t.Errorf("expected %q in output, got %q", tt.want, result)
			}
		})
	}
}

func TestFsSort(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"basic", []string{}, "c\na\nb\n", "a\nb\nc"},
		{"reverse", []string{"-r"}, "a\nb\nc", "c\nb\na"},
		{"numeric", []string{"-n"}, "10\n2\n1\n", "1\n2\n10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FsSort(tt.args, tt.stdin)
			if result != tt.want {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

func TestFsUniq(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"basic", []string{}, "a\nb\na\nc", "a\nb\nc"},
		{"count", []string{"-c"}, "a\na\nb", "2 a\n1 b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FsUniq(tt.args, tt.stdin)
			if result != tt.want && !strings.Contains(result, tt.want) {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

func TestFsGrep(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"basic", []string{"world"}, "hello\nworld\ntest", "world"},
		{"ignore case", []string{"-i", "WORLD"}, "hello\nworld\ntest", "world"},
		{"invert", []string{"-v", "world"}, "hello\nworld\ntest", "hello\ntest"},
		{"count", []string{"-c", "o"}, "hello\no world\no foo", "3"},
		{"no match", []string{"xyz"}, "hello\nworld", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FsGrep(tt.args, tt.stdin)
			if tt.want != "" && !strings.Contains(result, tt.want) {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

func TestFsEcho(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"single", []string{"hello"}, "", "hello\n"},
		{"multiple", []string{"hello", "world"}, "", "hello world\n"},
		{"with stdin", []string{}, "stdin", "stdin"},
		{"empty", []string{}, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FsEcho(tt.args, tt.stdin)
			if result != tt.want {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

func TestFsPwd(t *testing.T) {
	result := FsPwd(nil, "")
	if !strings.Contains(result, "gf-lt") {
		t.Errorf("expected gf-lt in path, got %q", result)
	}
}

func TestFsTime(t *testing.T) {
	result := FsTime(nil, "")
	if len(result) < 10 {
		t.Errorf("expected time output, got %q", result)
	}
}

func TestFsStat(t *testing.T) {
	tmpFile := filepath.Join(cfg.FilePickerDir, "test_stat.txt")
	os.WriteFile(tmpFile, []byte("content"), 0644)
	defer os.Remove(tmpFile)

	result := FsStat([]string{tmpFile}, "")
	if !strings.Contains(result, "test_stat.txt") {
		t.Errorf("expected filename in output, got %q", result)
	}
}

func TestFsMkdir(t *testing.T) {
	testDir := filepath.Join(cfg.FilePickerDir, "test_mkdir_xyz")
	defer os.RemoveAll(testDir)

	result := FsMkdir([]string{testDir}, "")
	if _, err := os.Stat(testDir); err != nil {
		t.Errorf("directory not created: %v, result: %q", err, result)
	}
}

func TestFsCp(t *testing.T) {
	src := filepath.Join(cfg.FilePickerDir, "test_cp_src.txt")
	dst := filepath.Join(cfg.FilePickerDir, "test_cp_dst.txt")
	os.WriteFile(src, []byte("test"), 0644)
	defer os.Remove(src)
	defer os.Remove(dst)

	result := FsCp([]string{src, dst}, "")
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("file not copied: %v, result: %q", err, result)
	}
}

func TestFsMv(t *testing.T) {
	src := filepath.Join(cfg.FilePickerDir, "test_mv_src.txt")
	dst := filepath.Join(cfg.FilePickerDir, "test_mv_dst.txt")
	os.WriteFile(src, []byte("test"), 0644)
	defer os.Remove(src)
	defer os.Remove(dst)

	result := FsMv([]string{src, dst}, "")
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("file not moved: %v, result: %q", err, result)
	}
	if _, err := os.Stat(src); err == nil {
		t.Errorf("source file still exists")
	}
}

func TestFsRm(t *testing.T) {
	tmpFile := filepath.Join(cfg.FilePickerDir, "test_rm_xyz.txt")
	os.WriteFile(tmpFile, []byte("test"), 0644)

	result := FsRm([]string{tmpFile}, "")
	if _, err := os.Stat(tmpFile); err == nil {
		t.Errorf("file not removed, result: %q", result)
	}
}

func TestFsSed(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"replace", []string{"s/hello/bye/"}, "hello world", "bye world"},
		{"global", []string{"s/o/X/g"}, "hello world", "hellX wXrld"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FsSed(tt.args, tt.stdin)
			if result != tt.want && !strings.Contains(result, tt.want) {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
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
		{"sort file", "sort " + tmpFile, func(r string) bool { return strings.Contains(r, "line1") }},
		{"grep file", "grep line1 " + tmpFile, func(r string) bool { return r == "line1" }},
		{"wc file", "wc -l " + tmpFile, func(r string) bool { return r == "3" }},
		{"head file", "head -2 " + tmpFile, func(r string) bool { return strings.Contains(r, "line3") }},
		{"tail file", "tail -2 " + tmpFile, func(r string) bool { return strings.Contains(r, "line2") }},
		{"echo | head", "echo a b c | head -2", func(r string) bool { return strings.Contains(r, "a") }},
		{"echo | wc -l", "echo a b c | wc -l", func(r string) bool { return r == "1" }},
		{"echo | sort", "echo c a b | sort", func(r string) bool { return strings.Contains(r, "a") }},
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
