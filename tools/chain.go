package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		return "", fmt.Errorf("empty command")
	}

	name := parts[0]
	args := parts[1:]

	// Check if it's a built-in Go command
	if result := execBuiltin(name, args, stdin); result != "" {
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
func execBuiltin(name string, args []string, stdin string) string {
	switch name {
	case "echo":
		if stdin != "" {
			return stdin
		}
		return strings.Join(args, " ")
	case "time":
		return "2006-01-02 15:04:05 MST"
	case "cat":
		if len(args) == 0 {
			if stdin != "" {
				return stdin
			}
			return ""
		}
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Sprintf("[error] cat: %v", err)
		}
		return string(data)
	case "pwd":
		return fsRootDir
	case "cd":
		if len(args) == 0 {
			return "[error] usage: cd <dir>"
		}
		dir := args[0]
		// Resolve relative to fsRootDir
		abs := dir
		if !filepath.IsAbs(dir) {
			abs = filepath.Join(fsRootDir, dir)
		}
		abs = filepath.Clean(abs)
		info, err := os.Stat(abs)
		if err != nil {
			return fmt.Sprintf("[error] cd: %v", err)
		}
		if !info.IsDir() {
			return fmt.Sprintf("[error] cd: not a directory: %s", dir)
		}
		fsRootDir = abs
		return fmt.Sprintf("Changed directory to: %s", fsRootDir)
	}
	return ""
}
