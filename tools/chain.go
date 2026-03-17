package tools

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Operator represents a chain operator between commands.
type Operator int

const (
	OpNone Operator = iota
	OpAnd           // &&
	OpOr            // ||
	OpSeq           // ;
	OpPipe          // |
)

// Segment is a single command in a chain.
type Segment struct {
	Raw string
	Op  Operator // operator AFTER this segment
}

// ParseChain splits a command string into segments by &&, ;, and |.
// Respects quoted strings (single and double quotes).
func ParseChain(input string) []Segment {
	var segments []Segment
	var current strings.Builder
	runes := []rune(input)
	n := len(runes)
	for i := 0; i < n; i++ {
		ch := runes[i]
		// handle quotes
		if ch == '\'' || ch == '"' {
			quote := ch
			current.WriteRune(ch)
			i++
			for i < n && runes[i] != quote {
				current.WriteRune(runes[i])
				i++
			}
			if i < n {
				current.WriteRune(runes[i])
			}
			continue
		}
		// &&
		if ch == '&' && i+1 < n && runes[i+1] == '&' {
			segments = append(segments, Segment{
				Raw: strings.TrimSpace(current.String()),
				Op:  OpAnd,
			})
			current.Reset()
			i++ // skip second &
			continue
		}
		// ;
		if ch == ';' {
			segments = append(segments, Segment{
				Raw: strings.TrimSpace(current.String()),
				Op:  OpSeq,
			})
			current.Reset()
			continue
		}
		// ||
		if ch == '|' && i+1 < n && runes[i+1] == '|' {
			segments = append(segments, Segment{
				Raw: strings.TrimSpace(current.String()),
				Op:  OpOr,
			})
			current.Reset()
			i++ // skip second |
			continue
		}
		// | (single pipe)
		if ch == '|' {
			segments = append(segments, Segment{
				Raw: strings.TrimSpace(current.String()),
				Op:  OpPipe,
			})
			current.Reset()
			continue
		}
		current.WriteRune(ch)
	}
	// last segment
	last := strings.TrimSpace(current.String())
	if last != "" {
		segments = append(segments, Segment{Raw: last, Op: OpNone})
	}
	return segments
}

// ExecChain executes a command string with pipe/chaining support.
// Returns the combined output of all commands.
func ExecChain(command string) string {
	segments := ParseChain(command)
	if len(segments) == 0 {
		return "[error] empty command"
	}
	var collected []string
	var lastOutput string
	var lastErr error
	pipeInput := ""
	for i, seg := range segments {
		if i > 0 {
			prevOp := segments[i-1].Op
			// && semantics: skip if previous failed
			if prevOp == OpAnd && lastErr != nil {
				continue
			}
			// || semantics: skip if previous succeeded
			if prevOp == OpOr && lastErr == nil {
				continue
			}
		}
		// determine stdin for this segment
		segStdin := ""
		if i == 0 {
			segStdin = pipeInput
		} else if segments[i-1].Op == OpPipe {
			segStdin = lastOutput
		}
		lastOutput, lastErr = execSingle(seg.Raw, segStdin)
		// pipe: output flows to next command's stdin
		// && or ;: collect output
		if i < len(segments)-1 && seg.Op == OpPipe {
			continue
		}
		if lastOutput != "" {
			collected = append(collected, lastOutput)
		}
	}
	return strings.Join(collected, "\n")
}

// execSingle executes a single command (with arguments) and returns output and error.
func execSingle(command, stdin string) (string, error) {
	parts := tokenize(command)
	if len(parts) == 0 {
		return "", errors.New("empty command")
	}
	name := parts[0]
	args := parts[1:]
	// Check if it's a built-in Go command
	if result, isBuiltin := execBuiltin(name, args, stdin); isBuiltin {
		return result, nil
	}
	// Otherwise execute as system command
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

// tokenize splits a command string by whitespace, respecting quotes.
func tokenize(input string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	var quoteChar rune
	for _, ch := range input {
		if inQuote {
			if ch == quoteChar {
				inQuote = false
			} else {
				current.WriteRune(ch)
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			inQuote = true
			quoteChar = ch
			continue
		}
		if ch == ' ' || ch == '\t' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// execBuiltin executes a built-in command if it exists.
// Returns (result, true) if it was a built-in (even if result is empty).
// Returns ("", false) if it's not a built-in command.
func execBuiltin(name string, args []string, stdin string) (string, bool) {
	switch name {
	case "echo":
		if stdin != "" {
			return stdin, true
		}
		return strings.Join(args, " "), true
	case "time":
		return "2006-01-02 15:04:05 MST", true
	case "cat":
		if len(args) == 0 {
			if stdin != "" {
				return stdin, true
			}
			return "", true
		}
		path := args[0]
		abs := path
		if !filepath.IsAbs(path) {
			abs = filepath.Join(cfg.FilePickerDir, path)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("[error] cat: %v", err), true
		}
		return string(data), true
	case "pwd":
		return cfg.FilePickerDir, true
	case "cd":
		if len(args) == 0 {
			return "[error] usage: cd <dir>", true
		}
		dir := args[0]
		// Resolve relative to cfg.FilePickerDir
		abs := dir
		if !filepath.IsAbs(dir) {
			abs = filepath.Join(cfg.FilePickerDir, dir)
		}
		abs = filepath.Clean(abs)
		info, err := os.Stat(abs)
		if err != nil {
			return fmt.Sprintf("[error] cd: %v", err), true
		}
		if !info.IsDir() {
			return "[error] cd: not a directory: " + dir, true
		}
		cfg.FilePickerDir = abs
		return "Changed directory to: " + cfg.FilePickerDir, true
	case "mkdir":
		if len(args) == 0 {
			return "[error] usage: mkdir [-p] <dir>", true
		}
		createParents := false
		var dirPath string
		for _, a := range args {
			if a == "-p" || a == "--parents" {
				createParents = true
			} else if dirPath == "" {
				dirPath = a
			}
		}
		if dirPath == "" {
			return "[error] usage: mkdir [-p] <dir>", true
		}
		abs := dirPath
		if !filepath.IsAbs(dirPath) {
			abs = filepath.Join(cfg.FilePickerDir, dirPath)
		}
		abs = filepath.Clean(abs)
		var mkdirFunc func(string, os.FileMode) error
		if createParents {
			mkdirFunc = os.MkdirAll
		} else {
			mkdirFunc = os.Mkdir
		}
		if err := mkdirFunc(abs, 0o755); err != nil {
			return fmt.Sprintf("[error] mkdir: %v", err), true
		}
		if createParents {
			return "Created " + dirPath + " (with parents)", true
		}
		return "Created " + dirPath, true
	case "ls":
		dir := "."
		for _, a := range args {
			if !strings.HasPrefix(a, "-") {
				dir = a
				break
			}
		}
		abs := dir
		if !filepath.IsAbs(dir) {
			abs = filepath.Join(cfg.FilePickerDir, dir)
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return fmt.Sprintf("[error] ls: %v", err), true
		}
		var out strings.Builder
		for _, e := range entries {
			info, _ := e.Info()
			switch {
			case e.IsDir():
				fmt.Fprintf(&out, "d  %-8s %s/\n", "-", e.Name())
			case info != nil:
				size := info.Size()
				sizeStr := strconv.FormatInt(size, 10)
				if size > 1024 {
					sizeStr = fmt.Sprintf("%.1fKB", float64(size)/1024)
				}
				fmt.Fprintf(&out, "f  %-8s %s\n", sizeStr, e.Name())
			default:
				fmt.Fprintf(&out, "f  %-8s %s\n", "?", e.Name())
			}
		}
		if out.Len() == 0 {
			return "(empty directory)", true
		}
		return strings.TrimRight(out.String(), "\n"), true
	case "go":
		// Allow all go subcommands
		if len(args) == 0 {
			return "[error] usage: go <subcommand> [options]", true
		}
		cmd := exec.Command("go", args...)
		cmd.Dir = cfg.FilePickerDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("[error] go %s: %v\n%s", args[0], err, string(output)), true
		}
		return string(output), true
	case "cp":
		if len(args) < 2 {
			return "[error] usage: cp <source> <dest>", true
		}
		src := args[0]
		dst := args[1]
		if !filepath.IsAbs(src) {
			src = filepath.Join(cfg.FilePickerDir, src)
		}
		if !filepath.IsAbs(dst) {
			dst = filepath.Join(cfg.FilePickerDir, dst)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Sprintf("[error] cp: %v", err), true
		}
		err = os.WriteFile(dst, data, 0644)
		if err != nil {
			return fmt.Sprintf("[error] cp: %v", err), true
		}
		return "Copied " + src + " to " + dst, true
	case "mv":
		if len(args) < 2 {
			return "[error] usage: mv <source> <dest>", true
		}
		src := args[0]
		dst := args[1]
		if !filepath.IsAbs(src) {
			src = filepath.Join(cfg.FilePickerDir, src)
		}
		if !filepath.IsAbs(dst) {
			dst = filepath.Join(cfg.FilePickerDir, dst)
		}
		err := os.Rename(src, dst)
		if err != nil {
			return fmt.Sprintf("[error] mv: %v", err), true
		}
		return "Moved " + src + " to " + dst, true
	case "rm":
		if len(args) == 0 {
			return "[error] usage: rm [-r] <file>", true
		}
		recursive := false
		var target string
		for _, a := range args {
			if a == "-r" || a == "-rf" || a == "-fr" || a == "-recursive" {
				recursive = true
			} else if target == "" {
				target = a
			}
		}
		if target == "" {
			return "[error] usage: rm [-r] <file>", true
		}
		abs := target
		if !filepath.IsAbs(target) {
			abs = filepath.Join(cfg.FilePickerDir, target)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return fmt.Sprintf("[error] rm: %v", err), true
		}
		if info.IsDir() {
			if recursive {
				err = os.RemoveAll(abs)
				if err != nil {
					return fmt.Sprintf("[error] rm: %v", err), true
				}
				return "Removed " + abs, true
			}
			return "[error] rm: is a directory (use -r)", true
		}
		err = os.Remove(abs)
		if err != nil {
			return fmt.Sprintf("[error] rm: %v", err), true
		}
		return "Removed " + abs, true
	}
	return "", false
}
