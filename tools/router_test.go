package tools

import (
	"gf-lt/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTier1VerbsAreRegistered(t *testing.T) {
	// Every verb the docs and the tool guide promise must resolve, or the model
	// is told about a capability that does not exist.
	for _, name := range []string{
		"read", "write", "edit_text", "edit_lines",
		"view_img", "memory", "browser", "help", "window", "capture", "capture_and_view",
	} {
		if _, ok := lookupVerb(name); !ok {
			t.Errorf("tier-1 verb %q is not registered", name)
		}
	}
	for alias, want := range map[string]string{
		"windows":             "window",
		"screenshot":          "capture",
		"screenshot_and_view": "capture_and_view",
	} {
		v, ok := lookupVerb(alias)
		if !ok || v.name != want {
			t.Errorf("alias %q should route to %q, got %+v", alias, want, v)
		}
	}
}

func TestTier2FallsThroughToTheShell(t *testing.T) {
	// Anything that is not a tier-1 verb goes to the shell. Previously these
	// were refused with "command not allowed" by a hand-maintained allowlist.
	for _, c := range []string{"ls", "echo hi", "grep -r x .", "true"} {
		if _, ok := lookupVerb(strings.Fields(c)[0]); ok {
			t.Errorf("%q should not be a tier-1 verb", c)
		}
	}
	out, err := CallToolWithAgent("bash", map[string]string{"command": "echo tier2-works"})
	if err != nil {
		t.Fatalf("tier-2 command failed: %v (%s)", err, out)
	}
	if !strings.Contains(string(out), "tier2-works") {
		t.Errorf("tier-2 output wrong: %q", out)
	}
}

func TestShellPassthroughActuallyExpands(t *testing.T) {
	// This is why tier 2 goes to a real shell. ExecChain does not expand globs
	// or variables, and it reported success while doing so, which is the worst
	// possible combination: a wrong answer that looks like a right one.
	dir := t.TempDir()
	prev := GetFSRoot()
	SetFSRoot(dir)
	defer SetFSRoot(prev)
	for _, f := range []string{"a.go", "b.go", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	out, err := CallToolWithAgent("bash", map[string]string{"command": "echo *.go"})
	if err != nil {
		t.Fatalf("glob failed: %v (%s)", err, out)
	}
	if strings.Contains(string(out), "*.go") {
		t.Errorf("glob was not expanded: %q", out)
	}
	for _, want := range []string{"a.go", "b.go"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("glob missing %s in %q", want, out)
		}
	}

	out, err = CallToolWithAgent("bash", map[string]string{"command": "echo $HOME"})
	if err != nil {
		t.Fatalf("expansion failed: %v", err)
	}
	if strings.Contains(string(out), "$HOME") {
		t.Errorf("variable was not expanded: %q", out)
	}
}

func TestFileVerbsParsePositionalArguments(t *testing.T) {
	// These five verbs used to be wired to the tool-argument map, so
	// `bash "read main.go 40 20"` looked for a "path" key in a map that only had
	// "command" in it, and every single call failed with "path is required".
	dir := t.TempDir()
	prev := GetFSRoot()
	SetFSRoot(dir)
	defer SetFSRoot(prev)

	if err := os.WriteFile(filepath.Join(dir, "f.txt"),
		[]byte("alpha\nbeta\ngamma\ndelta\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := CallToolWithAgent("bash", map[string]string{"command": "read f.txt"})
	if err != nil {
		t.Fatalf("read verb failed: %v (%s)", err, out)
	}
	if !strings.Contains(string(out), "alpha") {
		t.Errorf("read verb output wrong: %q", out)
	}

	// offset/limit, as the help text and tool guide have always claimed
	out, err = CallToolWithAgent("bash", map[string]string{"command": "read f.txt 2 2"})
	if err != nil {
		t.Fatalf("read verb range failed: %v (%s)", err, out)
	}
	if !strings.Contains(string(out), "lines 2-3 of 5") {
		t.Errorf("offset/limit not honoured: %q", out)
	}

	// write, with the content joined back together
	_, err = CallToolWithAgent("bash", map[string]string{"command": `write out.txt hello brave world`})
	if err != nil {
		t.Fatalf("write verb failed: %v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "out.txt"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "hello brave world" {
		t.Errorf("write verb lost quoting: %q", data)
	}

	// edit_text
	_, err = CallToolWithAgent("bash", map[string]string{"command": "edit_text f.txt beta BETA"})
	if err != nil {
		t.Fatalf("edit verb failed: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "f.txt"))
	if !strings.Contains(string(data), "BETA") {
		t.Errorf("edit_text verb did not apply: %q", data)
	}

	// edit_lines with an explicit end line
	_, err = CallToolWithAgent("bash", map[string]string{"command": `edit_lines f.txt 1 1 replaced`})
	if err != nil {
		t.Fatalf("edit_lines verb failed: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "f.txt"))
	if !strings.HasPrefix(string(data), "replaced") {
		t.Errorf("edit_lines verb did not apply: %q", data)
	}
}

func TestFileVerbsEnforceTheFsRoot(t *testing.T) {
	dir := t.TempDir()
	prev := GetFSRoot()
	SetFSRoot(dir)
	defer SetFSRoot(prev)

	// The file verbs go through resolvePath, so leaving the root is refused even
	// though the shell tier would happily read /etc/hostname. That asymmetry is
	// documented; the verbs are the guarded path.
	_, err := CallToolWithAgent("bash", map[string]string{"command": "read ../../../../etc/hostname"})
	if err == nil {
		t.Fatal("file verbs must enforce the fs root")
	}
	if !strings.Contains(err.Error(), "escapes fs root") {
		t.Errorf("expected a root-escape error, got: %v", err)
	}
}

func TestHelpVerbTableMatchesTheRouter(t *testing.T) {
	text := verbHelpText()
	for _, name := range VerbNames() {
		if !strings.Contains(text, name) {
			t.Errorf("verb table omits %q", name)
		}
	}
	if !strings.Contains(text, "Tier 2") {
		t.Error("verb table should say what goes to the shell")
	}
	// A shell command must not be described as a verb.
	if _, ok := verbHelpFor("ls"); ok {
		t.Error("`ls` is a shell command, not a tier-1 verb")
	}
}

// The veto list must survive the switch to shell passthrough. With a real shell,
// a nested `bash -c` is the obvious one-token bypass, so the policy unwraps
// indirection before matching.
func TestVetoSurvivesShellIndirection(t *testing.T) {
	UnregisterConfirmConsumer()
	bypasses := []string{
		`bash -c "rm -rf /"`,
		`sh -c 'rm -rf /'`,
		`zsh -c "mkfs.ext4 /dev/sda1"`,
		`env rm -rf /`,
		`nohup rm -rf /`,
		`eval "rm -rf /"`,
		`xargs rm -rf /`,
		`command rm -rf /`,
		`bash -lc "git push --force"`,
		`echo hi && bash -c "rm -rf /"`,
		`timeout 5 rm -rf /`,
	}
	for _, c := range bypasses {
		err := EnforceCommandPolicy("bash", c)
		if err == nil {
			t.Errorf("veto bypassed via indirection: %q", c)
			continue
		}
		if !strings.Contains(err.Error(), "refusing") {
			t.Errorf("expected a veto, got %q -> %v", c, err)
		}
	}
}

func TestConfirmSurvivesShellIndirection(t *testing.T) {
	UnregisterConfirmConsumer()
	for _, c := range []string{`bash -c "rm notes.txt"`, `env git push`} {
		if EnforceCommandPolicy("bash", c) == nil {
			t.Errorf("confirm-list command should still be gated: %q", c)
		}
	}
}

func TestIndirectionUnwrappingDoesNotBreakOrdinaryCommands(t *testing.T) {
	UnregisterConfirmConsumer()
	// A leading-space VAR=value assignment and a plain pipeline must survive the
	// unwrapper without being mistaken for a wrapper command.
	for _, c := range []string{
		"FOO=bar go build ./...",
		"cat go.mod | head -2",
		"ls -la",
		"time git status",
	} {
		if err := EnforceCommandPolicy("bash", c); err != nil {
			t.Errorf("ordinary command %q was gated: %v", c, err)
		}
	}
}

func TestPolicyArgvUnwrapsToRealCommands(t *testing.T) {
	got := policyArgv(`bash -c "echo a && rm -rf /"`)
	var names []string
	for _, argv := range got {
		names = append(names, argv[0])
	}
	joined := strings.Join(names, " ")
	if !strings.Contains(joined, "rm") {
		t.Errorf("nested rm not seen by the policy, got %v", names)
	}
	// The wrapper itself should not be reported as a command to judge.
	for _, n := range names {
		if n == "bash" {
			t.Error("the shell wrapper should be unwrapped, not judged")
		}
	}
}

func TestVetoUnwrappingIsDepthBounded(t *testing.T) {
	// A self-referential construct must not spin forever.
	done := make(chan struct{})
	go func() {
		EnforceCommandPolicy("bash", `bash -c "bash -c \"bash -c 'rm -rf /'\""`)
		close(done)
	}()
	<-done
}

// The policy unit tests call EnforceCommandPolicy directly, which skips runCmd's
// own normalisation. This checks the whole path, because the two used to
// disagree: runCmd stripped a leading "bash " *before* the policy ran, so
// `bash -c "rm -rf /"` was judged as the argv ["-c", "rm -rf /"] and the nested
// shell was never seen. The unit test passed; the command still ran.
func TestVetoEndToEndThroughTheTool(t *testing.T) {
	UnregisterConfirmConsumer()
	dir := t.TempDir()
	prev := GetFSRoot()
	SetFSRoot(dir)
	defer SetFSRoot(prev)

	attempts := []string{
		"rm -rf /",
		`bash -c "rm -rf /"`,
		`bash "rm -rf /"`,
		`sh -c 'mkfs.ext4 /dev/sda1'`,
		`env rm -rf /`,
		`bash -lc "git push --force"`,
		`echo hi && bash -c "rm -rf /"`,
	}
	for _, c := range attempts {
		out, err := CallToolWithAgent("bash", map[string]string{"command": c})
		if err == nil {
			t.Errorf("ran a vetoed command: %q -> %q", c, out)
			continue
		}
		if models.ErrorCodeOf(err) != models.CodeDenied {
			t.Errorf("expected denied for %q, got %s: %v", c, models.ErrorCodeOf(err), err)
		}
	}
}

func TestRedundantBashPrefixStillWorks(t *testing.T) {
	// The `bash ` prefix some models emit is a convenience, and stripping it is
	// how the policy-hole above got opened. It must keep working for ordinary
	// commands while leaving nested shells intact.
	out, err := CallToolWithAgent("bash", map[string]string{"command": "bash echo stripped"})
	if err != nil {
		t.Fatalf("bash prefix broke: %v (%s)", err, out)
	}
	if !strings.Contains(string(out), "stripped") {
		t.Errorf("bash prefix output wrong: %q", out)
	}
}

func TestTier2NormalisationReachesTheShell(t *testing.T) {
	// executeCommand reads the command string, not the tool-arg map, so a
	// normalised command actually reaches the shell. When it read the map, a
	// stripped prefix was discarded here and the original was re-run.
	out, err := CallToolWithAgent("bash", map[string]string{"command": `bash echo once`})
	if err != nil {
		t.Fatalf("failed: %v (%s)", err, out)
	}
	if strings.Count(string(out), "once") != 1 {
		t.Errorf("command should run exactly once, got: %q", out)
	}
}
