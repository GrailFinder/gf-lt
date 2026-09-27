package tools

import (
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
	out, ok := CallToolWithAgent("definitely_not_a_tool", map[string]string{})
	if ok {
		t.Error("expected ok=false for unknown tool")
	}
	if !strings.Contains(string(out), "help") {
		t.Errorf("unknown-tool error should point at the help tool, got: %s", out)
	}
}

func TestHelpListsToolsAndSeparatesSubcommands(t *testing.T) {
	help := getHelp(nil)
	if !strings.Contains(help, "Available tools (") {
		t.Errorf("help should list the live tool set:\n%s", help)
	}
	if !strings.Contains(help, "not separate tools") {
		t.Errorf("help should mark bash subcommands as not-tools:\n%s", help)
	}
	toolsOnly := getHelp([]string{"tools"})
	if !strings.Contains(toolsOnly, "Available tools (") {
		t.Error("help tools should list tools")
	}
	if strings.Contains(toolsOnly, "not separate tools") {
		t.Error("help tools should not include the subcommand list")
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

func TestWriteRefusesEmptyContentUnlessTruncateSet(t *testing.T) {
	dir := t.TempDir()
	prev := GetFSRoot()
	SetFSRoot(dir)
	t.Cleanup(func() { SetFSRoot(prev) })
	path := "keep.txt"
	abs := dir + "/" + path
	if got := FsWrite(map[string]string{"file_path": path, "content": "hello\n"}); strings.HasPrefix(got, "[error]") {
		t.Fatalf("normal write failed: %s", got)
	}

	got := FsWrite(map[string]string{"file_path": path, "content": ""})
	if !strings.HasPrefix(got, "[error]") {
		t.Fatalf("empty content should be refused, got: %s", got)
	}
	if data := mustReadFile(t, abs); data != "hello\n" {
		t.Fatalf("file was modified by a refused write: %q", data)
	}

	// explicit opt-in truncates, and says so
	got = FsWrite(map[string]string{"file_path": path, "content": "", "truncate": "true"})
	if strings.HasPrefix(got, "[error]") {
		t.Fatalf("truncate=true should be allowed: %s", got)
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
