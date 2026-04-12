package tools

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"gf-lt/models"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

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
	if cfg == nil {
		return
	}
	cfg.FilePickerDir = dir
}

func GetFSRoot() string {
	return cfg.FilePickerDir
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
	cfg.FilePickerDir = abs
	return nil
}

func resolvePath(rel string) (string, error) {
	if cfg.FilePickerDir == "" {
		return "", errors.New("fs root not set")
	}
	isAbs := filepath.IsAbs(rel)
	if isAbs {
		abs := filepath.Clean(rel)
		if !cfg.FSAllowOutOfRoot && !strings.HasPrefix(abs, cfg.FilePickerDir+string(os.PathSeparator)) && abs != cfg.FilePickerDir {
			return "", fmt.Errorf("path escapes fs root: %s", rel)
		}
		return abs, nil
	}
	abs := filepath.Join(cfg.FilePickerDir, rel)
	abs = filepath.Clean(abs)
	if !cfg.FSAllowOutOfRoot && !strings.HasPrefix(abs, cfg.FilePickerDir+string(os.PathSeparator)) && abs != cfg.FilePickerDir {
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
	showAll := false
	longFormat := false
	dir := ""
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") {
			flags := strings.TrimLeft(a, "-")
			for _, c := range flags {
				switch c {
				case 'a':
					showAll = true
				case 'l':
					longFormat = true
				}
			}
		} else if a != "" && dir == "" {
			dir = a
		}
	}

	hasGlob := strings.ContainsAny(dir, "*?[")

	if hasGlob {
		absDir := cfg.FilePickerDir
		if filepath.IsAbs(dir) {
			absDir = filepath.Dir(dir)
		} else if strings.Contains(dir, "/") {
			absDir = filepath.Join(cfg.FilePickerDir, filepath.Dir(dir))
		}
		globPattern := filepath.Base(dir)
		fullPattern := filepath.Join(absDir, globPattern)

		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			return fmt.Sprintf("[error] ls: %v", err)
		}
		if len(matches) == 0 {
			return "[error] ls: no such file or directory"
		}
		var out strings.Builder
		filter := func(name string) bool {
			return showAll || !strings.HasPrefix(name, ".")
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				continue
			}
			name := filepath.Base(match)
			if !filter(name) {
				continue
			}
			if longFormat {
				if info.IsDir() {
					fmt.Fprintf(&out, "d  %-8s %s/\n", "-", name)
				} else {
					fmt.Fprintf(&out, "f  %-8s %s\n", humanSize(info.Size()), name)
				}
			} else {
				if info.IsDir() {
					fmt.Fprintf(&out, "%s/\n", name)
				} else {
					fmt.Fprintf(&out, "%s\n", name)
				}
			}
		}
		if out.Len() == 0 {
			return "(empty directory)"
		}
		return strings.TrimRight(out.String(), "\n")
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
	filter := func(name string) bool {
		return showAll || !strings.HasPrefix(name, ".")
	}
	for _, e := range entries {
		name := e.Name()
		if !filter(name) {
			continue
		}
		info, _ := e.Info()
		if longFormat {
			if e.IsDir() {
				fmt.Fprintf(&out, "d  %-8s %s/\n", "-", name)
			} else if info != nil {
				fmt.Fprintf(&out, "f  %-8s %s\n", humanSize(info.Size()), name)
			} else {
				fmt.Fprintf(&out, "f  %-8s %s\n", "?", name)
			}
		} else {
			if e.IsDir() {
				fmt.Fprintf(&out, "%s/\n", name)
			} else {
				fmt.Fprintf(&out, "%s\n", name)
			}
		}
	}
	if out.Len() == 0 {
		return "(empty directory)"
	}
	return strings.TrimRight(out.String(), "\n")
}

func FsCat(args []string, stdin string) string {
	b64 := false
	var paths []string
	for _, a := range args {
		if a == "-b" || a == "--base64" {
			b64 = true
		} else if a != "" {
			paths = append(paths, a)
		}
	}
	if len(paths) == 0 {
		if stdin != "" {
			return stdin
		}
		return "[error] usage: cat <path> or cat (with stdin)"
	}

	var allFiles []string
	for _, path := range paths {
		if strings.ContainsAny(path, "*?[") {
			matches, err := filepath.Glob(path)
			if err != nil {
				return fmt.Sprintf("[error] cat: %v", err)
			}
			allFiles = append(allFiles, matches...)
		} else {
			allFiles = append(allFiles, path)
		}
	}

	if len(allFiles) == 0 {
		return "[error] cat: no files found"
	}

	var results []string
	for _, path := range allFiles {
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
			results = append(results, result)
		} else {
			results = append(results, string(data))
		}
	}
	return strings.Join(results, "")
}

func FsViewImg(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: view_img <image-path>"
	}
	path := args[0]
	var abs string
	if filepath.IsAbs(path) {
		abs = path
	} else {
		var err error
		abs, err = resolvePath(path)
		if err != nil {
			return fmt.Sprintf("[error] %v", err)
		}
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Sprintf("[error] view_img: %v", err)
	}
	if !IsImageFile(path) {
		return fmt.Sprintf("[error] not an image file: %s (use cat to read text files)", path)
	}
	dataURL, err := models.CreateImageURLFromPath(abs)
	if err != nil {
		return fmt.Sprintf("[error] view_img: %v", err)
	}
	result := models.MultimodalToolResp{
		Type: "multimodal_content",
		Parts: []map[string]string{
			{"type": "text", "text": "Image: " + path},
			{"type": "image_url", "url": dataURL},
		},
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("[error] view_img: %v", err)
	}
	return string(jsonResult)
}

func FsWrite(args []string, stdin string) string {
	b64 := false
	var path string
	var contentParts []string
	for _, a := range args {
		switch a {
		case "-b", "--base64":
			b64 = true
		default:
			if path == "" {
				path = a
			} else {
				contentParts = append(contentParts, a)
			}
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
	force := false
	var paths []string
	for _, a := range args {
		if a == "-f" || a == "--force" {
			force = true
		} else if !strings.HasPrefix(a, "-") {
			paths = append(paths, a)
		}
	}
	if len(paths) == 0 {
		return "[error] usage: rm <path>"
	}

	var removed []string
	var errs []string
	for _, path := range paths {
		if strings.ContainsAny(path, "*?[") {
			matches, err := filepath.Glob(path)
			if err != nil {
				if !force {
					return fmt.Sprintf("[error] rm: %v", err)
				}
				continue
			}
			for _, m := range matches {
				if err := os.RemoveAll(m); err != nil {
					if !force {
						errs = append(errs, fmt.Sprintf("%v", err))
					}
					continue
				}
				removed = append(removed, m)
			}
		} else {
			abs, err := resolvePath(path)
			if err != nil {
				if !force {
					return fmt.Sprintf("[error] %v", err)
				}
				continue
			}
			if err := os.RemoveAll(abs); err != nil {
				if !force {
					return fmt.Sprintf("[error] rm: %v", err)
				}
				continue
			}
			removed = append(removed, path)
		}
	}
	if len(removed) == 0 && len(errs) > 0 {
		return "[error] rm: " + strings.Join(errs, "; ")
	}
	return "Removed " + strings.Join(removed, ", ")
}

func FsCp(args []string, stdin string) string {
	if len(args) < 2 {
		return "[error] usage: cp <src> <dst>"
	}
	srcPattern := args[0]
	dstPath := args[1]

	// Check if dst is an existing directory (ends with / or is a directory)
	dstIsDir := strings.HasSuffix(dstPath, "/")
	if !dstIsDir {
		if info, err := os.Stat(dstPath); err == nil && info.IsDir() {
			dstIsDir = true
		}
	}

	// Check for single file copy (no glob and dst doesn't end with / and is not an existing dir)
	hasGlob := strings.ContainsAny(srcPattern, "*?[")

	// Single source file to a specific file path (not a glob, not a directory)
	if !hasGlob && !dstIsDir {
		// Check if destination is an existing file - if not, treat as single file copy
		if info, err := os.Stat(dstPath); err != nil || !info.IsDir() {
			srcAbs, err := resolvePath(srcPattern)
			if err != nil {
				return fmt.Sprintf("[error] %v", err)
			}
			data, err := os.ReadFile(srcAbs)
			if err != nil {
				return fmt.Sprintf("[error] cp read: %v", err)
			}
			if err := os.WriteFile(dstPath, data, 0o644); err != nil {
				return fmt.Sprintf("[error] cp write: %v", err)
			}
			return fmt.Sprintf("Copied %s → %s (%s)", srcPattern, dstPath, humanSize(int64(len(data))))
		}
	}

	// Copy to directory (either glob, or explicit directory)
	var srcFiles []string
	if hasGlob {
		matches, err := filepath.Glob(srcPattern)
		if err != nil {
			return fmt.Sprintf("[error] cp: %v", err)
		}
		if len(matches) == 0 {
			return "[error] cp: no files match pattern"
		}
		srcFiles = matches
	} else {
		srcFiles = []string{srcPattern}
	}

	var results []string
	for _, srcPath := range srcFiles {
		srcAbs, err := resolvePath(srcPath)
		if err != nil {
			return fmt.Sprintf("[error] %v", err)
		}
		data, err := os.ReadFile(srcAbs)
		if err != nil {
			return fmt.Sprintf("[error] cp read: %v", err)
		}

		dstAbs, err := resolvePath(filepath.Join(dstPath, filepath.Base(srcPath)))
		if err != nil {
			return fmt.Sprintf("[error] %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
			return fmt.Sprintf("[error] cp mkdir: %v", err)
		}
		if err := os.WriteFile(dstAbs, data, 0o644); err != nil {
			return fmt.Sprintf("[error] cp write: %v", err)
		}
		results = append(results, fmt.Sprintf("%s → %s (%s)", srcPath, filepath.Join(dstPath, filepath.Base(srcPath)), humanSize(int64(len(data)))))
	}
	return strings.Join(results, ", ")
}

func FsMv(args []string, stdin string) string {
	if len(args) < 2 {
		return "[error] usage: mv <src> <dst>"
	}
	srcPattern := args[0]
	dstPath := args[1]

	// Check if dst is an existing directory (ends with / or is a directory)
	dstIsDir := strings.HasSuffix(dstPath, "/")
	if !dstIsDir {
		if info, err := os.Stat(dstPath); err == nil && info.IsDir() {
			dstIsDir = true
		}
	}

	// Check for single file move (no glob and dst doesn't end with / and is not an existing dir)
	hasGlob := strings.ContainsAny(srcPattern, "*?[")

	// Single source file to a specific file path (not a glob, not a directory)
	if !hasGlob && !dstIsDir {
		// Check if destination is an existing file - if not, treat as single file move
		if info, err := os.Stat(dstPath); err != nil || !info.IsDir() {
			srcAbs, err := resolvePath(srcPattern)
			if err != nil {
				return fmt.Sprintf("[error] %v", err)
			}
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
				return fmt.Sprintf("[error] mv mkdir: %v", err)
			}
			if err := os.Rename(srcAbs, dstPath); err != nil {
				return fmt.Sprintf("[error] mv: %v", err)
			}
			return fmt.Sprintf("Moved %s → %s", srcPattern, dstPath)
		}
	}

	// Move to directory (either glob, or explicit directory)
	var srcFiles []string
	if hasGlob {
		matches, err := filepath.Glob(srcPattern)
		if err != nil {
			return fmt.Sprintf("[error] mv: %v", err)
		}
		if len(matches) == 0 {
			return "[error] mv: no files match pattern"
		}
		srcFiles = matches
	} else {
		srcFiles = []string{srcPattern}
	}

	var results []string
	for _, srcPath := range srcFiles {
		srcAbs, err := resolvePath(srcPath)
		if err != nil {
			return fmt.Sprintf("[error] %v", err)
		}

		dstAbs, err := resolvePath(filepath.Join(dstPath, filepath.Base(srcPath)))
		if err != nil {
			return fmt.Sprintf("[error] %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
			return fmt.Sprintf("[error] mv mkdir: %v", err)
		}
		if err := os.Rename(srcAbs, dstAbs); err != nil {
			return fmt.Sprintf("[error] mv: %v", err)
		}
		results = append(results, fmt.Sprintf("%s → %s", srcPath, filepath.Join(dstPath, filepath.Base(srcPath))))
	}
	return strings.Join(results, ", ")
}

func FsMkdir(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: mkdir [-p] <dir>"
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
		return "[error] usage: mkdir [-p] <dir>"
	}
	abs, err := resolvePath(dirPath)
	if err != nil {
		return fmt.Sprintf("[error] %v", err)
	}
	var mkdirFunc func(string, os.FileMode) error
	if createParents {
		mkdirFunc = os.MkdirAll
	} else {
		mkdirFunc = os.Mkdir
	}
	if err := mkdirFunc(abs, 0o755); err != nil {
		return fmt.Sprintf("[error] mkdir: %v", err)
	}
	if createParents {
		return "Created " + dirPath + " (with parents)"
	}
	return "Created " + dirPath
}

// Text processing commands

func FsEcho(args []string, stdin string) string {
	if stdin != "" {
		return stdin
	}
	result := strings.Join(args, " ")
	if result != "" {
		result += "\n"
	}
	return result
}

func FsTime(args []string, stdin string) string {
	return time.Now().Format("2006-01-02 15:04:05 MST")
}

func FsGrep(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: grep [-i] [-v] [-c] [-E] <pattern> [file]"
	}
	ignoreCase := false
	invert := false
	countOnly := false
	useRegex := false
	var pattern string
	var filePath string
	for _, a := range args {
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && len(a) > 1 {
			flags := strings.TrimLeft(a, "-")
			for _, c := range flags {
				switch c {
				case 'i':
					ignoreCase = true
				case 'v':
					invert = true
				case 'c':
					countOnly = true
				case 'E':
					useRegex = true
				}
			}
			continue
		}
		switch a {
		case "-i":
			ignoreCase = true
		case "-v":
			invert = true
		case "-c":
			countOnly = true
		case "-E":
			useRegex = true
		default:
			if pattern == "" {
				pattern = a
			} else if filePath == "" {
				filePath = a
			}
		}
	}
	if pattern == "" {
		return "[error] pattern required"
	}
	var lines []string
	if filePath != "" {
		abs, err := resolvePath(filePath)
		if err != nil {
			return fmt.Sprintf("[error] grep: %v", err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("[error] grep: %v", err)
		}
		lines = strings.Split(string(data), "\n")
	} else if stdin != "" {
		lines = strings.Split(stdin, "\n")
	} else {
		return "[error] grep: no input (use file path or pipe from stdin)"
	}

	var matched []string
	for _, line := range lines {
		var match bool
		if useRegex {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Sprintf("[error] grep: invalid regex: %v", err)
			}
			match = re.MatchString(line)
			if ignoreCase && !match {
				reIC, err := regexp.Compile("(?i)" + pattern)
				if err == nil {
					match = reIC.MatchString(line)
				}
			}
		} else {
			haystack := line
			if ignoreCase {
				haystack = strings.ToLower(line)
				patternLower := strings.ToLower(pattern)
				match = strings.Contains(haystack, patternLower)
			} else {
				match = strings.Contains(haystack, pattern)
			}
		}
		if invert {
			match = !match
		}
		if match {
			matched = append(matched, line)
		}
	}
	if countOnly {
		return strconv.Itoa(len(matched))
	}
	return strings.Join(matched, "\n")
}

func FsHead(args []string, stdin string) string {
	n := 10
	var filePath string
	for i, a := range args {
		if a == "-n" && i+1 < len(args) {
			if parsed, err := strconv.Atoi(args[i+1]); err == nil {
				n = parsed
			}
		} else if strings.HasPrefix(a, "-") {
			continue
		} else if parsed, err := strconv.Atoi(a); err == nil {
			n = parsed
		} else if filePath == "" && !strings.HasPrefix(a, "-") {
			filePath = a
		}
	}
	var lines []string
	if filePath != "" {
		abs, err := resolvePath(filePath)
		if err != nil {
			return fmt.Sprintf("[error] head: %v", err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("[error] head: %v", err)
		}
		lines = strings.Split(string(data), "\n")
	} else if stdin != "" {
		lines = strings.Split(stdin, "\n")
	} else {
		return "[error] head: no input (use file path or pipe from stdin)"
	}
	if n > 0 && len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

func FsTail(args []string, stdin string) string {
	n := 10
	var filePath string
	for i, a := range args {
		if a == "-n" && i+1 < len(args) {
			if parsed, err := strconv.Atoi(args[i+1]); err == nil {
				n = parsed
			}
		} else if strings.HasPrefix(a, "-") {
			continue
		} else if parsed, err := strconv.Atoi(a); err == nil {
			n = parsed
		} else if filePath == "" && !strings.HasPrefix(a, "-") {
			filePath = a
		}
	}
	var lines []string
	if filePath != "" {
		abs, err := resolvePath(filePath)
		if err != nil {
			return fmt.Sprintf("[error] tail: %v", err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("[error] tail: %v", err)
		}
		lines = strings.Split(string(data), "\n")
	} else if stdin != "" {
		lines = strings.Split(stdin, "\n")
	} else {
		return "[error] tail: no input (use file path or pipe from stdin)"
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func FsWc(args []string, stdin string) string {
	var content string
	var filePath string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if filePath == "" {
			filePath = a
		}
	}
	if filePath != "" {
		abs, err := resolvePath(filePath)
		if err != nil {
			return fmt.Sprintf("[error] wc: %v", err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("[error] wc: %v", err)
		}
		content = string(data)
	} else if stdin != "" {
		content = stdin
	} else {
		return "[error] wc: no input (use file path or pipe from stdin)"
	}
	content = strings.TrimRight(content, "\n")
	lines := len(strings.Split(content, "\n"))
	words := len(strings.Fields(content))
	chars := len(content)
	if len(args) > 0 {
		switch args[0] {
		case "-l":
			return strconv.Itoa(lines)
		case "-w":
			return strconv.Itoa(words)
		case "-c":
			return strconv.Itoa(chars)
		}
	}
	return fmt.Sprintf("%d lines, %d words, %d chars", lines, words, chars)
}

func FsSort(args []string, stdin string) string {
	reverse := false
	numeric := false
	var filePath string
	for _, a := range args {
		switch a {
		case "-r":
			reverse = true
		case "-n":
			numeric = true
		default:
			if filePath == "" && !strings.HasPrefix(a, "-") {
				filePath = a
			}
		}
	}
	var lines []string
	if filePath != "" {
		abs, err := resolvePath(filePath)
		if err != nil {
			return fmt.Sprintf("[error] sort: %v", err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("[error] sort: %v", err)
		}
		lines = strings.Split(string(data), "\n")
	} else if stdin != "" {
		lines = strings.Split(stdin, "\n")
	} else {
		return "[error] sort: no input (use file path or pipe from stdin)"
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
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
	showCount := false
	var filePath string
	for _, a := range args {
		if a == "-c" {
			showCount = true
		} else if filePath == "" && !strings.HasPrefix(a, "-") {
			filePath = a
		}
	}
	var lines []string
	if filePath != "" {
		abs, err := resolvePath(filePath)
		if err != nil {
			return fmt.Sprintf("[error] uniq: %v", err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("[error] uniq: %v", err)
		}
		lines = strings.Split(string(data), "\n")
	} else if stdin != "" {
		lines = strings.Split(stdin, "\n")
	} else {
		return "[error] uniq: no input (use file path or pipe from stdin)"
	}
	var result []string
	seen := make(map[string]bool)
	countMap := make(map[string]int)
	for _, line := range lines {
		countMap[line]++
		if !seen[line] {
			seen[line] = true
			result = append(result, line)
		}
	}
	if showCount {
		var counted []string
		for _, line := range result {
			counted = append(counted, fmt.Sprintf("%d %s", countMap[line], line))
		}
		return strings.Join(counted, "\n")
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
	return cfg.FilePickerDir
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
		return "[error] cd: not a directory: " + dir
	}
	cfg.FilePickerDir = abs
	return "Changed directory to: " + cfg.FilePickerDir
}

func FsSed(args []string, stdin string) string {
	if len(args) == 0 {
		return "[error] usage: sed 's/old/new/[g]' [file]"
	}
	inPlace := false
	var filePath string
	var pattern string
	for _, a := range args {
		switch a {
		case "-i", "--in-place":
			inPlace = true
		default:
			if strings.HasPrefix(a, "s") && len(a) > 1 {
				pattern = a
			} else if filePath == "" && !strings.HasPrefix(a, "-") {
				filePath = a
			}
		}
	}
	if pattern == "" {
		return "[error] usage: sed 's/old/new/[g]' [file]"
	}
	// Parse pattern: s/old/new/flags
	parts := strings.Split(pattern[2:], "/")
	if len(parts) < 2 {
		return "[error] invalid sed pattern. Use: s/old/new/[g]"
	}
	oldStr := parts[0]
	newStr := parts[1]
	global := len(parts) >= 3 && strings.Contains(parts[2], "g")
	var content string
	switch {
	case filePath != "" && stdin == "":
		abs, err := resolvePath(filePath)
		if err != nil {
			return fmt.Sprintf("[error] sed: %v", err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Sprintf("[error] sed: %v", err)
		}
		content = string(data)
	case stdin != "":
		content = stdin
	default:
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
		return "Modified " + filePath
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
		return "Stored under topic: " + topic
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
		return "Deleted topic: " + topic
	default:
		return fmt.Sprintf("[error] unknown subcommand: %s. Use: store, get, list, topics, forget, delete", args[0])
	}
}
