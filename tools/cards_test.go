package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// backticked finds any `...` span in a sysprompt. Cards used to document tools
// inline (file_edit, insert_at, edit) and went stale silently when the tools
// were renamed, so the model was told to call handlers that do not exist.
// Cards should defer to the injected tool guide instead.
var backticked = regexp.MustCompile("`([^`\n]+)`")

// bareToolName matches a backticked span that is a single lowercase
// identifier, i.e. a plausible tool name rather than a shell invocation with
// arguments (`sed 's/old/new/'`, `file_edit <file> <start>`).
var bareToolName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// cardSysPrompt is the subset of CharCard we care about here.
type cardSysPrompt struct {
	SysPrompt string `json:"sys_prompt"`
}

// TestCardToolNamesExist checks that every backticked single-word identifier in
// a card is a real tool. The tool guide injected per request is the single
// source of truth for tool names; a card that names its own is a second copy
// that can rot.
func TestCardToolNamesExist(t *testing.T) {
	// Shell commands and git subcommands that also appear in backticks. These
	// are ordinary prose in a sysprompt, not tool references.
	allow := map[string]bool{
		"ls": true, "cat": true, "grep": true, "find": true, "sed": true,
		"rm": true, "cp": true, "mv": true, "mkdir": true, "cd": true,
		"pwd": true, "echo": true, "stat": true, "head": true, "tail": true,
		"wc": true, "sort": true, "uniq": true, "time": true, "bash": true,
		"sh": true, "git": true, "go": true, "EOF": true, "user": true,
		"char": true, "id": true, "feat": true, "fix": true,
		// git subcommands named in policy prose
		"add": true, "commit": true, "checkout": true, "push": true,
		"branch": true, "reset": true, "stash": true, "restore": true,
		"switch": true, "merge": true, "rebase": true, "status": true,
		"log": true, "diff": true, "show": true, "reflog": true,
		"describe": true, "remote": true, "fetch": true, "pull": true,
		"archive": true,
	}

	schemas := map[string]bool{}
	// MissionBaseTools is only populated by RegisterMissionTools(), which runs
	// in mission mode. Register them here so the check does not depend on
	// init order or on which mode the test binary happens to run in.
	RegisterMissionTools()
	// ToolSchemas includes mission-only tools (create_pr, pm_consult, ...),
	// which a card may legitimately reference.
	for _, t := range ToolSchemas() {
		schemas[t.Function.Name] = true
	}
	// summarize_chat is internal: a real handler, deliberately not advertised,
	// driven by bot.go rather than the model.
	schemas["summarize_chat"] = true

	matches, err := filepath.Glob("../sysprompts/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no cards found; glob is wrong")
	}

	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var c cardSysPrompt
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if c.SysPrompt == "" {
			continue
		}
		for _, m := range backticked.FindAllStringSubmatch(c.SysPrompt, -1) {
			name := m[1]
			if !bareToolName.MatchString(name) {
				continue // has arguments: a shell invocation, not a bare tool name
			}
			if allow[name] || schemas[name] {
				continue
			}
			t.Errorf("%s references unknown tool %q; "+
				"remove it or use a real tool name (the tool guide is injected per request)",
				filepath.Base(path), name)
		}
	}
}
