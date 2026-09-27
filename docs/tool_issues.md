# Toolset Issues & Improvement Wishes

Notes on the agent toolset in `tools/`, collected while building the
[feature map](featuremap.md). These are **my own observations as a user of the toolset**
(one of the agents configured in this repo), not a code review of the implementation.

Everything here was verified against the source at the time of writing. Where I am
inferring intent rather than reading it, I say so.

---

## Summary

| # | Area | Severity | Kind | Status |
|---|---|---|---|---|
| 1 | `browser` args are not quote-aware | **high** | bug | fixed |
| 2 | Tool error signalling is a string-prefix convention | **high** | design | open (needs decision) |
| 3 | `write` with empty content truncates silently | **high** | safety | fixed |
| 4 | Danger guard is disabled in mission mode | **high** | safety | open (needs decision) |
| 5 | Tool registry has no single source of truth | medium | design | fixed |
| 6 | `bash` subcommand router is a growing `switch` | medium | design | open |
| 7 | File tools have no read-before-write guard | medium | safety | open |
| 8 | `read` silently truncates at 2000 lines | medium | design | fixed |
| 9 | Redundant file-mutation tools | low | design | open |
| 10 | No tool-result size limit | low | design | fixed |
| 11 | `browser`'s `args` is one opaque string | low | design | partially fixed |
| 12 | `help` output is a string, not a resource | low | ux | fixed |
| 13 | Window tools advertised but never registered | medium | bug | fixed |

---

## What landed (batch 1)

`tools/registry.go` is new and is now the single source of truth for the live
tool set. It follows the same idea pi uses: a tool should be able to find out
what it can do, without re-deriving conditional registration by hand.

- `AvailableTools()` / `AvailableToolCount()` / `ToolSchemas()` / `ToolSchemaNames()`
  / `ToolDescription(name)` — the live set, sorted.
- `RegistryIssues() []string` — reports *advertised but no handler*,
  *handler but not advertised*, and *duplicate schema*. Wired into
  `tools/registry_test.go`, so drift fails the build instead of surviving.
- `ToolsHelp()` — one line per live tool, plus any registry warnings.

Fixes:

- **#5 / #12** — `help` now leads with the live tool set and explicitly labels the
  bash subcommand vocabulary as *not* separate tools (`help tools` for just the
  list, `help <cmd>` for one subcommand). The old output listed subcommands
  while reading like a tool list, which is the trap described above.
  `FnMap` had to move from a package-level composite literal to `init()`
  assignments to avoid an initialization cycle (`getHelp → ToolsHelp →
  AvailableTools → FnMap`).
- **#13** — the window tools are registered under the names the removal code
  deletes (`list_windows`, `capture_window`, `capture_window_and_view`), so
  `removeWindowToolsFromBaseTools()` is no longer a no-op. The bash subcommands
  (`window`, `capture`, …) are now gated on the same availability flag, and
  `capture_window_and_view` is only advertised once vision support is known.
- **#1** — `browserCmd` uses the quote-aware `tokenize()` instead of
  `strings.Fields`, so `fill "#search" "hello world"` is expressible. The
  shorthand `browser "go https://x"` also works now.
- **#3** — `write` refuses empty content unless `truncate="true"` is passed
  explicitly, and reports a real truncation as such. Empty content is also
  flagged in the tool description.
- **#8** — `read` always returns a header naming the effective line range and
  whether it was truncated, not only when truncation happens.
- **#10** — `TruncateToolResult` caps a single tool result at
  `MaxToolResultBytes` (24 KiB), on a rune boundary, with a notice saying how to
  get the rest. Applied centrally in `CallToolWithAgent`.
- Unknown tool names now return an actionable error pointing at `help`, instead
  of `tool foo not found`.

Still open, and why:

- **#2** (`FnHandler` → `([]byte, error)`) is the highest-value remaining change
  and the most invasive: it touches every handler. It also needs a decision about
  what happens to the `[error]` prefix convention and `IsToolError`'s JSON
  heuristic.
- **#4** (danger guard in mission mode) needs a product decision, not a refactor.
- **#6 / #7 / #9** are refactors with no bug behind them.

---

## 1. `browser` args are not quote-aware — HIGH

`runCmd` (the `bash` tool) correctly uses the quote-aware `tokenize()`:

```go
// tools/tools.go:417
parts := tokenize(commandStr)
```

This was a deliberate fix, called out in `docs/auto-issue-solver.md` under P8
("Quoting fix"), specifically so `git commit -m "fix: message"` would parse.

`browserCmd` did **not** get the same treatment:

```go
// tools/tools.go:473
browserArgs = strings.Fields(argsStr)
```

`strings.Fields` splits on any whitespace, so quoted arguments containing spaces are
destroyed. The documented usage is:

```
browser fill <selector> <text>
```

So this is broken for its primary use case:

```
browser fill "#search" "hello world"     →  args become: "#search" "hello" "world"
browser fill "#search" hello world       →  same, indistinguishable
```

The second form is indistinguishable from the first, and the LLM has no way to express
"fill this with a multi-word string". There is no escape and no error — it silently
truncates the input.

**Wish:** `browserCmd` should use `tokenize()` like `runCmd` does. Ideally the raw
subcommand string would be tokenized once, rather than each tool re-implementing
splitting.

---

## 2. Tool error signalling is a string-prefix convention — HIGH

This is the one I keep tripping over, and I suspect other agents do too.

Tool handlers return `[]byte`. There is no error channel. Success and failure are
distinguished **only** by matching on the shape of the returned string:

```go
// tools/mission_tools.go:541
func IsToolError(toolName, resp string) bool {
    trimmed := strings.TrimSpace(resp)
    if strings.HasPrefix(trimmed, "[error]") { return true }
    if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, `"error"`) { return true }
    ...
}
```

Consequences:

- **A tool cannot fail loudly.** Any handler that forgets the `[error]` prefix is
  silently treated as success by mission mode's failure tracking.
- **The convention is load-bearing but undocumented.** There is no godoc comment on
  `FnHandler` saying "return `[error] ...` to signal failure". The rule exists only
  discoverable by reading `IsToolError` in a different file, in a different feature.
- **The JSON heuristic is fragile.** `strings.Contains(trimmed, '"error"')` will
  return `true` for a *successful* response whose payload merely mentions the word —
  e.g. a `write` of a file whose contents include the string `"error"`, or any
  tool echoing back user content.
- **It is only consulted in mission mode.** Outside mission mode the prefix is
  decorative and nothing reads it.

**Wish:** change `FnHandler` to return `([]byte, error)`. The `CallToolWithAgent`
signature already returns `(raw []byte, ok bool)`, so there is a precedent for a
second return value — the bool currently means "agent found a processor", not
"did it work", which is its own confusion.

Related: `CallToolWithAgent` returns `false` for an unknown tool, and callers
(`handleBatchToolCalls`) treat that as a mission failure. An LLM hallucinating
a tool name therefore burns one of only 3 allowed failures. That is arguably
correct, but a typo'd tool would be indistinguishable from a real problem.

---

## 3. `write` with empty content truncates silently — HIGH

`write` validates `file_path` but says nothing about `content`:

```go
// tools/edit.go:156
if filePath == "" { return "[error] file_path not provided" }
// ... no check that content is non-empty
if err := os.WriteFile(abs, []byte(content), 0644); err != nil { ... }
```

So `write` with `content: ""` truncates the target to zero bytes. For a coding
agent this is the single most destructive possible mistake, and it is
indistinguishable from "successfully cleared the file" in the return value:

```
wrote main.go (0 lines, 0 bytes)
```

`IsToolError` does not catch it — no `[error]` prefix, not JSON, no `FAIL`.

The neighbouring tools *do* guard empty input (`insert_at` and `edit` both reject
empty required strings), so this looks like an omission in `write` rather than a
deliberate policy.

**Wish:** `write` should refuse empty `content`, or require an explicit
`truncate: true` opt-in. At minimum the return value should not read like a
routine success. Relatedly, "delete this file" is currently expressible *only* as
`write` with empty content, which is exactly the wrong affordance — `rm` should
be the honest spelling, and it's already available via `bash`.

---

## 4. The danger guard is disabled in mission mode — HIGH

There is a confirmation prompt for destructive commands, and it is well built:

```go
// tools/dangerous.go:16
func IsDangerousCommand(name string, args map[string]string) (bool, string)
// blocklist: rm, git push, sudo, dd, shred, mkfs, fdisk, shutdown,
//            poweroff, reboot, iptables, ufw, chmod -R, chown -R
```

But both call sites gate it on `!tools.IsMissionMode()`:

```go
// bot.go:1605  (executeOneToolCall — the modern /v1/chat path)
// bot.go:1901  (findCall — the legacy /completion path)
if !tools.IsMissionMode() {
    if dangerous, label := tools.IsDangerousCommand(...); dangerous {
        approved := tools.RequestConfirmation(req)
        ...
    }
}
```

and

```go
// tools/mission_tools.go:39
func IsMissionMode() bool { return currentMission != nil }
```

`currentMission` is set only by `runMissionMode()`. So:

| Mode | Guard active? |
|---|---|
| TUI | yes |
| CLI (`--cli`, `--msg`) | yes |
| Mission (`--mission`) | **no** |

I want to be clear that I read this as *intentional* — an autonomous solver that
has to stop and ask a human before every `rm` cannot function unattended, and
`SIGINT`/checkpointing already exist as the escape hatch. There is also a
`workflow.guardrails.block_patterns` concept in the agent card for card-level
policy. So this is not a bug.

**The complaint is about the blast radius.** In mission mode the LLM holds a
`bash` tool with no confirmation and no blocklist, pointed at a real repository.
The safety properties that the rest of the toolset is careful about —
`resolvePath`'s root-escape checks, `edit`'s unique-match requirement — all still
hold, but nothing stops `rm -rf`, `git push`, or `git reset --hard` at the
execution layer. The only backstop is `git` itself, and only if a human made a
commit first.

Worth noting the blocklist is prefix-matched on the raw command string, so
`sh -c 'rm -rf /'`, `$(rm x)`, or `find . -delete` all bypass it — but that's
moot in mission mode anyway, and is a general property of prefix blocklists
rather than a defect in this design.

**Wish:** a middle path. Options, roughly in order of effort:

- Keep mission mode unconfirmed, but honour the agent card's
  `guardrails.block_patterns` (`docs/auto-issue-solver.md` already describes the
  concept) as a hard veto, with a message explaining the denial. This gives the
  card author real authority instead of advisory text.
- Require an explicit opt-in flag for the genuinely irreversible subset
  (`rm -rf /`, `git push --force`, `git reset --hard`) rather than everything.
- Log every blocklisted command in mission mode even though it isn't prompted, so
  the mission transcript is reviewable after the fact.

None of these require a human in the loop for ordinary work.

---

## 5. Tool registry has no single source of truth — MEDIUM

The set of available tools is assembled from at least four separate places:

| Where | What it adds | When |
|---|---|---|
| `FnMap` literal | ~18 base tools | always |
| `FnMap["memory"]` | `memory` | `MemoryEnabled` |
| `RegisterMissionTools()` | 4 mission tools | `--mission` / `--mission-tools` |
| `removeBrowserTools()` | *removes* `browser` | Playwright off |
| `checkWindowTools()` | *removes* 3 window tools | `xdotool`/`maim` missing |
| `mcp.RegisterToolHandlers()` | `mcp_<server>_<tool>` | any MCP server |

To answer "what can the agent do right now?" you have to read all of the above and
apply conditional logic. There is no function that enumerates the live set.

This is a real operational cost: I have to reason about *this* machine's state
(is Playwright installed? are `xdotool` and `maim` present?) to predict my own
capabilities. I cannot simply be told.

**Wish:** an explicit accessor, e.g.

```go
func AvailableTools() []string  // sorted live set
func ToolSchemas() []models.Tool // what we advertise to the LLM
```

Then a `help` tool that lists them would be genuinely self-describing, and the
`BaseTools` / `MissionBaseTools` bookkeeping could be asserted in a test. Today
there is no test that catches "we advertise a tool whose handler is missing".

**Related, and a concrete symptom of this:** `help` does not list tools. It lists
the *subcommand vocabulary* of the `bash` tool:

```
Available commands:
  help <cmd>     - show help for a command (use: help memory, help git, etc.)
  # File operations
  ls [path]       - list files in directory
  ...
  # Git (read-only)
  git <cmd>       - git commands (status, log, diff, show, branch, etc.)
  ...
  # Window (requires xdotool + maim)
  window              - list available windows
```

Those are not separate tools. They are dispatched inside `runCmd`:

```go
// tools/tools.go:458
case "mkdir", "ls", "cat", "stat", "pwd", "cd", "cp", "mv", "rm", "sed",
     "grep", "head", "tail", "wc", "sort", "uniq", "echo", "printf", "time",
     "go", "find", "file", "git", "magick", "which":
    return executeCommand(args)
default:
    return []byte("[error] command not allowed. Run 'help' tool to see available commands.")
```

So the `help` output reads as a tool list but is a subcommand list. An agent that
takes it at face value and calls a "tool" named `ls` gets told to run `help` —
having just been given the documentation. This is a real trap, and it is exactly
the confusion an `AvailableTools()` accessor would resolve.

Note also the help text advertises `window` / `capture` / `capture_and_view`
unconditionally, even though those tools are deleted from `FnMap` when
`xdotool`/`maim` are missing. So `help` is stale in both directions: it lists
things that aren't tools, and doesn't mention the ones that are.

**Wish:** split the two concerns — `help` (or a new `tools` tool) should list the
live `FnMap` set with one line each, and the `bash` subcommand vocabulary should be
documented as *part of the bash tool's description* or under a separate
`help bash`.

---

## 6. `bash` subcommand router is a growing `switch` — MEDIUM

```go
// tools/tools.go:424
switch subcmd {
case "help": ...
case "memory": ...
// ... more
}
```

`runCmd` is a general dispatcher that fronts several subsystems. Every new shell-ish
verb is another `case`. Meanwhile the browser verbs are a *second*, parallel router
(`runBrowserCommand`) that `browserCmd` wraps.

**Wish:** a `map[string]FnHandler` keyed on subcommand, populated at init, so adding
a verb is a one-line registration rather than a switch arm in the middle of a
200-line function. This is also the natural place to hang per-verb metadata
(help text, arg spec, whether it needs a confirmation).

---

## 7. File tools have no read-before-write guard — MEDIUM

`edit` requires `old_text` to appear exactly once, which is a good
safety property — ambiguous matches are rejected rather than guessed. But the
other mutation tools have no equivalent:

- `write` — unconditional overwrite
- `insert_at` — unconditional insert
- `file_edit` — replaces a line range outright

So a stale `edit` fails loudly (good), while a stale `write` succeeds silently
and clobbers whatever a human or another process put there. Given this is a tool
an autonomous agent holds, and `git` is the only backstop, an optional
`expect_sha` / `expect_mtime` argument would be cheap and would close the
optimistic-concurrency gap.

---

## 8. `read` silently truncates at 2000 lines — MEDIUM

```go
// tools/edit.go
limit := 2000
if limStr := args["limit"]; limStr != "" { ... }
...
result := strings.Join(lines[startIdx:endIdx], "\n")
remaining := totalLines - endIdx
if remaining > 0 {
    result += fmt.Sprintf("\n[%d more lines in file. Use offset=%d to continue.]", remaining, endIdx+1)
}
```

This is good — it tells me how to continue. But the default of 2000 lines is
applied *silently* when `limit` is omitted, so a large file comes back looking
complete-ish. I only notice because of the trailer. A large file that happens
to be exactly ≤2000 lines and a truncated one are hard to tell apart at a glance.

**Wish:** always state the effective limit in the response header, not only when
truncation occurs.

---

## 9. Redundant file-mutation tools — LOW

There are four ways to change file content:

- `write` — full overwrite
- `edit` — exact string replace, must be unique
- `file_edit` — replace line range `[start_line, end_line]`
- `insert_at` — insert before line N

`edit` and `file_edit` overlap heavily. In practice I reach for `edit` (unique-match
gives it a safety property) or `file_edit` (line ranges are stable for generated
files). Four near-synonyms means the choice is arbitrary and gets re-litigated
every session. I don't think this needs fixing urgently — but two of the four
could probably merge, and a note in each description about *when to prefer it*
would remove most of the hesitation.

---

## 10. No tool-result size limit — LOW

Tool output goes straight into the chat context. `read` is bounded (see #8), but:

- `bash` output is unbounded
- `read_url` / `websearch` are unbounded
- `view_img` / `screenshot` return base64

A single `cat` of a large file, or a screenshot, can blow a meaningful fraction of
the context window with one tool call. In mission mode this interacts badly with
context compaction (which triggers at 90% saturation) — a few fat tool results
can push an otherwise-healthy conversation into summarization.

**Wish:** a per-tool output cap with a clear truncation notice, in the same spirit
as `read`'s trailer. Silent context exhaustion is a much worse failure mode than
a message saying "output truncated, re-run with a filter".

---

## 11. `browser`'s `args` is one opaque string — LOW

`browser` is better schema'd than I first assumed — it has a real `action`
property and a `Required` list:

```go
// tools/tools.go:1428
Required: []string{"action"},
Properties: map[string]models.ToolArgProps{
    "action": {Type: "string", Description: "Browser action: start, stop, ..."},
    "args":   {Type: "string", Description: "Arguments for the action (e.g., URL for go, selector for click, etc.)"},
},
```

So `action` is properly typed. The weak part is `args`, which is a single
free-text string that gets split at runtime by `strings.Fields` (see #1). For
`go` the arg is a URL, which survives whitespace splitting fine. For `fill` and
`click` it is positional, so it is exactly the case where the opaque string
hurts — the model cannot express a multi-word value.

`help` has the same shape: a single optional `command` string, split with
`strings.Fields` at `tools.go:952`.

**Wish:** split `args` into typed per-action properties (`selector`, `text`,
`url`, `path`) so the multi-word cases are expressible, rather than relying on a
quoting convention the description doesn't state.

---

## 12. `help` output is a string, not a resource — LOW

`getHelp()` returns formatted help text as a plain string, which lands in the
conversation and burns context. With `help` now able to list the live tool set
(see #5), it becomes a genuinely useful self-description primitive — a few lines
that let an agent bootstrap from cold.

Worth keeping small and structured so it can be called once at session start
without much cost.

---

## 13. Window tools are advertised but never registered as tools — MEDIUM

`checkWindowTools()` gates window support on `xdotool` + `maim`, and the removal
path is thorough:

```go
// tools/tools.go:1221
func removeWindowToolsFromBaseTools() {
    windowToolNames := map[string]bool{
        "list_windows": true, "capture_window": true, "capture_window_and_view": true,
    }
    // ...filters BaseTools, and:
    delete(FnMap, "list_windows")
    delete(FnMap, "capture_window")
    delete(FnMap, "capture_window_and_view")
}
```

But the thing it removes was never added. Searching the whole repo, those three
names appear **only** inside that removal function — they are never keys in the
`FnMap` literal (which ends at `bash`, `browser`, `summarize_chat`,
`create_issue`) and never entries in `BaseTools`.

The functionality itself does work, but only through the `bash` subcommand router:

```go
// tools/tools.go:432
case "window", "windows":            return listWindows(args)
case "capture", "screenshot":        return captureWindow(args)
case "capture_and_view", "screenshot_and_view": return captureWindowAndView(args)
```

So the actual situation is:

- the tool names are `window` / `capture` / `capture_and_view` (bash subcommands),
- the removal code is written against `list_windows` / `capture_window` /
  `capture_window_and_view` (tool names that don't exist),
- therefore `removeWindowToolsFromBaseTools()` is a **no-op**, and the availability
  check it depends on (`WindowToolsAvailable`, properly computed from `exec.LookPath`)
  never actually gates anything.

Net effect: on a machine without `xdotool`/`maim`, the subcommands are still
advertised by `help` and still routed, and they fail at call time rather than being
absent. The graceful degradation described in the feature map doesn't happen for
these.

This is exactly the class of bug the `AvailableTools()` accessor in #5 would have
caught, plus a test asserting the removal path removes something real.

**Wish:** either register them as real tools with matching names (and then the
existing removal logic starts working), or delete the dead removal function and
gate the subcommands on `WindowToolsAvailable` at the router. Right now the code
reads as if window tools are properly self-disabling, and they aren't.

---

## What I'd prioritise

If I had to rank by "how much this hurts a real session":

1. **#3** (empty write) — the one that can destroy work, and the smallest fix
2. **#2** (error convention) — the one that silently corrupts mission failure tracking
3. **#1** (browser quoting) — the one that makes a documented action impossible
4. **#4** (danger guard in mission mode) — deliberate, but the blast radius deserves
   an explicit decision rather than an inherited `if !IsMissionMode()`
5. **#5** (tool introspection) — the one that costs the most over a long session

#1 and #3 are each a couple of lines. #2 is a signature change across every handler.
#4 needs a product decision before any code. #5 is slightly more structural but would
pay for itself quickly.

---

## Non-complaints

Things that are working well, so they don't get "fixed":

- **`edit`'s unique-match requirement.** Rejecting ambiguous matches instead of
  guessing is the right call and I rely on it.
- **`resolvePath` root-escape checks** (`tools/fs.go:56`). Clean, applied
  consistently, and `FSAllowOutOfRoot` is an explicit opt-in rather than a default.
- **Tool self-disabling.** Playwright correctly removes the `browser` tool when
  disabled (`removeBrowserTools()` does filter both `BaseTools` and `FnMap`, and
  `browser` genuinely is in the `FnMap` literal). Memory is likewise added only
  when `MemoryEnabled`. Good pattern — the window tools just haven't been wired
  into it yet (#13).
- **`read`'s continuation trailer.** Small thing, but it saves a round-trip.
- **Native function calling.** Keeping the loop in-process rather than shelling
  out to an external harness is the right architecture, and the two tool-call
  dialects (OpenAI `tool_calls` vs llama.cpp `__tool_call__`) are handled
  separately rather than tangled together.
