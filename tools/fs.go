package tools

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var fsRootDir string
var memoryStore MemoryStore
var agentRole string

type MemoryStore interface {
	Memorise(agent, topic, data string) (string, error)
	Recall(agent, topic string) (string, error)
	RecallTopics(agent string) ([]string, error)
	Forget(agent, topic string) error
}

func SetMemoryStore(store MemoryStore, role string) {
	memoryStore = store
	agentRole = role
}

func SetFSRoot(dir string) {
	fsRootDir = dir
}

func GetFSRoot() string {
	return fsRootDir
}

func SetFSCwd(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}
	fsRootDir = abs
	return nil
}

func resolvePath(rel string) (string, error) {
	if fsRootDir == "" {
		return "", fmt.Errorf("fs root not set")
	}

	if filepath.IsAbs(rel) {
		abs := filepath.Clean(rel)
		if !strings.HasPrefix(abs, fsRootDir+string(os.PathSeparator)) && abs != fsRootDir {
			return "", fmt.Errorf("path escapes fs root: %s", rel)
		}
		return abs, nil
	}

	abs := filepath.Join(fsRootDir, rel)
	abs = filepath.Clean(abs)
	if !strings.HasPrefix(abs, fsRootDir+string(os.PathSeparator)) && abs != fsRootDir {
		return "", fmt.Errorf("path escapes fs root: %s", rel)
	}
	return abs, nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func IsImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" || ext == ".svg"
}

func FsLs(args []string, stdin string) string {
	dir := ""
	if len(args) > 0 {
		dir = args[0]
	}
	abs, err := resolvePath(dir)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Sprintf("[error] ls: %v", err)
	}

	var out strings.Builder
	for _, e := range entries {
		info, _ := e.Info()
		if e.IsDir() {
			fmt.Fprintf(&out, "d  %-8s %s/\n", "-", e.Name())
		} else if info != nil {
			fmt.Fprintf(&out, "f  %-8s %s\n", humanSize(info.Size()), e.Name())
		} else {
			fmt.Fprintf(&out, "f  %-8s %s\n", "?", e.Name())
		}
	}
	if out.Len() == 0 {
		return "(empty directory)"
	}
	return strings.TrimRight(out.String(), "\n")
}

func FsCat(args []string, stdin string) string {
	b64 := false
	var path string
	for _, a := range args {
		if a == "-b" || a == "--base64" {
			b64 = true
		} else if path == "" {
			path = a
		}
	}
	if path == "" {
		return "[error] usage: cat <path>"
	}

	abs, err := resolvePath(path)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Sprintf("[error] cat: %v", err)
	}

	if b64 {
		result := base64.StdEncoding.EncodeToString(data)
		if IsImageFile(path) {
			result += fmt.Sprintf("\n![image](file://%s)", abs)
		}
		return result
	}
	return string(data)
}

func FsSee(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: see <image-path>"
	}
	path := args[0]

	abs, err := resolvePath(path)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Sprintf("[error] see: %v", err)
	}

	if !IsImageFile(path) {
		return fmt.Sprintf("[error] not an image file: %s (use cat to read text files)", path)
	}

	return fmt.Sprintf("Image: %s (%s)\n![image](file://%s)", path, humanSize(info.Size()), abs)
}

func FsWrite(args []string, stdin string) string {
	b64 := false
	var path string
	var contentParts []string
	for _, a := range args {
		if a == "-b" || a == "--base64" {
			b64 = true
		} else if path == "" {
			path = a
		} else {
			contentParts = append(contentParts, a)
		}
	}
	if path == "" {
		return "[error] usage: write <path> [content] or pipe stdin"
	}

	abs, err := resolvePath(path)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Sprintf("[error] mkdir: %v", err)
	}

	var data []byte
	if b64 {
		src := stdin
		if src == "" && len(contentParts) > 0 {
			src = strings.Join(contentParts, " ")
		}
		src = strings.TrimSpace(src)
		var err error
		data, err = base64.StdEncoding.DecodeString(src)
		if err != nil {
			return fmt.Sprintf("[error] base64 decode: %v", err)
		}
	} else {
		if len(contentParts) > 0 {
			data = []byte(strings.Join(contentParts, " "))
		} else {
			data = []byte(stdin)
		}
	}

	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return fmt.Sprintf("[error] write: %v", err)
	}

	size := humanSize(int64(len(data)))
	result := fmt.Sprintf("Written %s → %s", size, path)

	if IsImageFile(path) {
		result += fmt.Sprintf("\n![image](file://%s)", abs)
	}

	return result
}

func FsStat(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: stat <path>"
	}

	abs, err := resolvePath(args[0])
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Sprintf("[error] stat: %v", err)
	}

	mime := "application/octet-stream"
	if IsImageFile(args[0]) {
		ext := strings.ToLower(filepath.Ext(args[0]))
		switch ext {
		case ".png":
			mime = "image/png"
		case ".jpg", ".jpeg":
			mime = "image/jpeg"
		case ".gif":
			mime = "image/gif"
		case ".webp":
			mime = "image/webp"
		case ".svg":
			mime = "image/svg+xml"
		}
	}

	var out strings.Builder
	fmt.Fprintf(&out, "File: %s\n", args[0])
	fmt.Fprintf(&out, "Size: %s (%d bytes)\n", humanSize(info.Size()), info.Size())
	fmt.Fprintf(&out, "Type: %s\n", mime)
	fmt.Fprintf(&out, "Modified: %s\n", info.ModTime().Format(time.RFC3339))
	if info.IsDir() {
		fmt.Fprintf(&out, "Kind: directory\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func FsRm(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: rm <path>"
	}

	abs, err := resolvePath(args[0])
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	if err := os.RemoveAll(abs); err != nil {
		return fmt.Sprintf("[error] rm: %v", err)
	}
	return fmt.Sprintf("Removed %s", args[0])
}

func FsCp(args []string, stdin string) string {
	if len(args) < 2 {
		return "[error] usage: cp <src> <dst>"
	}

	srcAbs, err := resolvePath(args[0])
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}
	dstAbs, err := resolvePath(args[1])
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	data, err := os.ReadFile(srcAbs)
	if err != nil {
		return fmt.Sprintf("[error] cp read: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return fmt.Sprintf("[error] cp mkdir: %v", err)
	}

	if err := os.WriteFile(dstAbs, data, 0o644); err != nil {
		return fmt.Sprintf("[error] cp write: %v", err)
	}
	return fmt.Sprintf("Copied %s → %s (%s)", args[0], args[1], humanSize(int64(len(data))))
}

func FsMv(args []string, stdin string) string {
	if len(args) < 2 {
		return "[error] usage: mv <src> <dst>"
	}

	srcAbs, err := resolvePath(args[0])
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}
	dstAbs, err := resolvePath(args[1])
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return fmt.Sprintf("[error] mv mkdir: %v", err)
	}

	if err := os.Rename(srcAbs, dstAbs); err != nil {
		return fmt.Sprintf("[error] mv: %v", err)
	}
	return fmt.Sprintf("Moved %s → %s", args[0], args[1])
}

func FsMkdir(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: mkdir <dir>"
	}

	abs, err := resolvePath(args[0])
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}

	if err := os.MkdirAll(abs, 0o755); err != nil {
		return fmt.Sprintf("[error] mkdir: %v", err)
	}
	return fmt.Sprintf("Created %s", args[0])
}

// Text processing commands

func FsEcho(args []string, stdin string) string {
	if stdin != "" {
		return stdin
	}
	return strings.Join(args, " ")
}

func FsTime(args []string, stdin string) string {
	return time.Now().Format("2006-01-02 15:04:05 MST")
}

func FsGrep(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: grep [-i] [-v] [-c] <pattern>"
	}
	ignoreCase := false
	invert := false
	countOnly := false
	var pattern string
	for _, a := range args {
		switch a {
		case "-i":
			ignoreCase = true
		case "-v":
			invert = true
		case "-c":
			countOnly = true
		default:
			pattern = a
		}
	}
	if pattern == "" {
		return "[error] pattern required"
	}
	if ignoreCase {
		pattern = strings.ToLower(pattern)
	}

	lines := strings.Split(stdin, "\n")
	var matched []string
	for _, line := range lines {
		haystack := line
		if ignoreCase {
			haystack = strings.ToLower(line)
		}
		match := strings.Contains(haystack, pattern)
		if invert {
			match = !match
		}
		if match {
			matched = append(matched, line)
		}
	}
	if countOnly {
		return fmt.Sprintf("%d", len(matched))
	}
	return strings.Join(matched, "\n")
}

func FsHead(args []string, stdin string) string {
	n := 10
	for i, a := range args {
		if a == "-n" && i+1 < len(args) {
			if parsed, err := strconv.Atoi(args[i+1]); err == nil {
				n = parsed
			}
		} else if strings.HasPrefix(a, "-") {
			continue
		} else if parsed, err := strconv.Atoi(a); err == nil {
			n = parsed
		}
	}
	lines := strings.Split(stdin, "\n")
	if n > 0 && len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func FsTail(args []string, stdin string) string {
	n := 10
	for i, a := range args {
		if a == "-n" && i+1 < len(args) {
			if parsed, err := strconv.Atoi(args[i+1]); err == nil {
				n = parsed
			}
		} else if strings.HasPrefix(a, "-") {
			continue
		} else if parsed, err := strconv.Atoi(a); err == nil {
			n = parsed
		}
	}
	lines := strings.Split(stdin, "\n")
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func FsWc(args []string, stdin string) string {
	lines := len(strings.Split(stdin, "\n"))
	words := len(strings.Fields(stdin))
	chars := len(stdin)
	if len(args) > 0 {
		switch args[0] {
		case "-l":
			return fmt.Sprintf("%d", lines)
		case "-w":
			return fmt.Sprintf("%d", words)
		case "-c":
			return fmt.Sprintf("%d", chars)
		}
	}
	return fmt.Sprintf("%d lines, %d words, %d chars", lines, words, chars)
}

func FsSort(args []string, stdin string) string {
	lines := strings.Split(stdin, "\n")
	reverse := false
	numeric := false
	for _, a := range args {
		if a == "-r" {
			reverse = true
		} else if a == "-n" {
			numeric = true
		}
	}

	sortFunc := func(i, j int) bool {
		if numeric {
			ni, _ := strconv.Atoi(lines[i])
			nj, _ := strconv.Atoi(lines[j])
			if reverse {
				return ni > nj
			}
			return ni < nj
		}
		if reverse {
			return lines[i] > lines[j]
		}
		return lines[i] < lines[j]
	}

	sort.Slice(lines, sortFunc)
	return strings.Join(lines, "\n")
}

func FsUniq(args []string, stdin string) string {
	lines := strings.Split(stdin, "\n")
	showCount := false
	for _, a := range args {
		if a == "-c" {
			showCount = true
		}
	}

	var result []string
	var prev string
	first := true
	count := 0
	for _, line := range lines {
		if first || line != prev {
			if !first && showCount {
				result = append(result, fmt.Sprintf("%d %s", count, prev))
			} else if !first {
				result = append(result, prev)
			}
			count = 1
			prev = line
			first = false
		} else {
			count++
		}
	}
	if !first {
		if showCount {
			result = append(result, fmt.Sprintf("%d %s", count, prev))
		} else {
			result = append(result, prev)
		}
	}
	return strings.Join(result, "\n")
}

var allowedGitSubcommands = map[string]bool{
	"status":    true,
	"log":       true,
	"diff":      true,
	"show":      true,
	"branch":    true,
	"reflog":    true,
	"rev-parse": true,
	"shortlog":  true,
	"describe":  true,
	"rev-list":  true,
}

func FsGit(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: git <subcommand> [options]"
	}

	subcmd := args[0]
	if !allowedGitSubcommands[subcmd] {
		return fmt.Sprintf("[error] git: '%s' is not an allowed git command. Allowed: status, log, diff, show, branch, reflog, rev-parse, shortlog, describe, rev-list", subcmd)
	}

	abs, err := resolvePath(".")
	if err != nil {
		return fmt.Sprintf("[error] git: %v", err)
	}

	// Pass all args to git (first arg is subcommand, rest are options)
	cmd := exec.Command("git", args...)
	cmd.Dir = abs
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("[error] git %s: %v\n%s", subcmd, err, string(output))
	}
	return string(output)
}

func FsPwd(args []string, stdin string) string {
	return fsRootDir
}

func FsCd(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: cd <dir>"
	}
	dir := args[0]
	abs, err := resolvePath(dir)
	if err != nil {
		return fmt.Sprintf("[error] cd: %v", err)
	}
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

func FsSed(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: sed 's/old/new/[g]' [file]"
	}

	inPlace := false
	var filePath string
	var pattern string

	for _, a := range args {
		if a == "-i" || a == "--in-place" {
			inPlace = true
		} else if strings.HasPrefix(a, "s") && len(a) > 1 {
			// This looks like a sed pattern
			pattern = a
		} else if filePath == "" && !strings.HasPrefix(a, "-") {
			filePath = a
		}
	}

	if pattern == "" {
		return "[error] usage: sed 's/old/new/[g]' [file]"
	}

	// Parse pattern: s/old/new/flags
	parts := strings.Split(pattern[1:], "/")
	if len(parts) < 2 {
		return "[error] invalid sed pattern. Use: s/old/new/[g]"
	}

	oldStr := parts[0]
	newStr := parts[1]
	global := len(parts) >= 3 && strings.Contains(parts[2], "g")

	var content string
	if filePath != "" && stdin == "" {
		// Read from file
		abs, err := resolvePath(filePath)
		if err != nil {
			return fmt.Sprintf("[error] sed: %v", err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("[error] sed: %v", err)
		}
		content = string(data)
	} else if stdin != "" {
		// Use stdin
		content = stdin
	} else {
		return "[error] sed: no input (use file path or pipe from stdin)"
	}

	// Apply sed replacement
	if global {
		content = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		content = strings.Replace(content, oldStr, newStr, 1)
	}

	if inPlace && filePath != "" {
		abs, err := resolvePath(filePath)
		if err != nil {
			return fmt.Sprintf("[error] sed: %v", err)
		}
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			return fmt.Sprintf("[error] sed: %v", err)
		}
		return fmt.Sprintf("Modified %s", filePath)
	}

	return content
}

func FsMemory(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: memory store <topic> <data> | memory get <topic> | memory list | memory forget <topic>"
	}

	if memoryStore == nil {
		return "[error] memory store not initialized"
	}

	switch args[0] {
	case "store":
		if len(args) < 3 && stdin == "" {
			return "[error] usage: memory store <topic> <data>"
		}
		topic := args[1]
		var data string
		if len(args) >= 3 {
			data = strings.Join(args[2:], " ")
		} else {
			data = stdin
		}
		_, err := memoryStore.Memorise(agentRole, topic, data)
		if err != nil {
			return fmt.Sprintf("[error] failed to store: %v", err)
		}
		return fmt.Sprintf("Stored under topic: %s", topic)

	case "get":
		if len(args) < 2 {
			return "[error] usage: memory get <topic>"
		}
		topic := args[1]
		data, err := memoryStore.Recall(agentRole, topic)
		if err != nil {
			return fmt.Sprintf("[error] failed to recall: %v", err)
		}
		return fmt.Sprintf("Topic: %s\n%s", topic, data)

	case "list", "topics":
		topics, err := memoryStore.RecallTopics(agentRole)
		if err != nil {
			return fmt.Sprintf("[error] failed to list topics: %v", err)
		}
		if len(topics) == 0 {
			return "No topics stored."
		}
		return "Topics: " + strings.Join(topics, ", ")

	case "forget", "delete":
		if len(args) < 2 {
			return "[error] usage: memory forget <topic>"
		}
		topic := args[1]
		err := memoryStore.Forget(agentRole, topic)
		if err != nil {
			return fmt.Sprintf("[error] failed to forget: %v", err)
		}
		return fmt.Sprintf("Deleted topic: %s", topic)

	default:
		return fmt.Sprintf("[error] unknown subcommand: %s. Use: store, get, list, topics, forget, delete", args[0])
	}
}
