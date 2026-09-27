package models

import (
	"errors"
	"strings"
	"testing"
)

// The tool error contract lives in models, not in tools, because it is part of
// the message contract rather than of any one tool implementation. These tests
// moved here with it; `tools` imports `models`, and the agent package produces
// tool errors too, so the dependency only points one way.

func TestToolErrorRendering(t *testing.T) {
	te := &ToolError{Code: CodeNotFound, Msg: "no such file: x.md", Hint: "list the directory first"}
	got := string(RenderToolResult("read", nil, te))
	if !strings.HasPrefix(got, "[tool_error: not_found] no such file: x.md") {
		t.Errorf("render should lead with the code at position 0, got: %s", got)
	}
	if !strings.Contains(got, "hint: list the directory first") {
		t.Errorf("render should include the hint, got: %s", got)
	}

	// Output survives the error path: a tool that applied 3 of 4 edits still has
	// useful output, and the old "[error]" convention implied discarding it.
	got = string(RenderToolResult("edit", []byte("applied 3 of 4"), te))
	if !strings.Contains(got, "applied 3 of 4") {
		t.Errorf("output must be kept alongside the error, got: %s", got)
	}

	// A bare error must not leak its cause into the model's view.
	got = string(RenderToolResult("x", nil, errors.New("/internal/secret/path: permission denied")))
	if strings.Contains(got, "/internal/secret/path") {
		t.Errorf("internal cause leaked to the model: %s", got)
	}
	if !strings.Contains(got, "[tool_error: internal]") {
		t.Errorf("bare errors should render as internal, got: %s", got)
	}
}

func TestDeniedIsNotAFailure(t *testing.T) {
	if IsFailure(Denied("nope")) {
		t.Error("a denial must not count as a failure")
	}
	if !IsFailure(Internal("boom", errors.New("x"))) {
		t.Error("an internal error should count as a failure")
	}
	if !IsFailure(NotFound("gone", "")) {
		t.Error("a genuine not_found should count as a failure")
	}
	if IsFailure(&ToolError{Code: CodeUnknownTool, Msg: "nope"}) {
		t.Error("a hallucinated tool name should not count as a failure")
	}
	if IsFailure(nil) {
		t.Error("nil is not a failure")
	}
}

func TestRenderedErrorCannotFalsePositiveOnContent(t *testing.T) {
	// The old heuristic flagged a successful payload merely mentioning the word.
	// A successful tool result must render with no error marker at all.
	got := string(RenderToolResult("write", []byte(`wrote a.json: {"note":"no error here"}`), nil))
	if strings.Contains(got, "tool_error") {
		t.Errorf("a successful result must not render an error marker: %s", got)
	}
}
