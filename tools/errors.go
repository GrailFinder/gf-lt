package tools

import (
	"errors"
	"fmt"
	"os"
	"unicode/utf8"

	"gf-lt/models"
)

// The tool error contract itself - models.ToolError, its codes, the classification
// helpers, and the rendering of a tool result - lives in models, because it is
// part of the message contract rather than of any one tool implementation. Both
// this package and the agent package produce tool errors, and `tools` imports
// `models`, so the dependency only points one way.
//
// What stays here is the tool-specific part: bounding a tool result's size, and
// turning an OS error from a filesystem path into the right classification.

// MaxToolResultBytes caps a single tool result before it enters the chat
// context. Tool output otherwise flows in unbounded: `bash` on a large file,
// websearch, base64 images. One fat result can push an otherwise healthy
// conversation into context compaction, and silent exhaustion is a much worse
// failure mode than an explicit truncation notice. Mirrors read's continuation
// trailer: say what was cut and how to get the rest.
const MaxToolResultBytes = 24 * 1024

// TruncateToolResult clamps a tool result to MaxToolResultBytes, appending a
// notice that says how to retrieve the remainder. Byte-safe: it cuts on a rune
// boundary so the result stays valid UTF-8.
func TruncateToolResult(toolName string, raw []byte) []byte {
	if len(raw) <= MaxToolResultBytes {
		return raw
	}
	cut := MaxToolResultBytes
	for cut > 0 && !utf8.RuneStart(raw[cut]) {
		cut--
	}
	notice := fmt.Sprintf(
		"\n\n[output truncated: %d of %d bytes shown. Re-run with a narrower scope (e.g. grep/head, an offset+limit range, or a lower limit) to see the rest.]",
		cut, len(raw),
	)
	out := make([]byte, 0, cut+len(notice))
	out = append(out, raw[:cut]...)
	out = append(out, notice...)
	return out
}

// readError classifies an OS error from a read path. A missing file is the
// single most common tool failure and must not be reported as an models.Internal
// malfunction: the model can fix it, and "we broke" invites a retry loop
// instead of a re-read.
func readError(what string, err error) *models.ToolError {
	if errors.Is(err, os.ErrNotExist) {
		return models.NotFound(what+" does not exist", "list the containing directory to get the exact name")
	}
	if errors.Is(err, os.ErrPermission) {
		return models.Denied(what + " is not readable")
	}
	return models.Internal(what+" failed", err)
}
