package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"gf-lt/models"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func IsImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" || ext == ".svg"
}

func FsViewImg(args []string, stdin string) (string, error) {
	if len(args) == 0 {
		return "", models.InvalidArgs("usage: view_img <image-path>", "")
	}
	path := args[0]
	var abs string
	if filepath.IsAbs(path) {
		abs = path
	} else {
		var err error
		abs, err = resolvePath(path)
		if err != nil {
			return "", models.Internal("could not resolve path", err)
		}
	}
	if _, err := os.Stat(abs); err != nil {
		return "", readError(path, err)
	}
	if !IsImageFile(path) {
		return "", models.InvalidArgs(fmt.Sprintf("not an image file: %s", path), "use the read tool to read text files")
	}
	dataURL, err := models.CreateImageURLFromPath(abs)
	if err != nil {
		return "", models.Internal("view_img failed", err)
	}
	result := models.MultimodalToolResp{
		Type: "multimodal_content",
		Parts: []map[string]string{
			{"type": "text", "text": "Image: " + path},
			{"type": "image_url", "url": dataURL, "path": abs},
		},
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		return "", models.Internal("view_img failed", err)
	}
	return string(jsonResult), nil
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

var missionGitSubcommands = map[string]bool{
	"add":         true,
	"commit":      true,
	"checkout":    true,
	"push":        true,
	"reset":       true,
	"stash":       true,
	"restore":     true,
	"switch":      true,
	"merge":       true,
	"rebase":      true,
	"cherry-pick": true,
	"tag":         true,
	"remote":      true,
	"fetch":       true,
	"pull":        true,
	"status":      true,
	"log":         true,
	"diff":        true,
	"show":        true,
	"branch":      true,
	"reflog":      true,
	"rev-parse":   true,
	"shortlog":    true,
	"describe":    true,
	"rev-list":    true,
}

func FsGit(args []string, stdin string) (string, error) {
	if len(args) == 0 {
		return "", models.InvalidArgs("usage: git <subcommand> [options]", "")
	}
	subcmd := args[0]
	allowed := allowedGitSubcommands[subcmd]
	if !allowed && currentMission != nil {
		allowed = missionGitSubcommands[subcmd]
	}
	if !allowed {
		return "", models.Denied(fmt.Sprintf("git '%s' is not an allowed git command", subcmd))
	}
	abs, err := resolvePath(".")
	if err != nil {
		return "", models.Internal("git failed", err)
	}
	if currentMission != nil {
		currentMission.Log("FsGit: dir=%s, args=%v", abs, args)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = abs
	output, err := cmd.CombinedOutput()
	if currentMission != nil {
		currentMission.Log("FsGit: output (err=%v): %s", err, strings.TrimSpace(string(output)))
	}
	if err != nil {
		return string(output), &models.ToolError{Code: models.CodeConflict, Msg: fmt.Sprintf("git %s exited with an error", subcmd), Hint: "read the output above; fix the reported problem or adjust the arguments", Err: err}
	}
	return string(output), nil
}

func FsCd(args []string, stdin string) (string, error) {
	if len(args) == 0 {
		return "", models.InvalidArgs("usage: cd <dir>", "")
	}
	dir := args[0]
	// Resolve the path: absolute paths are used as-is; relative paths are
	// resolved against the current FilePickerDir (like a real shell).
	var abs string
	if filepath.IsAbs(dir) {
		abs = filepath.Clean(dir)
	} else {
		abs = filepath.Join(cfg.FilePickerDir, dir)
		abs = filepath.Clean(abs)
	}
	// Guard against path-doubling: if the target is the current directory
	// or a parent of it, just use the current FilePickerDir as-is.
	// Real shells are idempotent when you cd to a directory you're already in.
	if abs == cfg.FilePickerDir {
		return "Already in: " + abs, nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", models.NotFound(fmt.Sprintf("cd: no such directory: %s", dir), "list the parent directory with `bash ls`")
	}
	if !info.IsDir() {
		return "", models.InvalidArgs("cd: not a directory: "+dir, "")
	}
	cfg.FilePickerDir = abs
	return "Changed directory to: " + cfg.FilePickerDir, nil
}

func FsMemory(args []string, stdin string) (string, error) {
	if len(args) == 0 {
		return "", models.InvalidArgs("usage: memory store <topic> <data> | memory get <topic> | memory list | memory forget <topic>", "")
	}
	if memoryStore == nil {
		return "", models.Unavailable("memory store not initialized", "memory is disabled in this session")
	}
	switch args[0] {
	case "store":
		if len(args) < 3 && stdin == "" {
			return "", models.InvalidArgs("usage: memory store <topic> <data>", "")
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
			return "", models.Internal("failed to store memory", err)
		}
		return "Stored under topic: " + topic, nil
	case "get":
		if len(args) < 2 {
			return "", models.InvalidArgs("usage: memory get <topic>", "")
		}
		topic := args[1]
		data, err := memoryStore.Recall(agentRole, topic)
		if err != nil {
			return "", models.Internal("failed to recall memory", err)
		}
		return fmt.Sprintf("Topic: %s\n%s", topic, data), nil
	case "list", "topics":
		topics, err := memoryStore.RecallTopics(agentRole)
		if err != nil {
			return "", models.Internal("failed to list memory topics", err)
		}
		if len(topics) == 0 {
			return "No topics stored.", nil
		}
		return "Topics: " + strings.Join(topics, ", "), nil
	case "forget", "delete":
		if len(args) < 2 {
			return "", models.InvalidArgs("usage: memory forget <topic>", "")
		}
		topic := args[1]
		err := memoryStore.Forget(agentRole, topic)
		if err != nil {
			return "", models.Internal("failed to forget memory topic", err)
		}
		return "Deleted topic: " + topic, nil
	default:
		return "", models.InvalidArgs(fmt.Sprintf("unknown memory subcommand: %s", args[0]), "use: store, get, list, topics, forget, delete")
	}
}
