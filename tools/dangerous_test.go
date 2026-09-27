package tools

import (
	"gf-lt/models"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestConfirmationDoesNotBlockWithoutAConsumer is the regression test for the
// CLI-mode hang: RequestConfirmation used to do an unguarded channel send
// followed by a blocking receive, and nothing drains ConfirmChan outside the
// TUI. `gf-lt --cli` asking for an `rm` wedged the process until it was killed.
func TestConfirmationDoesNotBlockWithoutAConsumer(t *testing.T) {
	UnregisterConfirmConsumer()
	if HasConfirmConsumer() {
		t.Fatal("no consumer should be registered in a test")
	}

	done := make(chan bool, 1)
	go func() {
		done <- RequestConfirmation(ConfirmRequest{ToolName: "bash", Command: "rm -rf /"})
	}()

	select {
	case approved := <-done:
		if approved {
			t.Error("with no consumer available, the answer must be deny")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DEADLOCK: RequestConfirmation blocked with no consumer")
	}
}

func TestConfirmationDeniesWhenConsumerIsGone(t *testing.T) {
	// Simulate a UI that registered and then died: the request must time out
	// rather than park forever. Uses a short-lived stand-in for the timeout by
	// draining nothing and relying on the non-blocking send path.
	RegisterConfirmConsumer()
	UnregisterConfirmConsumer()

	done := make(chan bool, 1)
	go func() {
		done <- RequestConfirmation(ConfirmRequest{ToolName: "bash", Command: "rm x"})
	}()
	select {
	case approved := <-done:
		if approved {
			t.Error("a vanished consumer must not approve")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RequestConfirmation blocked after the consumer went away")
	}
}

func TestConfirmationAnswersThroughAChannel(t *testing.T) {
	RegisterConfirmConsumer()
	defer UnregisterConfirmConsumer()
	go func() {
		req := <-ConfirmChan
		req.Result <- true
	}()
	if !RequestConfirmation(ConfirmRequest{ToolName: "bash", Command: "rm x"}) {
		t.Error("an approving consumer should let the command through")
	}
}

// listVetoed / listConfirmed ask the two lists directly, one parsed segment at a
// time. Testing the lists separately matters: with no consumer registered both
// lists end up refusing, so asserting on EnforceCommandPolicy alone cannot tell a
// veto from an unattended confirmation.
func listVetoed(t *testing.T, command string) bool {
	t.Helper()
	for _, seg := range ParseChain(command) {
		if argv := tokenize(seg.Raw); len(argv) > 0 {
			if _, ok := vetoCommand(argv); ok {
				return true
			}
		}
	}
	return false
}

func listConfirmed(t *testing.T, command string) bool {
	t.Helper()
	for _, seg := range ParseChain(command) {
		if argv := tokenize(seg.Raw); len(argv) > 0 {
			if _, ok := confirmCommand(argv); ok {
				return true
			}
		}
	}
	return false
}

func TestVetoListAppliesInEveryMode(t *testing.T) {
	// A veto needs no human, so it must hold with no consumer registered -
	// this is the property mission mode depends on.
	cases := []string{
		"rm -rf /",
		"rm -rf $HOME",
		"rm -rf /etc",
		"rm /usr/lib/thing",
		"rm -rf .",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		"shutdown -h now",
		"reboot",
		"chmod -R 777 /etc",
		"chown -R nobody /usr",
		"git push --force",
		"git push -f origin main",
		"git -C /tmp/repo push --force",
		"git --git-dir=/x/.git push --force",
		"git push --delete origin main",
		"git clean -f",
	}
	for _, c := range cases {
		if !listVetoed(t, c) {
			t.Errorf("should be vetoed in every mode: %q", c)
		}
	}
}

func TestVetoMatchesThroughChainsAndIndirection(t *testing.T) {
	// The old implementation prefix-matched the raw string, so anything that
	// was not the very first token walked past it. These are not exotic
	// escapes - they are how compound commands are written.
	UnregisterConfirmConsumer()

	cases := []string{
		"  rm -rf /",                  // leading whitespace
		"echo hi && rm -rf /",         // second segment
		"cd /tmp && rm -rf /",         // second segment, after cd
		"true; mkfs.ext4 /dev/sda1",   // semicolon separator
		"git log && git push --force", // second git segment
		"cd /repo; git push -f origin main",
	}
	for _, c := range cases {
		if !listVetoed(t, c) {
			t.Errorf("bypass of the veto list: %q", c)
		}
	}
}

func TestVetoLeavesLegitimateWorkAlone(t *testing.T) {
	// The whole point of splitting the lists is that unattended operation still
	// works. If ordinary workspace commands get vetoed, the split is useless.
	UnregisterConfirmConsumer()

	cases := []string{
		"rm -rf node_modules",
		"rm build/tmp.o",
		"git push",
		"git reset --hard HEAD~1",
		"git clean -n",
		"chmod -R 755 ./scripts",
		"go build ./...",
		"grep -r pattern .",
		"dd if=a.img of=b.img", // in-workspace dd is confirm-only, not vetoed
	}
	for _, c := range cases {
		if listVetoed(t, c) {
			t.Errorf("legitimate command was vetoed: %q", c)
		}
	}

	// The confirm list still covers the irreversible-in-principle subset, so a
	// human is asked in the TUI even for these.
	confirmOnly := []string{
		"rm -rf node_modules",
		"git push",
		"git reset --hard HEAD~1",
		"chmod -R 755 ./scripts",
		"dd if=a.img of=b.img",
	}
	for _, c := range confirmOnly {
		if !listConfirmed(t, c) {
			t.Errorf("expected a confirmation for %q, got none", c)
		}
	}

	// ...and ordinary work is not nagged about.
	for _, c := range []string{"go build ./...", "grep -r pattern .", "git status", "ls"} {
		if listVetoed(t, c) || listConfirmed(t, c) {
			t.Errorf("ordinary command should not be gated: %q", c)
		}
	}
}

func TestVetoRespectsAllowOutOfRoot(t *testing.T) {
	if cfg == nil {
		t.Skip("no config")
	}
	prev := cfg.FSAllowOutOfRoot
	cfg.FSAllowOutOfRoot = true
	defer func() { cfg.FSAllowOutOfRoot = prev }()
	UnregisterConfirmConsumer()

	// With the escape hatch on, leaving the root is the user's explicit choice;
	// the veto is about not destroying the machine, not about the workspace.
	if listVetoed(t, "rm -rf /tmp/some-scratch-dir") {
		t.Error("FSAllowOutOfRoot should relax the workspace check")
	}
	// ...but system paths stay refused regardless: that is not a workspace rule.
	if !listVetoed(t, "rm -rf /etc") {
		t.Error("system paths must stay vetoed even with FSAllowOutOfRoot")
	}
}

func TestConfirmListIsAdvisoryWithNoHuman(t *testing.T) {
	// With nobody to ask, a confirm-list command is refused - but it is refused
	// as a denial, not a crash and not a hang.
	UnregisterConfirmConsumer()
	err := EnforceCommandPolicy("bash", "rm notes.txt")
	if err == nil {
		t.Fatal("rm with no consumer should be denied")
	}
	if !strings.Contains(err.Error(), "no interactive user") {
		t.Errorf("the denial should say why: %v", err)
	}
}

func TestPolicyDoesNotFireOnOtherTools(t *testing.T) {
	if err := EnforceCommandPolicy("read", "rm -rf /"); err != nil {
		t.Errorf("policy should only apply to bash, got %v", err)
	}
	if err := EnforceCommandPolicy("bash", ""); err != nil {
		t.Errorf("empty command should be left to argument validation: %v", err)
	}
}

func TestBashToolRefusesVetoedCommand(t *testing.T) {
	// End-to-end through the tool: the model gets a denial it can read and act
	// on, not a crash and not an executed command.
	UnregisterConfirmConsumer()
	out, err := CallToolWithAgent("bash", map[string]string{"command": "rm -rf /"})
	if err == nil {
		t.Fatalf("expected a denial, got: %s", out)
	}
	if models.ErrorCodeOf(err) != models.CodeDenied {
		t.Errorf("expected denied, got %s", models.ErrorCodeOf(err))
	}
	if !strings.Contains(string(out), "[tool_error: denied]") {
		t.Errorf("model should see a rendered denial, got: %s", out)
	}
	if models.IsFailure(err) {
		t.Error("a veto must not count against the mission failure budget")
	}
}

func TestDeniedCommandDoesNotTouchTheFilesystem(t *testing.T) {
	UnregisterConfirmConsumer()
	dir := t.TempDir()
	prev := GetFSRoot()
	SetFSRoot(dir)
	defer SetFSRoot(prev)

	victim := filepath.Join(dir, "precious.txt")
	if err := os.WriteFile(victim, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}
	// A relative path stays inside the root, so this one is confirm-only, and
	// with no consumer it must still not run.
	if _, err := CallToolWithAgent("bash", map[string]string{"command": "rm " + victim}); err == nil {
		t.Fatal("expected the delete to be refused")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("a refused command must not have run: %v", err)
	}
}
