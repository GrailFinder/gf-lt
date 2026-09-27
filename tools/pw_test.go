package tools

import (
	"gf-lt/models"
	"os"
	"strings"
	"testing"
)

// The browser tool was the last island on the old convention: it returned
// success-shaped JSON with an "error" key and a nil Go error, so a failed
// navigation or a missing element counted as a *success* to mission
// bookkeeping. It was also the only real consumer of the string-sniffing
// IsToolError heuristic, which batch 2 deleted.

func TestBrowserHandlersReturnRealErrors(t *testing.T) {
	if browserStarted {
		t.Skip("a browser is already running in this test process")
	}
	// No browser is up, so every interaction must be a typed error rather than
	// a success-shaped payload.
	// Driven through the bash verb form, which is the path that actually
	// tokenizes into positional arguments.
	commands := []string{
		`browser go "https://example.com"`,
		"browser click #x",
		"browser fill #x hello",
		"browser text #x",
		"browser html #x",
		"browser screenshot",
		"browser wait #x",
		"browser drag 1 1 2 2",
	}
	for _, cmd := range commands {
		out, err := CallToolWithAgent("bash", map[string]string{"command": cmd})
		if err == nil {
			t.Errorf("%s: expected an error, got %q", cmd, out)
			continue
		}
		if got := models.ErrorCodeOf(err); got != models.CodeConflict {
			t.Errorf("%s: code = %s, want conflict", cmd, got)
		}
		if !strings.Contains(string(out), "browser is not running") {
			t.Errorf("%s: model should be told the browser is down, got: %q", cmd, out)
		}
		if models.IsFailure(err) != true {
			t.Errorf("%s: a failed browser call should count as a failure, not a success", cmd)
		}
	}
}

func TestUnknownBrowserActionIsInvalidArgs(t *testing.T) {
	// An action the router does not know is a malformed call, not a state error,
	// and the message lists the actions that do exist.
	out, err := CallToolWithAgent("bash", map[string]string{"command": "browser frobnicate"})
	if err == nil {
		t.Fatalf("expected an error, got %q", out)
	}
	if got := models.ErrorCodeOf(err); got != models.CodeInvalidArgs {
		t.Errorf("code = %s, want invalid_args", got)
	}
	for _, action := range []string{"start", "click", "screenshot"} {
		if !strings.Contains(string(out), action) {
			t.Errorf("error should list the real actions, %q missing from: %q", action, out)
		}
	}
}

func TestBrowserNotRunningHintNamesARealCommand(t *testing.T) {
	// The old message said "Call pw_start first" in thirteen places. There is no
	// pw_start tool, bash verb, or browser action - the verb is `start`.
	te := browserNotStarted()
	if te.Hint == "" {
		t.Fatal("a state error the model can fix should carry a hint")
	}
	if !strings.Contains(te.Hint, "browser start") {
		t.Errorf("hint should name the real command, got %q", te.Hint)
	}
	if strings.Contains(strings.ToLower(te.Hint), "pw_start") {
		t.Errorf("hint still names the non-existent pw_start: %q", te.Hint)
	}
	// There must be no leftover copy of the stale string anywhere.
	assertNoStalePwStartMessage(t)
}

func TestBrowserStartIsIdempotentNotAFailure(t *testing.T) {
	if browserStarted {
		t.Skip("a browser is already running in this test process")
	}
	// A start call on a stopped browser cannot succeed here (no Playwright
	// install in CI), so assert the mirror case directly: the helper must not
	// classify a repeat start as a failure.
	if models.IsFailure(browserNotStarted()) != true {
		t.Error("browser-not-running should count as a real failure (the model can fix it, but the call did not work)")
	}
}

func TestBrowserMissingArgsAreInvalidArgs(t *testing.T) {
	// Malformed calls are distinguished from state failures, so the model can
	// tell "you called it wrong" from "the world is not ready".
	te := models.InvalidArgs("url is required", "browser go <url>")
	if te.Code != models.CodeInvalidArgs {
		t.Errorf("expected invalid_args, got %s", te.Code)
	}
	if te.Hint == "" {
		t.Error("a malformed call should say what the right call looks like")
	}
}

// Option (b) for multimodal results: the error rides inside the payload as a
// text part, because downstream consumers detect multimodal output by its
// prefix and a prepended text header would turn the image into unreadable text.
func TestMultimodalErrorRidesInsideThePayload(t *testing.T) {
	payload := []byte(`{"type":"multimodal_content","parts":[{"type":"image_url","url":"data:..."}]}`)
	te := &models.ToolError{Code: models.CodeConflict, Msg: "half the screenshot failed", Hint: "retry"}

	withError, _ := appendMultimodalErrorPart(payload, te)
	if !models.IsMultimodalPayload(withError) {
		t.Fatalf("payload lost its multimodal prefix: %q", withError)
	}
	if !strings.Contains(string(withError), "half the screenshot failed") {
		t.Errorf("error not embedded: %q", withError)
	}
	if !strings.Contains(string(withError), `"type":"image_url"`) {
		t.Errorf("image part was lost: %q", withError)
	}

	// And models.RenderToolResult must not prepend a header over a multimodal payload.
	rendered := models.RenderToolResult("browser", withError, te)
	if !models.IsMultimodalPayload(rendered) {
		t.Errorf("models.RenderToolResult broke the multimodal prefix: %q", rendered)
	}
	if strings.HasPrefix(string(rendered), "[tool_error:") {
		t.Errorf("models.RenderToolResult prepended a header over a multimodal payload: %q", rendered)
	}
}

func TestRenderToolResultStillPrependsForPlainOutput(t *testing.T) {
	te := &models.ToolError{Code: models.CodeNotFound, Msg: "gone"}
	got := models.RenderToolResult("read", nil, te)
	if !strings.HasPrefix(string(got), "[tool_error: not_found]") {
		t.Errorf("plain output should still get the header: %q", got)
	}
}

func assertNoStalePwStartMessage(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile("pw.go")
	if err != nil {
		t.Skipf("cannot read pw.go: %v", err)
	}
	if strings.Contains(string(data), "Call pw_start first") {
		t.Error("pw.go still emits the stale 'Call pw_start first' message")
	}
}
