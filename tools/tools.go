package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"gf-lt/agent"
	"gf-lt/config"
	"gf-lt/models"
	"gf-lt/storage"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gf-lt/rag"

	"github.com/GrailFinder/searchagent/searcher"
)

var (
	RpDefenitionSysMsg = `
For this roleplay immersion is at most importance.
Every character thinks and acts based on their personality and setting of the roleplay.
Meta discussions outside of roleplay is allowed if clearly labeled as out of character, for example: (ooc: {msg}) or <ooc>{msg}</ooc>.
`
	ToolSysMsgChat = `<tool_guide>
If you choose to call a function ONLY do a tool call in openai format with NO suffix.
You may put optional reasoning inside <think></think> but it must come BEFORE the tool call. Never put anything after the tool call.
Examples of common operations:
- Read file: use the read tool (path, offset, limit), or read main.go 40 20 via bash
- Shell: any command works - ls, cat, grep, find, git, go, sed, pipes, redirects
- View image: use read tool on image files (png, jpg, gif, webp). Example: read screenshot.png
- Create a file: use write. Example: write counter.json "{\"count\": 5}"
- Edit file (small, quotable): use edit_text to replace an exact unique text match. Example: edit_text main.go "return nil" "return err"
- Edit file (multi-line): use edit_lines to replace a line range. Example: edit_lines llm.go 182 186 "new content"
- Count lines: run "wc -l /path/file.txt"
- Find files: run "find . -name '*.go'"
- Search content: run "grep -r pattern /dir"

Tool errors: a failed tool returns a first line of the form
  [tool_error: <code>] <what went wrong>
optionally followed by a "hint:" line saying what to try instead. codes:
  invalid_args  a malformed call - fix the arguments
  not_found     the named file/tool/subject does not exist
  models.Conflict      the call was valid but the state is wrong (ambiguous match, non-zero exit)
  models.Denied        policy refused it; retrying unchanged will not help
  models.Unavailable   an optional dependency is missing
  models.Internal      we broke; the details are in the log, not here
Anything after the error header is real output (for example a compiler's
messages) and is usually what you need to act on. Do not repeat a call that
returned the same error twice - change your approach instead.
</tool_guide>
`
	ToolSysMsg = `Tools are enabled. While making a tool call avoid writing anything else.
You may put optional reasoning inside <think></think> but it must come BEFORE the tool call. Never put anything after the closing </tool_call>.
Your current tools:
<tools>
[
{
"name":"bash",
"args": ["command"],
"when_to_use": "Main tool for file operations, shell commands, memory, and git. Use help for all commands. Examples: ls -la, help, mkdir -p foo/bar, cat file.txt, git status, memory store foo bar, grep pattern file, grep -r pattern dir, find . -name '*.go', cd /path, pwd, head -n 100 file, tail -n 10 file, wc -l file, sort file, uniq file, sed 's/old/new/g' file, edit_lines llm.go 182 186 \"new content\", echo text, go build ./..., stat file, cp src dst, mv src dst, rm file"
},
{
"name":"browser",
"args": ["action", "args"],
"when_to_use": "Playwright browser automation. Actions: start, stop, running, go <url>, click <selector>, fill <selector> <text>, text [selector], html [selector], screenshot [path], screenshot_and_view, wait <selector>, drag <x1> <y1> <x2> <y2>. Example: browser start, browser go https://example.com, browser click #submit-button"
},
{
"name":"view_img",
"args": ["file"],
"when_to_use": "View an image file and get it displayed in the conversation for visual analysis. Supports: png, jpg, jpeg, gif, webp, svg. Example: view_img /path/to/image.png or view_img image.png"
},
{
"name":"websearch",
"args": ["query", "limit"],
"when_to_use": "search the web for information"
},
{
"name":"rag_search",
"args": ["query", "limit"],
"when_to_use": "search local document database"
},
{
"name":"read_url",
"args": ["url"],
"when_to_use": "get content from a webpage"
},
{
"name":"read_url_raw",
"args": ["url"],
"when_to_use": "get raw content from a webpage"
},
{
"name":"read",
"args": ["path", "offset", "limit"],
"when_to_use": "Read file content with optional line range. offset=start line (default 1), limit=max lines (default 2000). Example: read main.go 40 20"
},
{
"name":"write",
"args": ["file_path", "content"],
"when_to_use": "Write or overwrite a file with full content. Creates parent directories. Use for new files or full file replacements. Example: write config.toml \"port=8080\""
},
{
"name":"edit",
"args": ["file_path", "old_text", "new_text"],
"when_to_use": "Replace an exact text match in a file with new text. The old_text must be unique in the file. Use for targeted edits without line numbers. Example: edit main.go \"return nil\" \"return err\""
}
]
</tools>
To make a function call return a json object within __tool_call__ tags;
<example_request>
__tool_call__
{
"name":"bash",
"args": {"command": "ls -la /home"}
}
__tool_call__
</example_request>
<example_request>
__tool_call__
{
"name":"view_img",
"args": {"file": "screenshot.png"}
}
__tool_call__
</example_request>
Tool call is addressed to the tool agent, avoid sending more info than the tool call itself, while making a call.
When done right, tool call will be delivered to the tool agent. tool agent will respond with the results of the call.
<example_response>
tool:
total 1234
drwxr-xr-x   2 user user  4096 Jan  1 12:00 .
</example_response>
After that you are free to respond to the user.
`
	webSearchSysPrompt = `Summarize the web search results, extracting key information and presenting a concise answer. Provide sources and URLs where relevant.`
	ragSearchSysPrompt = `Synthesize the document search results, extracting key information and presenting a concise answer. Provide sources and document IDs where relevant.`
	readURLSysPrompt   = `Extract and summarize the content from the webpage. Provide key information, main points, and any relevant details.`
	summarySysPrompt   = `Please provide a concise summary of the following conversation. Focus on key points, decisions, and actions. Provide only the summary, no additional commentary.`
)

var WebSearcher searcher.WebSurfer

var (
	xdotoolPath string
	maimPath    string
	// windowToolsAvailable mirrors Tools.WindowToolsAvailable for the
	// package-level registration helpers, which run before/without a Tools value.
	windowToolsAvailable bool
	logger               *slog.Logger
	cfg                  *config.Config
	getTokenFunc         func() string
)

type Tools struct {
	cfg                  *config.Config
	logger               *slog.Logger
	store                storage.FullRepo
	WindowToolsAvailable bool
	// getTokenFunc         func() string
	webAgentClient     *agent.AgentClient
	webAgentClientOnce sync.Once
	webSearchAgent     agent.AgenterB
}

func (t *Tools) initAgentsB() {
	t.GetWebAgentClient()
	t.webSearchAgent = agent.NewWebAgentB(t.webAgentClient, webSearchSysPrompt)
	agent.RegisterB("rag_search", agent.NewWebAgentB(t.webAgentClient, ragSearchSysPrompt))
	// Register websearch agent
	agent.RegisterB("websearch", agent.NewWebAgentB(t.webAgentClient, webSearchSysPrompt))
	// Register read_url agent
	agent.RegisterB("read_url", agent.NewWebAgentB(t.webAgentClient, readURLSysPrompt))
	// Register summarize_chat agent
	agent.RegisterB("summarize_chat", agent.NewWebAgentB(t.webAgentClient, summarySysPrompt))
}

func InitTools(initCfg *config.Config, log *slog.Logger, store storage.FullRepo) *Tools {
	logger = log
	cfg = initCfg
	if initCfg.PlaywrightEnabled {
		if err := CheckPlaywright(); err != nil {
			// slow, need a faster check if playwright install
			if err := InstallPW(); err != nil {
				logger.Error("failed to install playwright", "error", err)
				os.Exit(1)
				return nil
			}
			if err := CheckPlaywright(); err != nil {
				logger.Error("failed to run playwright", "error", err)
				os.Exit(1)
				return nil
			}
		}
	} else {
		removeBrowserTools()
	}
	// Initialize fs root directory
	SetFSRoot(cfg.FilePickerDir)
	sa, err := searcher.NewWebSurfer(searcher.SearcherTypeScraper, "")
	if err != nil {
		if logger != nil {
			logger.Warn("search agent models.Unavailable; web_search tool disabled", "error", err)
		}
		WebSearcher = nil
	} else {
		WebSearcher = sa
	}
	if err := rag.Init(cfg, logger, store); err != nil {
		logger.Warn("failed to init rag; rag_search tool will not be available", "error", err)
	}
	t := &Tools{
		cfg:    cfg,
		logger: logger,
		store:  store,
	}
	t.checkWindowTools()
	registerWindowTools()
	t.initAgentsB()
	if initCfg.MemoryEnabled {
		SetMemoryStore(&memoryAdapter{store: store, cfg: cfg}, cfg.AssistantRole)
		FnMap["memory"] = memoryTool
		BaseTools = append(BaseTools, memoryToolDef)
	}
	return t
}

func (t *Tools) checkWindowTools() {
	xdotoolPath, _ = exec.LookPath("xdotool")
	maimPath, _ = exec.LookPath("maim")
	t.WindowToolsAvailable = xdotoolPath != "" && maimPath != ""
	windowToolsAvailable = t.WindowToolsAvailable
	if t.WindowToolsAvailable {
		t.logger.Info("window tools available: xdotool and maim found")
	} else {
		if xdotoolPath == "" {
			t.logger.Warn("xdotool not found, window listing tools will not be available")
		}
		if maimPath == "" {
			t.logger.Warn("maim not found, window capture tools will not be available")
		}
	}
}

func SetTokenFunc(fn func() string) {
	getTokenFunc = fn
}

func (t *Tools) GetWebAgentClient() *agent.AgentClient {
	t.webAgentClientOnce.Do(func() {
		getToken := func() string {
			if getTokenFunc != nil {
				return getTokenFunc()
			}
			return ""
		}
		t.webAgentClient = agent.NewAgentClient(t.cfg, t.logger, getToken)
	})
	return t.webAgentClient
}

// func RegisterPlaywrightTools() {
// 	removePlaywrightToolsFromBaseTools()
// 	if cfg != nil && cfg.PlaywrightEnabled {
// 		// Playwright tools are registered here
// 	}
// }

func websearch(args map[string]string) ([]byte, error) {
	// make http request return bytes
	query, ok := args["query"]
	if !ok || query == "" {
		msg := "query not provided to web_search tool"
		logger.Error(msg)
		return []byte(msg), nil
	}
	limitS, ok := args["limit"]
	if !ok || limitS == "" {
		limitS = "3"
	}
	limit, err := strconv.Atoi(limitS)
	if err != nil || limit == 0 {
		logger.Warn("websearch limit; passed bad value; setting to default (3)",
			"limit_arg", limitS, "error", err)
		limit = 3
	}
	resp, err := WebSearcher.Search(context.Background(), query, limit)
	if err != nil {
		msg := "search tool failed; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	data, err := json.Marshal(resp)
	if err != nil {
		msg := "failed to marshal search result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	return data, nil
}

// rag search (searches local document database)
func ragsearch(args map[string]string) ([]byte, error) {
	query, ok := args["query"]
	if !ok || query == "" {
		msg := "query not provided to rag_search tool"
		logger.Error(msg)
		return []byte(msg), nil
	}
	limitS, ok := args["limit"]
	if !ok || limitS == "" {
		limitS = "10"
	}
	limit, err := strconv.Atoi(limitS)
	if err != nil || limit == 0 {
		logger.Warn("ragsearch limit; passed bad value; setting to default (3)",
			"limit_arg", limitS, "error", err)
		limit = 10
	}
	ragInstance := rag.GetInstance()
	if ragInstance == nil {
		msg := "rag not initialized; rag_search tool is not available"
		logger.Error(msg)
		return []byte(msg), nil
	}
	results, err := ragInstance.Search(query, limit)
	if err != nil {
		msg := "rag search failed; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	data, err := json.Marshal(results)
	if err != nil {
		msg := "failed to marshal rag search result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	return data, nil
}

// web search raw (returns raw data without processing)
func websearchRaw(args map[string]string) ([]byte, error) {
	// make http request return bytes
	query, ok := args["query"]
	if !ok || query == "" {
		msg := "query not provided to websearch_raw tool"
		logger.Error(msg)
		return []byte(msg), nil
	}
	limitS, ok := args["limit"]
	if !ok || limitS == "" {
		limitS = "3"
	}
	limit, err := strconv.Atoi(limitS)
	if err != nil || limit == 0 {
		logger.Warn("websearch_raw limit; passed bad value; setting to default (3)",
			"limit_arg", limitS, "error", err)
		limit = 3
	}
	resp, err := WebSearcher.Search(context.Background(), query, limit)
	if err != nil {
		msg := "search tool failed; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	// Return raw response without any processing
	return []byte(fmt.Sprintf("%+v", resp)), nil
}

// retrieves url content (text)
func readURL(args map[string]string) ([]byte, error) {
	// make http request return bytes
	link, ok := args["url"]
	if !ok || link == "" {
		msg := "link not provided to read_url tool"
		logger.Error(msg)
		return []byte(msg), nil
	}
	resp, err := WebSearcher.RetrieveFromLink(context.Background(), link)
	if err != nil {
		msg := "search tool failed; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	data, err := json.Marshal(resp)
	if err != nil {
		msg := "failed to marshal search result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	return data, nil
}

// retrieves url content raw (returns raw content without processing)
func readURLRaw(args map[string]string) ([]byte, error) {
	// make http request return bytes
	link, ok := args["url"]
	if !ok || link == "" {
		msg := "link not provided to read_url_raw tool"
		logger.Error(msg)
		return []byte(msg), nil
	}
	resp, err := WebSearcher.RetrieveFromLink(context.Background(), link)
	if err != nil {
		msg := "search tool failed; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	// Return raw response without any processing
	return []byte(fmt.Sprintf("%+v", resp)), nil
}

// runCmd is the bash tool: policy check, then two tiers. See router.go.
func runCmd(args map[string]string) ([]byte, error) {
	commandStr := strings.TrimSpace(args["command"])
	if commandStr == "" {
		return nil, models.InvalidArgs("command is required", "")
	}

	// Command policy runs here, once, before any normalisation and before any
	// dispatch - so the modern /v1/chat path, the legacy /completion path and
	// the subcommand router are all covered by the same check, and so that
	// nothing we do afterwards can hide a command from it.
	//
	// Order matters. Stripping a leading "bash " first would turn
	// `bash -c "rm -rf /"` into `-c "rm -rf /"`, which the policy can no longer
	// recognise as a shell wrapper, and the command would run.
	if err := EnforceCommandPolicy("bash", commandStr); err != nil {
		return nil, err
	}

	commandStr = stripBashPrefix(commandStr)
	parts := tokenize(commandStr)
	if len(parts) == 0 {
		return nil, models.InvalidArgs("empty command", "")
	}

	// Tier 1: a Go capability.
	if v, ok := lookupVerb(parts[0]); ok {
		return v.run(parts[1:], args)
	}

	// Tier 2: the shell. Default-allow, bounded by the veto list above.
	return executeCommand(commandStr)
}

// stripBashPrefix accepts the redundant "bash " some models prefix a bash tool
// call with. `bash ls` means `ls`; `bash -c "..."` does not - that is a nested
// shell and must be left intact (the policy has already judged it).
func stripBashPrefix(commandStr string) string {
	rest, ok := strings.CutPrefix(commandStr, "bash ")
	if !ok {
		return strings.TrimSpace(strings.Trim(commandStr, "\""))
	}
	trimmed := strings.TrimSpace(rest)
	if trimmed == "-c" || strings.HasPrefix(trimmed, "-c ") || strings.HasPrefix(trimmed, "--") {
		return commandStr
	}
	return strings.TrimSpace(strings.Trim(trimmed, "\""))
}

// toBytes adapts a string-returning builtin to the handler signature.
func toBytes(s string, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// browserCmd handles top-level browser tool calls
func browserCmd(args map[string]string) ([]byte, error) {
	action := args["action"]
	argsStr := args["args"]
	if action == "" && argsStr != "" {
		// allow the shorthand `browser "go https://example.com"`
		action = argsStr
		argsStr = ""
	}
	// Parse args string into slice. tokenize() is quote-aware, so
	// `fill "#search" "hello world"` keeps the multi-word value intact. Using
	// strings.Fields here made that impossible to express.
	var browserArgs []string
	if argsStr != "" {
		browserArgs = tokenize(argsStr)
	}
	if action == "" {
		return []byte(`usage: browser <action> [args...]
Actions:
  start              - start browser
  stop               - stop browser
  running            - check if running
  go <url>           - navigate to URL
  click <selector>   - click element
  fill <selector> <text> - fill input
  text [selector]    - extract text
  html [selector]    - get HTML
  screenshot [path]  - take screenshot
  screenshot_and_view - take and view screenshot
  wait <selector>    - wait for element
  drag <from> <to>   - drag element`), nil
	}
	// Prepend action to args for runBrowserCommand
	fullArgs := append([]string{action}, browserArgs...)
	return runBrowserCommand(fullArgs, args)
}

// runBrowserCommand routes browser subcommands to Playwright handlers
func runBrowserCommand(args []string, originalArgs map[string]string) ([]byte, error) {
	if len(args) == 0 {
		return []byte(`usage: browser <action> [args...]
Actions:
  start              - start browser
  stop               - stop browser
  running            - check if browser is running
  go <url>           - navigate to URL
  click <selector>   - click element
  fill <selector> <text> - fill input
  text [selector]    - extract text
  html [selector]    - get HTML
  dom                - get DOM
  screenshot [path]  - take screenshot
  screenshot_and_view - take and view screenshot
  wait <selector>    - wait for element
  drag <from> <to>   - drag element`), nil
	}
	action := args[0]
	rest := args[1:]
	switch action {
	case "start":
		return pwStart(originalArgs)
	case "stop":
		return pwStop(originalArgs)
	case "running":
		return pwIsRunning(originalArgs)
	case "go", "navigate", "open":
		// browser go <url>
		url := ""
		if len(rest) > 0 {
			url = rest[0]
		}
		if url == "" {
			return []byte("usage: browser go <url>"), nil
		}
		return pwNavigate(map[string]string{"url": url})
	case "click":
		// browser click <selector> [index]
		selector := ""
		index := "0"
		if len(rest) > 0 {
			selector = rest[0]
		}
		if len(rest) > 1 {
			index = rest[1]
		}
		if selector == "" {
			return []byte("usage: browser click <selector> [index]"), nil
		}
		return pwClick(map[string]string{"selector": selector, "index": index})
	case "fill":
		// browser fill <selector> <text>
		if len(rest) < 2 {
			return []byte("usage: browser fill <selector> <text>"), nil
		}
		return pwFill(map[string]string{"selector": rest[0], "text": strings.Join(rest[1:], " ")})
	case "text":
		// browser text [selector]
		selector := ""
		if len(rest) > 0 {
			selector = rest[0]
		}
		return pwExtractText(map[string]string{"selector": selector})
	case "html":
		// browser html [selector]
		selector := ""
		if len(rest) > 0 {
			selector = rest[0]
		}
		return pwGetHTML(map[string]string{"selector": selector})
	case "dom":
		return pwGetDOM(originalArgs)
	case "screenshot":
		// browser screenshot [path]
		path := ""
		if len(rest) > 0 {
			path = rest[0]
		}
		return pwScreenshot(map[string]string{"path": path})
	case "screenshot_and_view":
		// browser screenshot_and_view [path]
		path := ""
		if len(rest) > 0 {
			path = rest[0]
		}
		return pwScreenshotAndView(map[string]string{"path": path})
	case "wait":
		// browser wait <selector>
		selector := ""
		if len(rest) > 0 {
			selector = rest[0]
		}
		if selector == "" {
			return []byte("usage: browser wait <selector>"), nil
		}
		return pwWaitForSelector(map[string]string{"selector": selector})
	case "drag":
		// browser drag <x1> <y1> <x2> <y2> OR browser drag <from_selector> <to_selector>
		if len(rest) < 4 && len(rest) < 2 {
			return []byte("usage: browser drag <x1> <y1> <x2> <y2> OR browser drag <from_selector> <to_selector>"), nil
		}
		// Check if first arg is a number (coordinates) or selector
		_, err := strconv.Atoi(rest[0])
		_, err2 := strconv.ParseFloat(rest[0], 64)
		if err == nil || err2 == nil {
			// Coordinates: browser drag 100 200 300 400
			if len(rest) < 4 {
				return []byte("usage: browser drag <x1> <y1> <x2> <y2>"), nil
			}
			return pwDrag(map[string]string{
				"x1": rest[0], "y1": rest[1],
				"x2": rest[2], "y2": rest[3],
			})
		}
		// Selectors: browser drag #item #container
		// pwDrag needs coordinates, so we need to get element positions first
		// This requires a different approach - use JavaScript to get centers
		return pwDragBySelector(map[string]string{
			"fromSelector": rest[0],
			"toSelector":   rest[1],
		})
	case "help":
		return []byte(`browser <action> [args]
Playwright browser automation.
Actions:
  start              - start browser
  stop               - stop browser
  running            - check if browser is running
  go <url>          - navigate to URL
  click <selector>  - click element (use index for multiple: click #btn 1)
  fill <sel> <text> - fill input field
  text [selector]   - extract text (from element or whole page)
  html [selector]   - get HTML (from element or whole page)
  screenshot [path] - take screenshot
  screenshot_and_view - take and view screenshot
  wait <selector>   - wait for element to appear
  drag <x1> <y1> <x2> <y2> - drag by coordinates
  drag <sel1> <sel2> - drag by selectors (center points)
Examples:
  browser start
  browser go https://example.com
  browser click #submit-button
  browser fill #search-input hello
  browser text
  browser screenshot
  browser screenshot_and_view
  browser drag 100 200 300 400
  browser drag #item1 #container2`), nil
	default:
		return nil, models.InvalidArgs("unknown browser action: "+action, "use: start, stop, running, go, click, fill, text, html, screenshot, screenshot_and_view, wait, drag")
	}
}

// getHelp returns help text.
//
// The first section lists the live *tool* set, and the second the tier-1 verb
// table, both generated from the registries that actually dispatch. The previous
// version listed the bash subcommand vocabulary in prose while reading like a
// tool list - an agent that took it at face value and called a "tool" named `ls`
// was told to run `help`, having just been given the documentation.
func getHelp(args []string) string {
	if len(args) == 0 {
		return ToolsHelp() + "\n\n" + verbHelpText()
	}
	if usage, ok := verbHelpFor(args[0]); ok {
		return usage
	}
	switch args[0] {
	case "tools", "tool":
		return ToolsHelp()
	case "bash", "commands", "shell":
		return verbHelpText()
	}
	return fmt.Sprintf("%q is not a tier-1 verb; it is passed to the shell.\n\n%s",
		args[0], verbHelpText())
}

func executeCommand(commandStr string) ([]byte, error) {
	if strings.TrimSpace(commandStr) == "" {
		return nil, models.InvalidArgs("command is required", "")
	}
	if shellPassthroughEnabled() {
		return runInShell(commandStr)
	}
	out, err := ExecChain(commandStr)
	return []byte(out), err
}

// shellPassthroughEnabled reports whether tier 2 goes to a real shell. Default on.
func shellPassthroughEnabled() bool {
	return cfg == nil || !cfg.DisableShellPassthrough
}

// runInShell hands the command to the user's shell verbatim.
//
// This replaced gf-lt's own chain parser, which handled pipes and redirects but
// did not expand globs, variables or command substitution - and reported success
// while doing so. `echo *.go` came back as the literal string "*.go", `echo $HOME`
// as the literal "$HOME", both with err == nil. A silent wrong answer is worse
// than a failure, because nothing downstream can tell it apart from a right one.
//
// Non-interactive on purpose (no rc files, no aliases, no job control) so
// behaviour does not depend on the user's dotfiles. Bounded by MaxToolResultBytes
// in CallToolWithAgent, and by the veto list in dangerous.go, which unwraps
// shell indirection before matching.
func runInShell(commandStr string) ([]byte, error) {
	shell, shellArgs := resolveShell()
	if shell == "" {
		return nil, models.Unavailable("no shell found on PATH",
			"install bash or sh, or set DisableShellPassthrough=true to use the built-in executor")
	}
	cmd := exec.Command(shell, append(append([]string{}, shellArgs...), commandStr)...)
	cmd.Dir = cfg.FilePickerDir
	cmd.Env = append(os.Environ(), "GF_LT_SHELL=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, &models.ToolError{
			Code: models.CodeConflict,
			Msg:  fmt.Sprintf("%s exited with a non-zero status", filepath.Base(shell)),
			Hint: "the output above says what failed; fix that and re-run",
			Err:  err,
		}
	}
	return output, nil
}

// resolveShell picks the shell to run, and whether it needs a -c flag.
func resolveShell() (string, []string) {
	for _, candidate := range []struct {
		shell string
		args  []string
	}{
		{"bash", []string{"-c"}},
		{"sh", []string{"-c"}},
	} {
		if path, err := exec.LookPath(candidate.shell); err == nil {
			return path, candidate.args
		}
	}
	return "", nil
}

// // handleCdCommand handles the cd command to update FilePickerDir
// func handleCdCommand(args []string) []byte {
// 	var targetDir string
// 	if len(args) == 0 {
// 		// cd with no args goes to home directory
// 		homeDir, err := os.UserHomeDir()
// 		if err != nil {
// 			msg := "cd: cannot determine home directory: " + err.Error()
// 			logger.Error(msg)
// 			return []byte(msg)
// 		}
// 		targetDir = homeDir
// 	} else {
// 		targetDir = args[0]
// 	}
// 	// Resolve relative paths against current FilePickerDir
// 	if !filepath.IsAbs(targetDir) {
// 		targetDir = filepath.Join(cfg.FilePickerDir, targetDir)
// 	}
// 	// Verify the directory exists
// 	info, err := os.Stat(targetDir)
// 	if err != nil {
// 		msg := "cd: " + targetDir + ": " + err.Error()
// 		logger.Error(msg)
// 		return []byte(msg)
// 	}
// 	if !info.IsDir() {
// 		msg := "cd: " + targetDir + ": not a directory"
// 		logger.Error(msg)
// 		return []byte(msg)
// 	}
// 	// Update FilePickerDir
// 	absDir, err := filepath.Abs(targetDir)
// 	if err != nil {
// 		msg := "cd: failed to resolve path: " + err.Error()
// 		logger.Error(msg)
// 		return []byte(msg)
// 	}
// 	cfg.FilePickerDir = absDir
// 	msg := "FilePickerDir changed to: " + absDir
// 	return []byte(msg)
// }

// Helper functions for command execution
func viewImgTool(args map[string]string) ([]byte, error) {
	file, ok := args["file"]
	if !ok || file == "" {
		return nil, models.InvalidArgs("file is required", "")
	}
	return toBytes(FsViewImg([]string{file}, ""))
}

func helpTool(args map[string]string) ([]byte, error) {
	command, ok := args["command"]
	if !ok || strings.TrimSpace(command) == "" {
		return []byte(getHelp(nil)), nil
	}
	return []byte(getHelp(tokenize(command))), nil
}

// func summarizeChat(args map[string]string) []byte {
// 	if len(chatBody.Messages) == 0 {
// 		return []byte("No chat history to summarize.")
// 	}
// 	// Format chat history for the agent
// 	chatText := chatToText(chatBody.Messages, true) // include system and tool messages
// 	return []byte(chatText)
// }

func windowIDToHex(decimalID string) string {
	id, err := strconv.ParseInt(decimalID, 10, 64)
	if err != nil {
		return decimalID
	}
	return fmt.Sprintf("0x%x", id)
}

func listWindows(args map[string]string) ([]byte, error) {
	cmd := exec.Command(xdotoolPath, "search", "--name", ".")
	output, err := cmd.Output()
	if err != nil {
		msg := "failed to list windows: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	windowIDs := strings.Fields(string(output))
	windows := make(map[string]string)
	for _, id := range windowIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		nameCmd := exec.Command(xdotoolPath, "getwindowname", id)
		nameOutput, err := nameCmd.Output()
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(nameOutput))
		windows[id] = name
	}
	data, err := json.Marshal(windows)
	if err != nil {
		msg := "failed to marshal window list: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	return data, nil
}

func captureWindow(args map[string]string) ([]byte, error) {
	window, ok := args["window"]
	if !ok || window == "" {
		return []byte("window parameter required (window ID or name)"), nil
	}
	var windowID string
	if _, err := strconv.Atoi(window); err == nil {
		windowID = window
	} else {
		cmd := exec.Command(xdotoolPath, "search", "--name", window)
		output, err := cmd.Output()
		if err != nil || len(strings.Fields(string(output))) == 0 {
			return []byte("window not found: " + window), nil
		}
		windowID = strings.Fields(string(output))[0]
	}
	nameCmd := exec.Command(xdotoolPath, "getwindowname", windowID)
	nameOutput, _ := nameCmd.Output()
	windowName := strings.TrimSpace(string(nameOutput))
	windowName = regexp.MustCompile(`[^a-zA-Z]+`).ReplaceAllString(windowName, "")
	if windowName == "" {
		windowName = "window"
	}
	timestamp := time.Now().Unix()
	filename := fmt.Sprintf("/tmp/%s_%d.jpg", windowName, timestamp)
	cmd := exec.Command(maimPath, "-i", windowIDToHex(windowID), filename)
	if err := cmd.Run(); err != nil {
		msg := "failed to capture window: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	return []byte("screenshot saved: " + filename), nil
}

func captureWindowAndView(args map[string]string) ([]byte, error) {
	window, ok := args["window"]
	if !ok || window == "" {
		return []byte("window parameter required (window ID or name)"), nil
	}
	var windowID string
	if _, err := strconv.Atoi(window); err == nil {
		windowID = window
	} else {
		cmd := exec.Command(xdotoolPath, "search", "--name", window)
		output, err := cmd.Output()
		if err != nil || len(strings.Fields(string(output))) == 0 {
			return []byte("window not found: " + window), nil
		}
		windowID = strings.Fields(string(output))[0]
	}
	nameCmd := exec.Command(xdotoolPath, "getwindowname", windowID)
	nameOutput, _ := nameCmd.Output()
	windowName := strings.TrimSpace(string(nameOutput))
	windowName = regexp.MustCompile(`[^a-zA-Z]+`).ReplaceAllString(windowName, "")
	if windowName == "" {
		windowName = "window"
	}
	timestamp := time.Now().Unix()
	filename := fmt.Sprintf("/tmp/%s_%d.jpg", windowName, timestamp)
	captureCmd := exec.Command(maimPath, "-i", windowIDToHex(windowID), filename)
	if err := captureCmd.Run(); err != nil {
		msg := "failed to capture window: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	dataURL, err := models.CreateImageURLFromPath(filename)
	if err != nil {
		msg := "failed to create image URL: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	result := models.MultimodalToolResp{
		Type: "multimodal_content",
		Parts: []map[string]string{
			{"type": "text", "text": "Screenshot saved: " + filename},
			{"type": "image_url", "url": dataURL},
		},
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		msg := "failed to marshal result: " + err.Error()
		logger.Error(msg)
		return []byte(msg), nil
	}
	return jsonResult, nil
}

// FnHandler is a tool implementation. It returns the model-facing output and,
// independently, an error. The two are orthogonal on purpose: a tool that applied
// 3 of 4 edits still has useful output, so the output is not discarded when the
// error is non-nil. See errors.go for how the error reaches the model.
type FnHandler func(args map[string]string) ([]byte, error)

// FS Command Handlers - Unix-style file operations
// Convert map[string]string to []string for tools package
func argsToSlice(args map[string]string) []string {
	var result []string
	// Common positional args in order
	for _, key := range []string{"path", "src", "dst", "dir", "file"} {
		if v, ok := args[key]; ok && v != "" {
			result = append(result, v)
		}
	}
	return result
}

func memoryTool(args map[string]string) ([]byte, error) {
	action := args["action"]
	topic := args["topic"]
	data := args["data"]
	switch action {
	case "store":
		return toBytes(FsMemory([]string{"store", topic, data}, ""))
	case "get":
		return toBytes(FsMemory([]string{"get", topic}, ""))
	case "list", "topics":
		return toBytes(FsMemory([]string{action}, ""))
	case "forget", "delete":
		return toBytes(FsMemory([]string{action, topic}, ""))
	default:
		return nil, models.InvalidArgs("unknown memory action: "+action, "use: store, get, list, topics, forget, delete")
	}
}

var memoryToolDef = models.Tool{
	Type: "function",
	Function: models.ToolFunc{
		Name:        "memory",
		Description: "Persistent memory storage. Store and retrieve information by topic.",
		Parameters: models.ToolFuncParams{
			Type:     "object",
			Required: []string{"action"},
			Properties: map[string]models.ToolArgProps{
				"action": {Type: "string", Description: "store, get, list, topics, forget, delete"},
				"topic":  {Type: "string", Description: "topic name (required for store/get/forget)"},
				"data":   {Type: "string", Description: "data content (required for store)"},
			},
		},
	},
}

type memoryAdapter struct {
	store storage.Memories
	cfg   *config.Config
}

func (m *memoryAdapter) Memorise(agent, topic, data string) (string, error) {
	mem := &models.Memory{
		Agent:     agent,
		Topic:     topic,
		Mind:      data,
		UpdatedAt: time.Now(),
		CreatedAt: time.Now(),
	}
	result, err := m.store.Memorise(mem)
	if err != nil {
		return "", err
	}
	return result.Topic, nil
}

func (m *memoryAdapter) Recall(agent, topic string) (string, error) {
	return m.store.Recall(agent, topic)
}

func (m *memoryAdapter) RecallTopics(agent string) ([]string, error) {
	return m.store.RecallTopics(agent)
}

func (m *memoryAdapter) Forget(agent, topic string) error {
	return m.store.Forget(agent, topic)
}

// FnMap maps a tool name to its handler. Populated in init() rather than as a
// package-level composite literal: getHelp -> ToolsHelp -> AvailableTools ->
// FnMap would otherwise be an initialization cycle.
var FnMap = map[string]FnHandler{}

func init() {
	FnMap["rag_search"] = ragsearch
	FnMap["websearch"] = websearch
	FnMap["websearch_raw"] = websearchRaw
	FnMap["read_url"] = readURL
	FnMap["read_url_raw"] = readURLRaw
	FnMap["view_img"] = viewImgTool
	FnMap["help"] = helpTool
	FnMap["edit_lines"] = func(args map[string]string) ([]byte, error) { return toBytes(FsEditLines(args)) }
	FnMap["read"] = func(args map[string]string) ([]byte, error) { return toBytes(FsRead(args)) }
	FnMap["write"] = func(args map[string]string) ([]byte, error) { return toBytes(FsWrite(args)) }
	FnMap["edit_text"] = func(args map[string]string) ([]byte, error) { return toBytes(FsEditText(args)) }
	// Unified run command
	FnMap["bash"] = runCmd
	// Browser tool - routes to runBrowserCommand
	FnMap["browser"] = browserCmd
	FnMap["summarize_chat"] = summarizeChat
	// Issue management - always available
	FnMap["create_issue"] = createIssueTool
}

func removeBrowserTools() {
	delete(FnMap, "browser")
	var filtered []models.Tool
	for _, tool := range BaseTools {
		if tool.Function.Name != "browser" {
			filtered = append(filtered, tool)
		}
	}
	BaseTools = filtered
}

// registerWindowTools registers the window tools as *real tools* (not just bash
// subcommands) and removes them again when xdotool/maim are missing.
//
// This previously could not work: removeWindowToolsFromBaseTools() deleted
// FnMap["list_windows"] etc., but those names were never in FnMap, so the whole
// availability gate was a no-op. The names now match.
func registerWindowTools() {
	if !windowToolsAvailable {
		removeWindowToolsFromBaseTools()
		return
	}
	if _, ok := FnMap["list_windows"]; !ok {
		FnMap["list_windows"] = listWindows
		FnMap["capture_window"] = captureWindow
		BaseTools = append(BaseTools, windowListToolDef, captureWindowToolDef)
	}
}

// RegisterWindowTools is called once the model's vision capability is known.
// capture_window_and_view returns an image, so it is only advertised to a model
// that can actually look at one.
func RegisterWindowTools(modelHasVision bool) {
	registerWindowTools()
	if !windowToolsAvailable || !modelHasVision {
		return
	}
	if _, ok := FnMap["capture_window_and_view"]; ok {
		return
	}
	FnMap["capture_window_and_view"] = captureWindowAndView
	BaseTools = append(BaseTools, captureWindowAndViewToolDef)
}

func removeWindowToolsFromBaseTools() {
	delete(FnMap, "list_windows")
	delete(FnMap, "capture_window")
	delete(FnMap, "capture_window_and_view")
	var filtered []models.Tool
	for _, tool := range BaseTools {
		switch tool.Function.Name {
		case "list_windows", "capture_window", "capture_window_and_view":
			continue
		}
		filtered = append(filtered, tool)
	}
	BaseTools = filtered
}

func summarizeChat(args map[string]string) ([]byte, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return []byte("error: failed to marshal arguments"), nil
	}
	return data, nil
}

// for pw agentA
// var browserAgentSysPrompt = `You are an autonomous browser automation agent. Your goal is to complete the user's task by intelligently using browser automation

// Important: The browser may already be running from a previous task! Always check pw_is_running first before starting a new browser.

// Always provide clear feedback about what you're doing and what you found.`

func CallToolWithAgent(name string, args map[string]string) ([]byte, error) {
	f, ok := FnMap[name]
	if !ok {
		err := &models.ToolError{
			Code: models.CodeUnknownTool,
			Msg:  fmt.Sprintf("no such tool: %q", name),
			Hint: fmt.Sprintf("call the help tool to list the %d available tools", len(FnMap)),
		}
		return models.RenderToolResult(name, nil, err), err
	}
	out, err := f(args)
	if a := agent.Get(name); a != nil {
		out = a.Process(args, out)
	}
	rendered := models.RenderToolResult(name, out, err)
	if err != nil && logger != nil {
		logger.Debug("tool call failed", "tool", name, "code", models.ErrorCodeOf(err), "error", err)
	}
	return TruncateToolResult(name, rendered), err
}

// openai style def
var BaseTools = []models.Tool{
	// rag_search
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "rag_search",
			Description: "Search local document database given query, limit of sources (default 3). Performs query refinement, semantic search, reranking, and synthesis.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"query", "limit"},
				Properties: map[string]models.ToolArgProps{
					"query": models.ToolArgProps{
						Type:        "string",
						Description: "search query",
					},
					"limit": models.ToolArgProps{
						Type:        "string",
						Description: "limit of the document results",
					},
				},
			},
		},
	},
	// websearch
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "websearch",
			Description: "Search web given query, limit of sources (default 3).",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"query", "limit"},
				Properties: map[string]models.ToolArgProps{
					"query": models.ToolArgProps{
						Type:        "string",
						Description: "search query",
					},
					"limit": models.ToolArgProps{
						Type:        "string",
						Description: "limit of the website results",
					},
				},
			},
		},
	},
	// read_url
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "read_url",
			Description: "Retrieves text content of given link, providing clean summary without html,css and other web elements.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"url"},
				Properties: map[string]models.ToolArgProps{
					"url": models.ToolArgProps{
						Type:        "string",
						Description: "link to the webpage to read text from",
					},
				},
			},
		},
	},
	// view_img
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "view_img",
			Description: "View an image file and get it displayed in the conversation for visual analysis. Supports: png, jpg, jpeg, gif, webp, svg.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"file"},
				Properties: map[string]models.ToolArgProps{
					"file": models.ToolArgProps{
						Type:        "string",
						Description: "path to the image file to view",
					},
				},
			},
		},
	},
	// websearch_raw
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "websearch_raw",
			Description: "Search web given query, returning raw data as is without processing. Use when you need the raw response data instead of a clean summary.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"query", "limit"},
				Properties: map[string]models.ToolArgProps{
					"query": models.ToolArgProps{
						Type:        "string",
						Description: "search query",
					},
					"limit": models.ToolArgProps{
						Type:        "string",
						Description: "limit of the website results",
					},
				},
			},
		},
	},
	// read_url_raw
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "read_url_raw",
			Description: "Retrieves raw content of given link without processing. Use when you need the raw response data instead of a clean summary.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"url"},
				Properties: map[string]models.ToolArgProps{
					"url": models.ToolArgProps{
						Type:        "string",
						Description: "link to the webpage to read text from",
					},
				},
			},
		},
	},
	// help
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "help",
			Description: "List what you can actually do right now. With no arguments: the live tool set, then the Go capabilities the bash tool handles directly. Pass command=\"tools\" for just the tool list, or command=\"<verb>\" for one verb (e.g. \"help read\"). Anything not listed is passed to the shell.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"command": models.ToolArgProps{
						Type:        "string",
						Description: "optional: get help for specific command (e.g., 'help memory')",
					},
				},
			},
		},
	},
	// bash - unified command
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "bash",
			Description: "Run a shell command, or a built-in verb. Most things are shell: ls, cat, grep, find, git, go, sed, pipes and redirects all work as typed. Built-in verbs with no shell equivalent: read <file> [offset] [limit], write <file> <content> (add overwrite=true to replace an existing non-empty file), edit_text <file> <old> <new>, edit_lines <file> <start> [end] <content>, view_img <file>, memory <store|get|list|forget>, browser <action>, window, capture. Use help to list the current set.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"command"},
				Properties: map[string]models.ToolArgProps{
					"command": models.ToolArgProps{
						Type:        "string",
						Description: "command to execute: either a shell command (ls, cat f, grep -r x ., git status, go build ./...) or a built-in verb (read, write, edit, memory, browser, help).",
					},
				},
			},
		},
	},
	// browser - Playwright browser automation
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "browser",
			Description: "Playwright browser automation. Actions: start (launch browser), stop (close browser), running (check if browser is running), go <url> (navigate), click <selector> [index] (click element), fill <selector> <text> (type into input), text [selector] (extract text), html [selector] (get HTML), screenshot [path] (take screenshot), screenshot_and_view (take and view), wait <selector> (wait for element), drag <x1> <y1> <x2> <y2> (drag by coords) or drag <sel1> <sel2> (drag by selectors)",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"action"},
				Properties: map[string]models.ToolArgProps{
					"action": models.ToolArgProps{
						Type:        "string",
						Description: "Browser action: start, stop, running, go, click, fill, text, html, screenshot, screenshot_and_view, wait, drag",
					},
					"args": models.ToolArgProps{
						Type:        "string",
						Description: "Arguments for the action (e.g., URL for go, selector for click, etc.)",
					},
				},
			},
		},
	},
	// edit_lines - replace a line range
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "edit_lines",
			Description: "Replace an inclusive range of lines with new content. Choose this when the change is multi-line, or when you can only locate it by line number - quoting a long block back exactly is where edit_text fails. For a one-line change you can quote exactly, edit_text is cheaper: it skips the read-and-count. A start_line one past the last line appends.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"file_path", "start_line", "new_content"},
				Properties: map[string]models.ToolArgProps{
					"file_path": models.ToolArgProps{
						Type:        "string",
						Description: "path to the file to edit",
					},
					"start_line": models.ToolArgProps{
						Type:        "integer",
						Description: "1-indexed line where replacement starts. Past the end of the file, it appends. To delete lines without replacing, pass new_content as an empty string.",
					},
					"end_line": models.ToolArgProps{
						Type:        "integer",
						Description: "1-indexed line where replacement ends (inclusive). Defaults to start_line. Must be >= start_line.",
					},
					"new_content": models.ToolArgProps{
						Type:        "string",
						Description: "replacement content (use \\n for newlines). Pass empty string to delete the line range.",
					},
				},
			},
		},
	},
	// read - read a file with offset/limit
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "read",
			Description: "Read the content of a file. For text files: supports offset and limit for line ranges (default: line 1, up to 2000 lines). The response starts with a header naming the line range and whether it was truncated. For image files (png, jpg, gif, webp, svg): returns the image for visual analysis.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]models.ToolArgProps{
					"path": models.ToolArgProps{
						Type:        "string",
						Description: "path to the file to read",
					},
					"offset": models.ToolArgProps{
						Type:        "string",
						Description: "line number to start reading from (1-indexed, default 1)",
					},
					"limit": models.ToolArgProps{
						Type:        "string",
						Description: "maximum number of lines to read (default 2000)",
					},
				},
			},
		},
	},
	// write - overwrite a file with new content
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "write",
			Description: "Write a file. Use this to create a new file, or to replace one wholesale - not to change part of a file, which is what edit_text and edit_lines are for. Overwriting a non-empty file requires overwrite=true, and the result always says what was there before (\"was 412 lines, now 3\"), so check it. Empty content is rejected unless truncate=true is also set; to delete a file use the bash tool: rm <path>.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"file_path", "content"},
				Properties: map[string]models.ToolArgProps{
					"file_path": models.ToolArgProps{
						Type:        "string",
						Description: "path to the file to write",
					},
					"content": models.ToolArgProps{
						Type:        "string",
						Description: "full content to write to the file",
					},
					"truncate": models.ToolArgProps{
						Type:        "string",
						Description: "opt-in: set to \"true\" to allow empty content (clears the file). Any other non-empty content is rejected.",
					},
				},
			},
		},
	},
	// edit - replace exact text in a file
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "edit_text",
			Description: "Replace an exact run of text with new text. Choose this for a small, distinctive change you can quote exactly - a flag, a comment, a line of markdown - and you do not know or want the line number. old_text must appear exactly once, or the call is refused rather than guessed; add surrounding context to disambiguate. For anything multi-line, use edit_lines instead.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"file_path", "old_text", "new_text"},
				Properties: map[string]models.ToolArgProps{
					"file_path": models.ToolArgProps{
						Type:        "string",
						Description: "path to the file to edit",
					},
					"old_text": models.ToolArgProps{
						Type:        "string",
						Description: "exact text to find in the file (must be unique)",
					},
					"new_text": models.ToolArgProps{
						Type:        "string",
						Description: "replacement text",
					},
				},
			},
		},
	},
	// create_issue - issue management (always available)
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "create_issue",
			Description: "Create a new issue and save it to the issues directory. The issue is saved with status 'open' and can be solved later with --mission. Example: create_issue title='Fix login timeout' description='The login page times out after 30s' acceptance_criteria='[\"Timeout should be 60s\", \"Tests pass\"]' context_files='[\"auth/login.go\"]'",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"title"},
				Properties: map[string]models.ToolArgProps{
					"title":               {Type: "string", Description: "Issue title (required)"},
					"description":         {Type: "string", Description: "Issue description (optional, defaults to 'No description provided')"},
					"id":                  {Type: "string", Description: "Issue ID (optional, auto-generated if not provided)"},
					"project_path":        {Type: "string", Description: "Path to the project repository (optional, defaults to current working directory)"},
					"acceptance_criteria": {Type: "string", Description: "JSON array of acceptance criteria strings, e.g. '[\"criteria 1\", \"criteria 2\"]' (optional)"},
					"context_files":       {Type: "string", Description: "JSON array of file paths relevant to this issue, e.g. '[\"main.go\", \"main_test.go\"]' (optional)"},
					"labels":              {Type: "string", Description: "Comma-separated labels, e.g. 'bug,validation' (optional)"},
					"priority":            {Type: "string", Description: "Priority level: low, medium, high, critical (optional)"},
					"branch_name":         {Type: "string", Description: "Suggested branch name for this issue (optional)"},
				},
			},
		},
	},
}
