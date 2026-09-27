package tools

import (
	"fmt"
	"gf-lt/models"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Command policy.
//
// This replaces a single prefix-matched blocklist that was disabled wholesale in
// mission mode. That shape had three problems:
//
//   - it conflated "needs a human's judgement" with "must never happen", so the
//     only way to run unattended was to switch the whole thing off;
//   - it matched the raw command string, so `echo hi && rm -rf /` and a single
//     leading space both walked straight past it;
//   - it deadlocked in CLI mode, where nothing drains the confirmation channel.
//
// So there are now two lists, matched against the *parsed* command, and the
// check lives in the tool rather than at the call site so there is exactly one of
// them:
//
//	veto   - irreversible and/or outside the workspace. Refused in every mode,
//	         including mission. A veto needs no human, so unattended operation
//	         loses nothing by honouring it.
//	confirm- needs judgement. Asked only when a human is actually present; with
//	         nobody to ask, the default is deny.

const (
	// ConfirmTimeout bounds how long we wait for a human who has already been
	// shown the prompt. A consumer that dies mid-prompt must not wedge the
	// process; the safe answer is no.
	ConfirmTimeout = 2 * time.Minute
)

// ConfirmChan carries confirmation requests to the UI. Buffered(1) so the send
// side can be non-blocking; a full channel means the UI is already showing a
// prompt, which is itself an answer of "not right now".
var ConfirmChan = make(chan ConfirmRequest, 1)

// confirmConsumer is set once a UI is listening. It is checked before we enqueue
// so that a mode with no consumer (CLI, mission) fails fast and denies, rather
// than parking a goroutine on a channel nobody will read.
var confirmConsumer atomic.Bool

type ConfirmRequest struct {
	ToolName string
	Command  string
	ToolArgs map[string]string
	Result   chan<- bool
}

// RegisterConfirmConsumer marks a UI as listening on ConfirmChan. Called by the
// TUI when it starts its confirmation loop.
func RegisterConfirmConsumer() { confirmConsumer.Store(true) }

// UnregisterConfirmConsumer marks the UI as gone.
func UnregisterConfirmConsumer() { confirmConsumer.Store(false) }

// HasConfirmConsumer reports whether a human can currently be asked.
func HasConfirmConsumer() bool { return confirmConsumer.Load() }

// RequestConfirmation asks a human. It never blocks indefinitely: with no
// consumer it denies immediately, and even with one it gives up after
// ConfirmTimeout. Default-deny throughout - the failure mode of a missing or
// unresponsive UI should be "did not run", not "ran anyway".
func RequestConfirmation(req ConfirmRequest) bool {
	if !confirmConsumer.Load() {
		return false
	}
	resultCh := make(chan bool, 1)
	select {
	case ConfirmChan <- ConfirmRequest{
		ToolName: req.ToolName,
		Command:  req.Command,
		ToolArgs: req.ToolArgs,
		Result:   resultCh,
	}:
	default:
		// Buffer full: a prompt is already pending. Deny rather than queue.
		return false
	}
	select {
	case approved := <-resultCh:
		return approved
	case <-time.After(ConfirmTimeout):
		return false
	}
}

// systemPaths are directories that no amount of legitimate workspace work
// justifies writing to.
var systemPaths = []string{
	"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/proc", "/root",
	"/sbin", "/sys", "/usr", "/var",
}

func isSystemPath(p string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(p)
	if clean == "/" {
		return true
	}
	for _, sys := range systemPaths {
		if clean == sys || strings.HasPrefix(clean, sys+"/") {
			return true
		}
	}
	return false
}

// escapesWorkspace reports whether an absolute path is outside the fs root. Only
// meaningful when the root is enforced (FSAllowOutOfRoot off).
func escapesWorkspace(p string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	if cfg == nil || cfg.FilePickerDir == "" {
		return false
	}
	if cfg.FSAllowOutOfRoot {
		return false
	}
	root, err := filepath.Abs(cfg.FilePickerDir)
	if err != nil {
		return false
	}
	root = filepath.Clean(root)
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	return abs != root && !strings.HasPrefix(abs, root+string(os.PathSeparator))
}

func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n {
				return true
			}
		}
	}
	return false
}

// flagSetHas reports whether any of args[0:upto] is one of names, which is how
// combined short flags are handled: "-rf" contains "r" and "f", "-R" does not.
func flagSetHas(args []string, names ...string) bool {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		flags := strings.TrimLeft(a, "-")
		for _, r := range flags {
			for _, n := range names {
				if string(r) == n {
					return true
				}
			}
		}
	}
	return false
}

// EnforceCommandPolicy is the single entry point. It is called from the bash
// tool before dispatch, so it covers every route to the shell - the modern
// /v1/chat path, the legacy /completion path, and the bash subcommand router -
// with one implementation instead of a check duplicated at each call site.
//
// Returns a *models.ToolError (models.CodeDenied) if the command must not run, nil otherwise.
func EnforceCommandPolicy(toolName, command string) error {
	if toolName != "bash" || strings.TrimSpace(command) == "" {
		return nil
	}
	for _, argv := range policyArgv(command) {
		if msg, vetoed := vetoCommand(argv); vetoed {
			if logger != nil {
				logger.Warn("command vetoed", "command", strings.Join(argv, " "), "reason", msg)
			}
			return &models.ToolError{
				Code: models.CodeDenied,
				Msg:  msg,
				Hint: "this is permanently refused in every mode, including mission. " +
					"If the task genuinely requires it, it has to be done by a person.",
			}
		}
	}
	// Confirmation only where a human exists. In mission mode this is skipped
	// entirely and the confirm list is advisory - which is the pre-existing
	// behaviour, and the reason the veto list above exists.
	for _, argv := range policyArgv(command) {
		if label, dangerous := confirmCommand(argv); dangerous {
			approved := RequestConfirmation(ConfirmRequest{
				ToolName: toolName,
				Command:  command,
				ToolArgs: map[string]string{"command": command},
			})
			if approved {
				return nil
			}
			reason := "a human did not approve this command"
			if !HasConfirmConsumer() {
				reason = fmt.Sprintf("%s, and this mode has no interactive user to ask "+
					"(CLI and mission run unattended)", label)
			}
			if logger != nil {
				logger.Info("dangerous command denied", "command", strings.Join(argv, " "), "label", label)
			}
			return &models.ToolError{Code: models.CodeDenied, Msg: reason}
		}
	}
	return nil
}

// policyArgv flattens a command line into the argument vectors the policy rules
// are written against, unwrapping shell indirection along the way.
//
// Without this, tier 2's passthrough would be a one-line bypass: the model would
// only ever have to write `bash -c "rm -rf /"` instead of `rm -rf /`, and the
// rules - which are written over parsed argv - would see a program called "bash"
// and shrug. Same for xargs, env, eval, nohup, timeout, and friends, which all
// take a command as their arguments.
func policyArgv(command string) [][]string {
	var out [][]string
	var walk func(cmd string, depth int)
	walk = func(cmd string, depth int) {
		// Bounded so a self-referential construct cannot spin forever.
		if depth > 4 {
			return
		}
		for _, seg := range ParseChain(cmd) {
			argv := tokenize(seg.Raw)
			if len(argv) == 0 {
				continue
			}
			if inner, ok := unwrapIndirection(argv); ok {
				walk(inner, depth+1)
				continue
			}
			out = append(out, argv)
		}
	}
	walk(command, 0)
	return out
}

// unwrapIndirection returns the nested command line when argv is a wrapper whose
// job is to run something else: a shell, or a command runner taking a command
// plus arguments.
func unwrapIndirection(argv []string) (string, bool) {
	name := filepath.Base(argv[0])
	rest := argv[1:]

	switch name {
	case "sh", "bash", "zsh", "dash", "ksh", "fish", "busybox":
		// sh -c '<script>'; busybox sh -c '<script>'
		if strings.HasPrefix(name, "busybox") {
			rest = argv[2:]
		}
		for i, a := range rest {
			if a == "-c" || a == "-lc" || a == "-ic" {
				if i+1 < len(rest) {
					return rest[i+1], true
				}
				return "", false
			}
		}
		// `bash script.sh` / `bash "rm -rf /"` - a positional argument to a
		// shell is a script, so it is still a command line to judge.
		var positional []string
		for _, a := range rest {
			if !strings.HasPrefix(a, "-") {
				positional = append(positional, a)
			}
		}
		if len(positional) > 0 {
			return strings.Join(positional, " "), true
		}
	case "eval":
		return strings.Join(rest, " "), true
	case "xargs":
		// xargs [-opts] <cmd> [args...] - the command and its arguments are
		// what actually runs, so keep them all.
		for i, a := range rest {
			if strings.HasPrefix(a, "-") {
				continue
			}
			return strings.Join(rest[i:], " "), true
		}
	case "timeout":
		// timeout [-opts] <duration> <cmd> [args...] - the duration is not part
		// of the command, and treating it as one would hide the real argv.
		i := 0
		for i < len(rest) {
			a := rest[i]
			if strings.HasPrefix(a, "-") {
				i++
				continue
			}
			break
		}
		if i < len(rest) {
			return strings.Join(rest[i+1:], " "), true
		}
	case "env", "nohup", "nice", "ionice", "setsid", "stdbuf", "command", "doas", "time":
		// These take options, then VAR=value pairs, then the real command.
		i := 0
		for i < len(rest) {
			a := rest[i]
			if strings.HasPrefix(a, "-") {
				// Options that consume the next argument.
				if a == "-u" || a == "--unset" {
					i += 2
					continue
				}
				i++
				continue
			}
			if strings.Contains(a, "=") && !strings.Contains(a, "/") {
				i++
				continue
			}
			return strings.Join(rest[i:], " "), true
		}
	}
	return "", false
}

// vetoCommand decides whether a single parsed segment must never run.
//
// Everything here is irreversible, or reaches outside the workspace, or takes
// the machine away from the user. None of it is something an unattended solver
// legitimately needs, which is exactly why it can be refused without asking.
func vetoCommand(argv []string) (string, bool) {
	name := filepath.Base(argv[0])
	args := argv[1:]

	// mkfs.* and friends are variants of one command.
	if strings.HasPrefix(name, "mkfs") {
		return fmt.Sprintf("refusing to run %s: it destroys a filesystem", name), true
	}

	switch name {
	case "rm":
		// Recursive deletion of the workspace root or of anything above it.
		recursive := flagSetHas(args, "r", "R") || hasFlag(args, "--recursive")
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				continue
			}
			target := a
			if strings.HasPrefix(target, "$HOME") {
				target = os.Getenv("HOME")
			}
			if isSystemPath(target) || escapesWorkspace(target) {
				return fmt.Sprintf("refusing to remove %s: it is outside the workspace", target), true
			}
			if recursive && (target == "." || target == ".." || target == "/") {
				return fmt.Sprintf("refusing to recursively remove %s", target), true
			}
		}
	case "mke2fs", "mkswap", "fdisk", "sfdisk", "parted", "wipefs",
		"shutdown", "poweroff", "reboot", "halt", "init", "telinit":
		return fmt.Sprintf("refusing to run %s: it takes the machine or its filesystem down", name), true
	case "dd":
		// Only the device/system case is a veto. `dd if=a.img of=b.img` inside
		// the workspace is an ordinary file operation and belongs on the
		// confirm list, not the veto list.
		for _, a := range args {
			if !strings.HasPrefix(a, "of=") {
				continue
			}
			target := strings.TrimPrefix(a, "of=")
			if isSystemPath(target) || strings.HasPrefix(target, "/dev/") {
				return fmt.Sprintf("refusing to dd directly onto %s: it is a device or system path", target), true
			}
		}
	case "shred":
		for _, a := range args {
			if !strings.HasPrefix(a, "-") && (isSystemPath(a) || escapesWorkspace(a)) {
				return fmt.Sprintf("refusing to shred %s: it is outside the workspace", a), true
			}
		}
	case "chmod", "chown", "chgrp":
		recursive := flagSetHas(args, "r", "R") || hasFlag(args, "--recursive")
		if recursive {
			for _, a := range args {
				if strings.HasPrefix(a, "-") {
					continue
				}
				if isSystemPath(a) {
					return fmt.Sprintf("refusing to recursively change ownership/permissions of %s", a), true
				}
			}
		}
	case "git":
		return vetoGit(argv)
	}
	return "", false
}

// vetoGit handles the git subcommands that cannot be undone for us.
func vetoGit(argv []string) (string, bool) {
	// Skip git's own global flags so `git -C /tmp push` and
	// `git --git-dir=/x push` are matched like the plain form.
	i := 1
	for i < len(argv) {
		a := argv[i]
		if !strings.HasPrefix(a, "-") {
			break
		}
		if a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree" || a == "--namespace" {
			i += 2
			continue
		}
		i++
	}
	if i >= len(argv) {
		return "", false
	}
	sub := argv[i]
	rest := argv[i+1:]

	switch sub {
	case "push":
		if hasFlag(rest, "--force", "--force-with-lease", "--delete") || flagSetHas(rest, "f") {
			return "refusing a forced or deleting git push: it rewrites history others may depend on", true
		}
	case "clean":
		if hasFlag(rest, "--force", "-f") {
			return "refusing git clean -f: it permanently deletes untracked files", true
		}
	}
	return "", false
}

// confirmCommand is the "ask a human" list. These are destructive or
// far-reaching, but a person might reasonably want them, so where a person is
// present we ask.
//
// The previous implementation was a list of string prefixes on the raw command,
// which is why it is written here over parsed argv: combined flags, `git -C`,
// and per-segment matching all fall out of parsing instead of needing their own
// special case.
func confirmCommand(argv []string) (string, bool) {
	name := filepath.Base(argv[0])
	args := argv[1:]

	// mkfs.* and friends are variants of one command.
	if strings.HasPrefix(name, "mkfs") {
		return fmt.Sprintf("refusing to run %s: it destroys a filesystem", name), true
	}

	if strings.HasPrefix(name, "mkfs") {
		return name + " (filesystem format)", true
	}

	switch name {
	case "rm":
		return "rm (delete file)", true
	case "sudo", "doas", "su":
		return name + " (privilege escalation)", true
	case "dd":
		return "dd (raw device write)", true
	case "truncate":
		return "truncate (resize a file to zero)", true
	case "mke2fs", "fdisk", "sfdisk", "parted":
		return name + " (filesystem/partition operation)", true
	case "shutdown", "poweroff", "reboot", "halt":
		return name + " (system power state)", true
	case "iptables", "ufw", "nft":
		return name + " (firewall manipulation)", true
	case "chmod", "chown", "chgrp":
		if flagSetHas(args, "r", "R") || hasFlag(args, "--recursive") {
			return name + " (recursive permission/ownership change)", true
		}
	case "find":
		if hasFlag(args, "-delete", "-exec", "-execdir") {
			return "find with -delete/-exec (mass filesystem operation)", true
		}
	case "git":
		i := 1
		for i < len(argv) {
			a := argv[i]
			if !strings.HasPrefix(a, "-") {
				break
			}
			if a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree" {
				i += 2
				continue
			}
			i++
		}
		if i >= len(argv) {
			return "", false
		}
		switch argv[i] {
		case "push":
			return "git push (remote change)", true
		case "reset":
			if hasFlag(argv[i+1:], "--hard") {
				return "git reset --hard (discards uncommitted work)", true
			}
		case "clean":
			return "git clean (deletes untracked files)", true
		case "checkout", "restore":
			rest := argv[i+1:]
			if hasFlag(rest, "--force", "-f") || (argv[i] == "checkout" && hasFlag(rest, "--discard-changes")) {
				return "git " + argv[i] + " with force (discards local changes)", true
			}
		}
	}
	return "", false
}
