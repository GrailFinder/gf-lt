package tools

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Operator represents a chain operator between commands.
type Operator int

const (
	OpNone     Operator = iota
	OpAnd               // &&
	OpOr                // ||
	OpSeq               // ;
	OpPipe              // |
	OpRedirect          // >
	OpAppend            // >>
)

// Segment is a single command in a chain.
type Segment struct {
	Raw        string
	Op         Operator // operator AFTER this segment
	RedirectTo string   // file path for > or >>
	IsAppend   bool     // true for >>, false for >
}

// ParseChain splits a command string into segments by &&, ;, |, >, and >>.
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
		// >>
		if ch == '>' && i+1 < n && runes[i+1] == '>' {
			cmd := strings.TrimSpace(current.String())
			if cmd != "" {
				segments = append(segments, Segment{
					Raw: cmd,
					Op:  OpAppend,
				})
			}
			current.Reset()
			i++ // skip second >
			continue
		}
		// >
		if ch == '>' {
			cmd := strings.TrimSpace(current.String())
			if cmd != "" {
				segments = append(segments, Segment{
					Raw: cmd,
					Op:  OpRedirect,
				})
			}
			current.Reset()
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

	// Handle redirects: find the segment with OpRedirect or OpAppend
	// The NEXT segment (if any) is the target file
	var redirectTo string
	var isAppend bool
	redirectIdx := -1
	for i, seg := range segments {
		if seg.Op == OpRedirect || seg.Op == OpAppend {
			redirectIdx = i
			isAppend = seg.Op == OpAppend
			break
		}
	}

	if redirectIdx >= 0 && redirectIdx+1 < len(segments) {
		// The segment after redirect is the target path
		targetPath, err := resolveRedirectPath(segments[redirectIdx+1].Raw)
		if err != nil {
			return fmt.Sprintf("[error] redirect: %v", err)
		}
		redirectTo = targetPath
		// Get the redirect command BEFORE removing segments
		redirectCmd := segments[redirectIdx].Raw
		// Remove both the redirect segment and its target
		segments = append(segments[:redirectIdx], segments[redirectIdx+2:]...)

		// Execute the redirect command explicitly
		var lastOutput string
		var lastErr error
		lastOutput, lastErr = execSingle(redirectCmd, "")
		if lastErr != nil {
			return fmt.Sprintf("[error] redirect: %v", lastErr)
		}
		// Write output to file
		if err := writeFile(redirectTo, lastOutput, isAppend); err != nil {
			return fmt.Sprintf("[error] redirect: %v", err)
		}
		mode := "Wrote"
		if isAppend {
			mode = "Appended"
		}
		size := humanSizeChain(int64(len(lastOutput)))
		return fmt.Sprintf("%s %s → %s", mode, size, filepath.Base(redirectTo))
	} else if redirectIdx >= 0 && redirectIdx+1 >= len(segments) {
		// Redirect but no target file
		return "[error] redirect: target file required"
	}

	var collected []string
	var lastOutput string
	var lastErr error
	pipeInput := ""
	for i, seg := range segments {
		if i > 0 {
			prevOp := segments[i-1].Op
			if prevOp == OpAnd && lastErr != nil {
				continue
			}
			if prevOp == OpOr && lastErr == nil {
				continue
			}
		}
		segStdin := ""
		if i == 0 {
			segStdin = pipeInput
		} else if segments[i-1].Op == OpPipe {
			segStdin = lastOutput
		}
		lastOutput, lastErr = execSingle(seg.Raw, segStdin)
		if i < len(segments)-1 && seg.Op == OpPipe {
			continue
		}
		if lastOutput != "" {
			collected = append(collected, lastOutput)
		}
	}

	// Handle redirect if present
	if redirectTo != "" {
		output := lastOutput
		if err := writeFile(redirectTo, output, isAppend); err != nil {
			return fmt.Sprintf("[error] redirect: %v", err)
		}
		mode := "Wrote"
		if isAppend {
			mode = "Appended"
		}
		size := humanSizeChain(int64(len(output)))
		return fmt.Sprintf("%s %s → %s", mode, size, filepath.Base(redirectTo))
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
	result, err := execBuiltin(name, args, stdin)
	if err == nil {
		return result, nil
	}
	// Check if it's a "not a builtin" error (meaning we should try system command)
	if err.Error() == "not a builtin" {
		// Execute as system command
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
	// It's a builtin that returned an error
	return result, err
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
func execBuiltin(name string, args []string, stdin string) (string, error) {
	var result string
	switch name {
	case "echo":
		result = FsEcho(args, stdin)
	case "time":
		result = FsTime(args, stdin)
	case "cat":
		result = FsCat(args, stdin)
	case "pwd":
		result = FsPwd(args, stdin)
	case "cd":
		result = FsCd(args, stdin)
	case "mkdir":
		result = FsMkdir(args, stdin)
	case "ls":
		result = FsLs(args, stdin)
	case "cp":
		result = FsCp(args, stdin)
	case "mv":
		result = FsMv(args, stdin)
	case "rm":
		result = FsRm(args, stdin)
	case "grep":
		result = FsGrep(args, stdin)
	case "head":
		result = FsHead(args, stdin)
	case "tail":
		result = FsTail(args, stdin)
	case "wc":
		result = FsWc(args, stdin)
	case "sort":
		result = FsSort(args, stdin)
	case "uniq":
		result = FsUniq(args, stdin)
	case "sed":
		result = FsSed(args, stdin)
	case "stat":
		result = FsStat(args, stdin)
	case "go":
		if len(args) == 0 {
			return "[error] usage: go <subcommand> [options]", nil
		}
		cmd := exec.Command("go", args...)
		cmd.Dir = cfg.FilePickerDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("[error] go %s: %v\n%s", args[0], err, string(output)), nil
		}
		return string(output), nil
	default:
		return "", errors.New("not a builtin")
	}
	if strings.HasPrefix(result, "[error]") {
		return result, errors.New(result)
	}
	return result, nil
}

// resolveRedirectPath resolves the target path for a redirect operator
func resolveRedirectPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("redirect target required")
	}
	abs, err := resolvePath(path)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// writeFile writes content to a file (truncate or append)
func writeFile(path, content string, append bool) error {
	flags := os.O_CREATE | os.O_WRONLY
	if append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// humanSizeChain returns human-readable file size
func humanSizeChain(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
