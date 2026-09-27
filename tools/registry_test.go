package tools

import (
	"gf-lt/models"
	"os"
	"strings"
	"testing"
)

// The registry is assembled from several places; this test is the safety net
// that catches "we advertise a tool whose handler is missing" (or the reverse),
// which is exactly the class of drift that went unnoticed for the window tools.
func TestRegistryIsConsistent(t *testing.T) {
	issues := RegistryIssues()
	if len(issues) == 0 {
		return
	}
	// Mission tools are only registered under --mission; account for that.
	var real []string
	for _, issue := range issues {
		if strings.HasPrefix(issue, "advertised but no handler: ") {
			name := strings.TrimPrefix(issue, "advertised but no handler: ")
			if name == "move_issue" || name == "create_pr" ||
				name == "pm_consult" || name == "add_issue_comment" {
				continue // mission-only, registered by RegisterMissionTools()
			}
		}
		real = append(real, issue)
	}
	if len(real) > 0 {
		t.Errorf("tool registry drift:\n  %s", strings.Join(real, "\n  "))
	}
}

func TestAvailableToolsSorted(t *testing.T) {
	got := AvailableTools()
	if len(got) == 0 {
		t.Fatal("no tools registered")
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("AvailableTools not sorted: %q before %q", got[i-1], got[i])
		}
	}
	if _, ok := FnMap["bash"]; !ok {
		t.Error("bash missing from FnMap")
	}
}

func TestUnknownToolErrorIsActionable(t *testing.T) {
	out, err := CallToolWithAgent("definitely_not_a_tool", map[string]string{})
	if err == nil {
		t.Fatal("expected an error for an unknown tool")
	}
	if models.ErrorCodeOf(err) != models.CodeUnknownTool {
		t.Errorf("unknown tool should be unknown_tool, got %s", models.ErrorCodeOf(err))
	}
	// A model error must not consume a mission failure.
	if models.IsFailure(err) {
		t.Error("an unknown tool name should not count as a tool failure")
	}
	if !strings.Contains(string(out), "help") {
		t.Errorf("unknown-tool error should point at the help tool, got: %s", out)
	}
}

func TestWriteRefusesEmptyContentUnlessTruncateSet(t *testing.T) {
	dir := t.TempDir()
	prev := GetFSRoot()
	SetFSRoot(dir)
	t.Cleanup(func() { SetFSRoot(prev) })
	path := "keep.txt"
	abs := dir + "/" + path
	if out, err := FsWrite(map[string]string{"file_path": path, "content": "hello\n"}); err != nil {
		t.Fatalf("normal write failed: %v", out)
	}

	got, err := FsWrite(map[string]string{"file_path": path, "content": ""})
	if err == nil {
		t.Fatalf("empty content should be refused, got: %s", got)
	}
	// Conflict, not denied: the fix is one argument away, and a refusal the
	// model cannot retry its way out of should not cost a mission failure.
	if models.ErrorCodeOf(err) != models.CodeConflict {
		t.Errorf("empty write should be a conflict, got %s", models.ErrorCodeOf(err))
	}
	if data := mustReadFile(t, abs); data != "hello\n" {
		t.Fatalf("file was modified by a refused write: %q", data)
	}

	// explicit opt-in truncates, and says so
	got, err = FsWrite(map[string]string{"file_path": path, "content": "", "truncate": "true", "overwrite": "true"})
	if err != nil {
		t.Fatalf("truncate=true should be allowed: %v", err)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncation should be reported explicitly, got: %s", got)
	}
	if data := mustReadFile(t, abs); data != "" {
		t.Fatalf("expected empty file, got %q", data)
	}
}

func TestTruncateToolResult(t *testing.T) {
	small := []byte("hello")
	if got := TruncateToolResult("x", small); string(got) != "hello" {
		t.Errorf("small result should pass through, got %q", got)
	}
	big := []byte(strings.Repeat("a", MaxToolResultBytes+500))
	got := TruncateToolResult("bash", big)
	if len(got) > MaxToolResultBytes+300 {
		t.Errorf("result not clamped: %d bytes", len(got))
	}
	if !strings.Contains(string(got), "output truncated") {
		t.Error("truncation notice missing")
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHelpListsToolsAndSeparatesSubcommands(t *testing.T) {
	help := getHelp(nil)
	if !strings.Contains(help, "Available tools (") {
		t.Errorf("help should list the live tool set:\n%s", help)
	}
	if !strings.Contains(help, "Tier 2") {
		t.Errorf("help should say what falls through to the shell:\n%s", help)
	}
	toolsOnly := getHelp([]string{"tools"})
	if !strings.Contains(toolsOnly, "Available tools (") {
		t.Error("help tools should list tools")
	}
	if strings.Contains(toolsOnly, "Tier 1") {
		t.Error("help tools should not include the verb table")
	}
	// The verb table is generated from the router's own table, so it cannot go
	// stale the way the hand-written list did.
	for _, v := range VerbNames() {
		if !strings.Contains(help, v) {
			t.Errorf("help omits tier-1 verb %q", v)
		}
	}
	for _, name := range AvailableTools() {
		if internalTools[name] {
			continue
		}
		if !strings.Contains(toolsOnly, name) {
			t.Errorf("help tools omits registered tool %q", name)
		}
	}
}

func TestBrowserArgsAreQuoteAware(t *testing.T) {
	// The tool is unavailable in CI without Playwright, so test the parsing
	// boundary directly: tokenize is what browserCmd now uses.
	got := tokenize(`fill "#search" "hello world"`)
	want := []string{"fill", "#search", "hello world"}
	if len(got) != len(want) {
		t.Fatalf("tokenize = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tokenize = %q, want %q", got, want)
		}
	}
}
