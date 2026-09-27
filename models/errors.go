package models

import (
	"errors"
	"fmt"
	"strings"
)

// Tool errors live here rather than in the tools package because they are part
// of the message contract, not of any one tool implementation: they describe how
// a tool result is rendered, and both the tools package and the agent package
// need to produce them. `tools` imports `models`; the reverse would be a cycle.
//
// Handlers used to signal failure by returning a string starting with "[error]",
// and the host recovered the fact by sniffing that prefix (IsToolError), plus a
// pile of content heuristics ("cannot use", "\nFAIL\n") that false-positived on
// successful payloads that merely mentioned the word. This file makes the error a
// value: handlers return it, the host reads it structurally, and exactly one
// function decides what the model is told.
//
// The model still only ever sees a string - the OpenAI tool-result message has no
// error channel - so the model-facing rendering is deliberately a single fixed
// prefix at position 0. Position matters (an error buried in a 40-line output
// gets skimmed) and a fixed prefix cannot false-positive on content, which a
// substring match can.

// ErrorCode classifies a failure so the host can act on it without parsing text.
// Notably, CodeDenied is a legitimate outcome rather than a malfunction: it must
// not consume the mission failure budget.
type ErrorCode string

const (
	// CodeInvalidArgs: the call was malformed. Almost always agent-fixable.
	CodeInvalidArgs ErrorCode = "invalid_args"
	// CodeNotFound: a named file/tool/subject does not exist. Agent-fixable.
	CodeNotFound ErrorCode = "not_found"
	// CodeConflict: the call was well-formed but the world is not in the state it
	// requires (ambiguous edit match, file changed underneath, target not clean).
	// Agent-fixable, but only by changing the approach.
	CodeConflict ErrorCode = "conflict"
	// CodeDenied: policy refused the call. Not a failure, and retrying unchanged
	// will not help. Carries no hint on purpose.
	CodeDenied ErrorCode = "denied"
	// CodeUnknownTool: the model named a tool that does not exist. This is a
	// model error rather than a malfunction, and charging it against the failure
	// budget only teaches the solver to guess different names.
	CodeUnknownTool ErrorCode = "unknown_tool"
	// CodeUnavailable: an optional dependency is missing (Playwright, xdotool).
	// Not agent-fixable.
	CodeUnavailable ErrorCode = "unavailable"
	// CodeInternal: we broke. The cause is logged, never shown to the model.
	CodeInternal ErrorCode = "internal"
)

// ToolError is the error type handlers return.
//
// Msg is model-facing: one line, no internal detail, no file paths we did not
// choose to reveal. Hint is model-facing and optional: give it only when a
// different next call would plausibly work, because a hint on a terminal failure
// just invites a blind retry. Err is host-only and is never rendered.
type ToolError struct {
	Code ErrorCode
	Msg  string
	Hint string
	Err  error
}

func (e *ToolError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Msg, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Msg)
}

func (e *ToolError) Unwrap() error { return e.Err }

// ModelText renders the model-facing half. Kept separate from Error() so the
// wrapped cause can never leak by accident.
func (e *ToolError) ModelText() string {
	text := fmt.Sprintf("[tool_error: %s] %s", e.Code, e.Msg)
	if e.Hint != "" {
		text += "\nhint: " + e.Hint
	}
	return text
}

// IsFailure reports whether err should count against the caller's failure
// budget.
//
// A denial and an unknown tool name are outcomes, not malfunctions: refusing a
// destructive command is the system working, and a hallucinated tool name says
// something about the caller. Charging either against the budget pushes a
// mission solver toward workarounds, which is the opposite of what we want.
func IsFailure(err error) bool {
	if err == nil {
		return false
	}
	var te *ToolError
	if errors.As(err, &te) {
		return te.Code != CodeDenied && te.Code != CodeUnknownTool
	}
	return true
}

// ErrorCodeOf returns the classification of err, or CodeInternal for an error
// that did not come from a handler.
func ErrorCodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var te *ToolError
	if errors.As(err, &te) {
		return te.Code
	}
	return CodeInternal
}

// Constructors. Each defaults the Hint to the usual recovery for that class; pass
// an empty hint to suppress it.

func InvalidArgs(msg, hint string) *ToolError {
	if hint == "" {
		hint = "check the argument names and types against this tool's description"
	}
	return &ToolError{Code: CodeInvalidArgs, Msg: msg, Hint: hint}
}

func NotFound(msg, hint string) *ToolError {
	if hint == "" {
		hint = "list the containing directory first to get the exact name"
	}
	return &ToolError{Code: CodeNotFound, Msg: msg, Hint: hint}
}

func Conflict(msg, hint string) *ToolError {
	if hint == "" {
		hint = "re-read the file to see its current state before retrying"
	}
	return &ToolError{Code: CodeConflict, Msg: msg, Hint: hint}
}

func Denied(msg string) *ToolError {
	// No hint: the same call will be denied again.
	return &ToolError{Code: CodeDenied, Msg: msg}
}

func Unavailable(msg, hint string) *ToolError {
	return &ToolError{Code: CodeUnavailable, Msg: msg, Hint: hint}
}

func Internal(msg string, err error) *ToolError {
	return &ToolError{Code: CodeInternal, Msg: msg, Err: err}
}

// Exported constructors, for tool providers that live outside this package -
// currently the MCP bridge in mcp/, which wraps third-party servers whose error
// semantics we do not control.

// Constructors with an explicit code, for providers that wrap third-party
// failures and cannot map them onto the codes above. Currently the MCP bridge.

// NewToolError builds a ToolError with an explicit code and hint.
func NewToolError(code ErrorCode, msg, hint string, cause error) *ToolError {
	return &ToolError{Code: code, Msg: msg, Hint: hint, Err: cause}
}

// NewDenied reports a refusal by policy. Exported for the confirmation prompt,
// which lives outside this package. No hint: the same call will be refused again.
func NewDenied(label string) *ToolError {
	return Denied("this command requires user confirmation: " + label)
}

// NewToolNotFound reports a call naming a resource that does not exist. For a
// tool name that does not exist, use CodeUnknownTool directly: a hallucinated
// tool name is a model error and does not count against the failure budget.
func NewToolNotFound(msg string) *ToolError {
	return NotFound(msg, "")
}

// NewToolInternal reports a failure on our side of the boundary. The cause is
// logged, never shown to the model.
func NewToolInternal(msg string, cause error) *ToolError {
	return Internal(msg, cause)
}

// IsMultimodalPayload reports whether out is a multimodal_content blob.
//
// Consumers detect these by prefix (`{"type":"multimodal_content"` - see
// bot.go), which means a tool result cannot be both multimodal and prefixed with
// a text error header. Handlers that can produce one therefore carry the error
// inside the payload as a text part, via MultimodalErrorPart, and get a
// non-nil error alongside for the host. RenderToolResult leaves such output
// alone.
func IsMultimodalPayload(out []byte) bool {
	return strings.HasPrefix(strings.TrimSpace(string(out)), `{"type":"multimodal_content"`)
}

// MultimodalErrorPart renders a ToolError as a text part to embed in a
// multimodal payload, so the model reads it in the same place it reads the
// image, and the host still gets a real error value.
func MultimodalErrorPart(err error) map[string]string {
	var te *ToolError
	if !errors.As(err, &te) {
		te = Internal("internal tool error", err)
	}
	return map[string]string{"type": "text", "text": te.ModelText()}
}

// RenderToolResult combines whatever a handler produced with its error into the
// single string the model will read.
//
// The output is kept on the error path on purpose: a tool that applied 3 of 4
// edits still has useful output, and the old "[error]" convention implied
// "discard the rest".
func RenderToolResult(toolName string, out []byte, err error) []byte {
	if err == nil {
		return out
	}
	var te *ToolError
	if !errors.As(err, &te) {
		// A handler returned a bare error. Keep the cause out of the model's
		// view; the host logs it with full detail.
		te = Internal("internal tool error", err)
	}
	// A multimodal payload is detected by its prefix downstream, so prepending a
	// header would turn the image into unreadable text. Those handlers embed the
	// error as a text part instead; leave their output as it is.
	if IsMultimodalPayload(out) {
		return out
	}
	header := te.ModelText()
	if len(out) == 0 {
		return []byte(header)
	}
	return []byte(header + "\n" + string(out))
}
