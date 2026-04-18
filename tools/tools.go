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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	taskActive     atomic.Bool
	ToolSysMsgChat = `
If you choose to call a function ONLY reply in the following format with NO suffix:
<tool_call>
<function=example_function_name>
<parameter=example_parameter_1>value_1</parameter>
...
</function>
</tool_call>
When a task is in progress you MUST output either a tool call or task_done. Do not output normal text, explanations, or markdown.
You may put optional reasoning inside <think></think> but it must come BEFORE the tool call. Never put anything after the closing </tool_call>.
If you finished with task or you got stuck and user's input required call task_done.
`
	ToolSysMsg = `Tools are enabled. While making a tool call avoid writing anything else.
You may put optional reasoning inside <think></think> but it must come BEFORE the tool call. Never put anything after the closing </tool_call>.
If you finished with task or you got stuck and user's input required call task_done.
Your current tools:
<tools>
[
{
"name":"run",
"args": ["command"],
"when_to_use": "Main tool for file operations, shell commands, memory, git, and todo. Use run "help" for all commands. Examples: run "ls -la", run "help", run "mkdir -p foo/bar", run "cat file.txt", run "write file.txt content", run "git status", run "memory store foo bar", run "todo create task", run "grep pattern file", run "cd /path", run "pwd", run "find . -name *.txt", run "file image.png", run "head file", run "tail file", run "wc -l file", run "sort file", run "uniq file", run "sed 's/old/new/' file", run "echo text", run "go build ./...", run "time", run "stat file", run "cp src dst", run "mv src dst", run "rm file"
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
}
]
</tools>
To make a function call return a json object within __tool_call__ tags;
<example_request>
__tool_call__
{
"name":"run",
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
	// reminderPrompt     = `Received a message without a tool call while task is in progress. Either call task_done to complete the task or proceed with the intended tool call.`
	ReminderPrompt = `Received a message without a tool call while task is in progress. In case task is done call task_done. Otherwsie only do next intended tool call`
)

var WebSearcher searcher.WebSurfer

var (
	xdotoolPath  string
	maimPath     string
	logger       *slog.Logger
	cfg          *config.Config
	getTokenFunc func() string
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

func InitTools(initCfg *config.Config, logger *slog.Logger, store storage.FullRepo) *Tools {
	logger = logger
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
	}
	// Initialize fs root directory
	SetFSRoot(cfg.FilePickerDir)
	// Initialize memory store
	SetMemoryStore(&memoryAdapter{store: store, cfg: cfg}, cfg.AssistantRole)
	sa, err := searcher.NewWebSurfer(searcher.SearcherTypeScraper, "")
	if err != nil {
		if logger != nil {
			logger.Warn("search agent unavailable; web_search tool disabled", "error", err)
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
	t.initAgentsB()
	return t
}

func (t *Tools) checkWindowTools() {
	xdotoolPath, _ = exec.LookPath("xdotool")
	maimPath, _ = exec.LookPath("maim")
	t.WindowToolsAvailable = xdotoolPath != "" && maimPath != ""
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

func RegisterWindowTools(modelHasVision bool) {
	removeWindowToolsFromBaseTools()
	// Window tools registration happens here if needed
}

// func RegisterPlaywrightTools() {
// 	removePlaywrightToolsFromBaseTools()
// 	if cfg != nil && cfg.PlaywrightEnabled {
// 		// Playwright tools are registered here
// 	}
// }

func websearch(args map[string]string) []byte {
	// make http request return bytes
	query, ok := args["query"]
	if !ok || query == "" {
		msg := "query not provided to web_search tool"
		logger.Error(msg)
		return []byte(msg)
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
		return []byte(msg)
	}
	data, err := json.Marshal(resp)
	if err != nil {
		msg := "failed to marshal search result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	return data
}

// rag search (searches local document database)
func ragsearch(args map[string]string) []byte {
	query, ok := args["query"]
	if !ok || query == "" {
		msg := "query not provided to rag_search tool"
		logger.Error(msg)
		return []byte(msg)
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
		return []byte(msg)
	}
	results, err := ragInstance.Search(query, limit)
	if err != nil {
		msg := "rag search failed; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	data, err := json.Marshal(results)
	if err != nil {
		msg := "failed to marshal rag search result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	return data
}

// web search raw (returns raw data without processing)
func websearchRaw(args map[string]string) []byte {
	// make http request return bytes
	query, ok := args["query"]
	if !ok || query == "" {
		msg := "query not provided to websearch_raw tool"
		logger.Error(msg)
		return []byte(msg)
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
		return []byte(msg)
	}
	// Return raw response without any processing
	return []byte(fmt.Sprintf("%+v", resp))
}

// retrieves url content (text)
func readURL(args map[string]string) []byte {
	// make http request return bytes
	link, ok := args["url"]
	if !ok || link == "" {
		msg := "link not provided to read_url tool"
		logger.Error(msg)
		return []byte(msg)
	}
	resp, err := WebSearcher.RetrieveFromLink(context.Background(), link)
	if err != nil {
		msg := "search tool failed; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	data, err := json.Marshal(resp)
	if err != nil {
		msg := "failed to marshal search result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	return data
}

// retrieves url content raw (returns raw content without processing)
func readURLRaw(args map[string]string) []byte {
	// make http request return bytes
	link, ok := args["url"]
	if !ok || link == "" {
		msg := "link not provided to read_url_raw tool"
		logger.Error(msg)
		return []byte(msg)
	}
	resp, err := WebSearcher.RetrieveFromLink(context.Background(), link)
	if err != nil {
		msg := "search tool failed; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	// Return raw response without any processing
	return []byte(fmt.Sprintf("%+v", resp))
}

// Unified run command - single entry point for shell, memory, and todo
func runCmd(args map[string]string) []byte {
	commandStr := args["command"]
	if commandStr == "" {
		msg := "command not provided to run tool"
		logger.Error(msg)
		return []byte(msg)
	}
	// Parse the command - first word is subcommand
	parts := strings.Fields(commandStr)
	if len(parts) == 0 {
		return []byte("[error] empty command")
	}
	subcmd := parts[0]
	rest := parts[1:]
	// Route to appropriate handler
	switch subcmd {
	case "help":
		// help - show all commands
		// help <cmd> - show help for specific command
		return []byte(getHelp(rest))
	case "memory":
		// memory store <topic> <data> | memory get <topic> | memory list | memory forget <topic>
		return []byte(FsMemory(append([]string{"store"}, rest...), ""))
	case "todo":
		return handleTodoSubcommand(rest, args)
	case "window", "windows":
		// window list - list all windows
		return listWindows(args)
	case "capture", "screenshot":
		// capture <window-name> - capture a window
		return captureWindow(args)
	case "capture_and_view", "screenshot_and_view":
		// capture and view screenshot
		return captureWindowAndView(args)
	case "view_img":
		// view_img <file> - view image for multimodal
		return []byte(FsViewImg(rest, ""))
	case "browser":
		// browser <action> [args...] - Playwright browser automation
		return runBrowserCommand(rest, args)
	case "mkdir", "ls", "cat", "write", "stat", "pwd", "cd", "cp", "mv", "rm", "sed", "grep", "head", "tail", "wc", "sort", "uniq", "echo", "time", "go", "find", "file":
		// File operations and shell commands - use ExecChain which has whitelist
		return executeCommand(args)
	case "git":
		// git has its own whitelist in FsGit
		return []byte(FsGit(rest, ""))
	default:
		// Unknown subcommand - tell user to run help tool
		return []byte("[error] command not allowed. Run 'help' tool to see available commands.")
	}
}

// browserCmd handles top-level browser tool calls
func browserCmd(args map[string]string) []byte {
	action, _ := args["action"]
	argsStr, _ := args["args"]
	// Parse args string into slice (space-separated, respecting quoted strings)
	var browserArgs []string
	if argsStr != "" {
		browserArgs = strings.Fields(argsStr)
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
  drag <from> <to>   - drag element`)
	}
	// Prepend action to args for runBrowserCommand
	fullArgs := append([]string{action}, browserArgs...)
	return runBrowserCommand(fullArgs, args)
}

// runBrowserCommand routes browser subcommands to Playwright handlers
func runBrowserCommand(args []string, originalArgs map[string]string) []byte {
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
  drag <from> <to>   - drag element`)
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
			return []byte("usage: browser go <url>")
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
			return []byte("usage: browser click <selector> [index]")
		}
		return pwClick(map[string]string{"selector": selector, "index": index})
	case "fill":
		// browser fill <selector> <text>
		if len(rest) < 2 {
			return []byte("usage: browser fill <selector> <text>")
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
			return []byte("usage: browser wait <selector>")
		}
		return pwWaitForSelector(map[string]string{"selector": selector})
	case "drag":
		// browser drag <x1> <y1> <x2> <y2> OR browser drag <from_selector> <to_selector>
		if len(rest) < 4 && len(rest) < 2 {
			return []byte("usage: browser drag <x1> <y1> <x2> <y2> OR browser drag <from_selector> <to_selector>")
		}
		// Check if first arg is a number (coordinates) or selector
		_, err := strconv.Atoi(rest[0])
		_, err2 := strconv.ParseFloat(rest[0], 64)
		if err == nil || err2 == nil {
			// Coordinates: browser drag 100 200 300 400
			if len(rest) < 4 {
				return []byte("usage: browser drag <x1> <y1> <x2> <y2>")
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
  browser drag #item1 #container2`)
	default:
		return []byte("unknown browser action: " + action)
	}
}

// getHelp returns help text for commands
func getHelp(args []string) string {
	if len(args) == 0 {
		// General help - show all commands
		return `Available commands:
  help <cmd>     - show help for a command (use: help memory, help git, etc.)
  
  # File operations
  ls [path]       - list files in directory
  cat <file>      - read file content
  view_img <file> - view image file
  write <file>    - write content to file
  stat <file>     - get file info
  rm <file>       - delete file
  cp <src> <dst> - copy file
  mv <src> <dst> - move/rename file
  mkdir [-p] <dir> - create directory (use full path)
  pwd             - print working directory
  cd <dir>        - change directory
  sed 's/old/new/[g]' [file] - text replacement
  
  # Text processing
  echo <args>     - echo back input
  time             - show current time
  grep <pattern>  - filter lines (supports -i, -v, -c)
  head [n]         - show first n lines
  tail [n]        - show last n lines
  wc [-l|-w|-c]   - count lines/words/chars
  sort [-r|-n]    - sort lines
  uniq [-c]       - remove duplicates
  
  # Git (read-only)
  git <cmd>       - git commands (status, log, diff, show, branch, etc.)
  
  # Go
  go <cmd>        - go commands (run, build, test, mod, etc.)
  
  # Memory
  memory store <topic> <data>  - save to memory
  memory get <topic>           - retrieve from memory
  memory list                   - list all topics
  memory forget <topic>         - delete from memory
  
  # Todo
  todo create <task>   - create a todo
  todo read            - list all todos
  todo update <id> <status> - update todo (pending/in_progress/completed)
  todo delete <id>     - delete a todo
  
  # Window (requires xdotool + maim)
  window              - list available windows
  capture <name>    - capture a window screenshot
  capture_and_view <name> - capture and view screenshot

  # System
  <any shell command> - run shell command directly

Use: run "command" to execute.`
	}

	// Specific command help
	cmd := args[0]
	switch cmd {
	case "ls":
		return `ls [directory]
  List files in a directory.
  Examples:
    run "ls"
    run "ls /home/user"
    run "ls -la" (via shell)`
	case "cat":
		return `cat <file>
  Read file content.
  Examples:
    run "cat readme.md"
    run "cat -b image.png" (base64 output)`
	case "view_img":
		return `view_img <image-file>
  View an image file for multimodal analysis.
  Supports: png, jpg, jpeg, gif, webp, svg
  Example:
    run "view_img screenshot.png"`
	case "write":
		return `write <file> [content]
  Write content to a file.
  Examples:
    run "write notes.txt hello world"
    run "write data.json" (with stdin)`
	case "memory":
		return `memory <subcommand> [args]
  Manage memory storage.
  Subcommands:
    store <topic> <data>  - save data to a topic
    get <topic>           - retrieve data from a topic
    list                  - list all topics
    forget <topic>        - delete a topic
  Examples:
    run "memory store foo bar"
    run "memory get foo"
    run "memory list"`
	case "todo":
		return `todo <subcommand> [args]
  Manage todo list.
  Subcommands:
    create <task>      - create a new todo
    read [id]          - list all todos or read specific one
    update <id> <status> - update status (pending/in_progress/completed)
    delete <id>        - delete a todo
  Examples:
    run "todo create fix bug"
    run "todo read"
    run "todo update 1 completed"`
	case "git":
		return `git <subcommand>
  Read-only git commands.
  Allowed: status, log, diff, show, branch, reflog, rev-parse, shortlog, describe, rev-list
  Examples:
    run "git status"
    run "git log --oneline -5"
    run "git diff HEAD~1"`
	case "grep":
		return `grep <pattern> [options]
  Filter lines matching a pattern.
  Options:
    -i  ignore case
    -v  invert match
    -c  count matches
  Example:
    run "grep error" (from stdin)
    run "grep -i warn log.txt"`
	case "cd":
		return `cd <directory>
  Change working directory.
  Example:
    run "cd /tmp"
    run "cd .."`
	case "pwd":
		return `pwd
  Print working directory.
  Example:
    run "pwd"`
	case "mkdir":
		return `mkdir [-p] <directory>
  Create a directory (use full path).
  Options:
    -p, --parents  create parent directories as needed
  Examples:
    run "mkdir /full/path/myfolder"
    run "mkdir -p /full/path/to/nested/folder"`
	case "sed":
		return `sed 's/old/new/[g]' [file]
  Stream editor for text replacement.
  Options:
    -i  in-place editing
    -g  global replacement (replace all)
  Examples:
    run "sed 's/foo/bar/' file.txt"
    run "sed 's/foo/bar/g' file.txt" (global)
    run "sed -i 's/foo/bar/' file.txt" (in-place)
    run "cat file.txt | sed 's/foo/bar/'" (pipe from stdin)`
	case "go":
		return `go <command>
  Go toolchain commands.
  Allowed: run, build, test, mod, get, install, clean, fmt, vet, etc.
  Examples:
    run "go run main.go"
    run "go build ./..."
    run "go test ./..."
    run "go mod tidy"
    run "go get github.com/package"`
	case "window", "windows":
		return `window
  List available windows.
  Requires: xdotool and maim
  Example:
    run "window"`
	case "capture", "screenshot":
		return `capture <window-name-or-id>
  Capture a screenshot of a window.
  Requires: xdotool and maim
  Examples:
    run "capture Firefox"
    run "capture 0x12345678"
    run "capture_and_view Firefox"`
	case "capture_and_view":
		return `capture_and_view <window-name-or-id>
  Capture a window and return for viewing.
  Requires: xdotool and maim
  Examples:
    run "capture_and_view Firefox"`
	default:
		return fmt.Sprintf("No help available for: %s. Use: run \"help\" for all commands.", cmd)
	}
}

// handleTodoSubcommand routes todo subcommands to existing handlers
func handleTodoSubcommand(args []string, originalArgs map[string]string) []byte {
	if len(args) == 0 {
		return []byte("usage: todo create|read|update|delete")
	}
	subcmd := args[0]
	switch subcmd {
	case "create":
		task := strings.Join(args[1:], " ")
		if task == "" {
			task = originalArgs["task"]
		}
		if task == "" {
			return []byte("usage: todo create <task>")
		}
		return todoCreate(map[string]string{"task": task})
	case "read":
		id := ""
		if len(args) > 1 {
			id = args[1]
		}
		return todoRead(map[string]string{"id": id})
	case "update":
		if len(args) < 2 {
			return []byte("usage: todo update <id> <status>")
		}
		return todoUpdate(map[string]string{"id": args[1], "status": args[2]})
	case "delete":
		if len(args) < 2 {
			return []byte("usage: todo delete <id>")
		}
		return todoDelete(map[string]string{"id": args[1]})
	default:
		return []byte("unknown todo subcommand: " + subcmd)
	}
}

// Command Execution Tool with pipe/chaining support
func executeCommand(args map[string]string) []byte {
	commandStr := args["command"]
	if commandStr == "" {
		msg := "command not provided to execute_command tool"
		logger.Error(msg)
		return []byte(msg)
	}
	// Use chain execution for pipe/chaining support
	result := ExecChain(commandStr)
	return []byte(result)
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
// Todo structure
type TodoItem struct {
	ID     string `json:"id"`
	Task   string `json:"task"`
	Status string `json:"status"` // "pending", "in_progress", "completed"
}
type TodoList struct {
	Items []TodoItem `json:"items"`
}

func (t TodoList) ToString() string {
	sb := strings.Builder{}
	for i := range t.Items {
		fmt.Fprintf(&sb, "\n[%s] %s. %s\n", t.Items[i].Status, t.Items[i].ID, t.Items[i].Task)
	}
	return sb.String()
}

// Global todo list storage
var globalTodoList = TodoList{
	Items: []TodoItem{},
}

// Todo Management Tools
func todoCreate(args map[string]string) []byte {
	task, ok := args["task"]
	if !ok || task == "" {
		msg := "task not provided to todo_create tool"
		logger.Error(msg)
		return []byte(msg)
	}
	// Generate simple ID
	id := fmt.Sprintf("todo_%d", len(globalTodoList.Items)+1)
	newItem := TodoItem{
		ID:     id,
		Task:   task,
		Status: "pending",
	}
	globalTodoList.Items = append(globalTodoList.Items, newItem)
	result := map[string]string{
		"message": "todo created successfully",
		"id":      id,
		"task":    task,
		"status":  "pending",
		"todos":   globalTodoList.ToString(),
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		msg := "failed to marshal result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	return jsonResult
}

func todoRead(args map[string]string) []byte {
	// Return all todos if no ID specified
	result := map[string]interface{}{
		"todos": globalTodoList.ToString(),
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		msg := "failed to marshal result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	return jsonResult
}

func todoUpdate(args map[string]string) []byte {
	id, ok := args["id"]
	if !ok || id == "" {
		msg := "id not provided to todo_update tool"
		logger.Error(msg)
		return []byte(msg)
	}
	task, taskOk := args["task"]
	status, statusOk := args["status"]
	if !taskOk && !statusOk {
		msg := "neither task nor status provided to todo_update tool"
		logger.Error(msg)
		return []byte(msg)
	}
	// Find and update the todo
	for i, item := range globalTodoList.Items {
		if item.ID == id {
			if taskOk {
				globalTodoList.Items[i].Task = task
			}
			if statusOk {
				// Validate status
				if status == "pending" || status == "in_progress" || status == "completed" {
					globalTodoList.Items[i].Status = status
				} else {
					result := map[string]string{
						"error": "status must be one of: pending, in_progress, completed",
					}
					jsonResult, err := json.Marshal(result)
					if err != nil {
						msg := "failed to marshal result; error: " + err.Error()
						logger.Error(msg)
						return []byte(msg)
					}
					return jsonResult
				}
			}
			result := map[string]string{
				"message": "todo updated successfully",
				"id":      id,
				"todos":   globalTodoList.ToString(),
			}
			jsonResult, err := json.Marshal(result)
			if err != nil {
				msg := "failed to marshal result; error: " + err.Error()
				logger.Error(msg)
				return []byte(msg)
			}
			return jsonResult
		}
	}
	// ID not found
	result := map[string]string{
		"error": fmt.Sprintf("todo with id %s not found", id),
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		msg := "failed to marshal result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	return jsonResult
}

func todoDelete(args map[string]string) []byte {
	id, ok := args["id"]
	if !ok || id == "" {
		msg := "id not provided to todo_delete tool"
		logger.Error(msg)
		return []byte(msg)
	}
	// Find and remove the todo
	for i, item := range globalTodoList.Items {
		if item.ID == id {
			// Remove item from slice
			globalTodoList.Items = append(globalTodoList.Items[:i], globalTodoList.Items[i+1:]...)
			result := map[string]string{
				"message": "todo deleted successfully",
				"id":      id,
				"todos":   globalTodoList.ToString(),
			}
			jsonResult, err := json.Marshal(result)
			if err != nil {
				msg := "failed to marshal result; error: " + err.Error()
				logger.Error(msg)
				return []byte(msg)
			}
			return jsonResult
		}
	}
	// ID not found
	result := map[string]string{
		"error": fmt.Sprintf("todo with id %s not found", id),
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		msg := "failed to marshal result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	return jsonResult
}

func taskDone(args map[string]string) []byte {
	taskActive.Store(false)
	result := map[string]string{
		"message": "task marked as done",
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		msg := "failed to marshal result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	return jsonResult
}

func viewImgTool(args map[string]string) []byte {
	file, ok := args["file"]
	if !ok || file == "" {
		msg := "file not provided to view_img tool"
		logger.Error(msg)
		return []byte(msg)
	}
	result := FsViewImg([]string{file}, "")
	return []byte(result)
}

func helpTool(args map[string]string) []byte {
	command, ok := args["command"]
	var rest []string
	if ok && command != "" {
		parts := strings.Fields(command)
		if len(parts) > 1 {
			rest = parts[1:]
		}
	}
	return []byte(getHelp(rest))
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

func listWindows(args map[string]string) []byte {
	cmd := exec.Command(xdotoolPath, "search", "--name", ".")
	output, err := cmd.Output()
	if err != nil {
		msg := "failed to list windows: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
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
		return []byte(msg)
	}
	return data
}

func captureWindow(args map[string]string) []byte {
	window, ok := args["window"]
	if !ok || window == "" {
		return []byte("window parameter required (window ID or name)")
	}
	var windowID string
	if _, err := strconv.Atoi(window); err == nil {
		windowID = window
	} else {
		cmd := exec.Command(xdotoolPath, "search", "--name", window)
		output, err := cmd.Output()
		if err != nil || len(strings.Fields(string(output))) == 0 {
			return []byte("window not found: " + window)
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
		return []byte(msg)
	}
	return []byte("screenshot saved: " + filename)
}

func captureWindowAndView(args map[string]string) []byte {
	window, ok := args["window"]
	if !ok || window == "" {
		return []byte("window parameter required (window ID or name)")
	}
	var windowID string
	if _, err := strconv.Atoi(window); err == nil {
		windowID = window
	} else {
		cmd := exec.Command(xdotoolPath, "search", "--name", window)
		output, err := cmd.Output()
		if err != nil || len(strings.Fields(string(output))) == 0 {
			return []byte("window not found: " + window)
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
		return []byte(msg)
	}
	dataURL, err := models.CreateImageURLFromPath(filename)
	if err != nil {
		msg := "failed to create image URL: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
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
		return []byte(msg)
	}
	return jsonResult
}

type fnSig func(map[string]string) []byte

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

func cmdMemory(args map[string]string) []byte {
	return []byte(FsMemory(argsToSlice(args), ""))
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

var FnMap = map[string]fnSig{
	"memory":        cmdMemory,
	"rag_search":    ragsearch,
	"websearch":     websearch,
	"websearch_raw": websearchRaw,
	"read_url":      readURL,
	"read_url_raw":  readURLRaw,
	"view_img":      viewImgTool,
	"help":          helpTool,
	// Unified run command
	"run": runCmd,
	// Browser tool - routes to runBrowserCommand
	"browser":        browserCmd,
	"summarize_chat": summarizeChat,
	"task_done":      taskDone,
}

func removeWindowToolsFromBaseTools() {
	windowToolNames := map[string]bool{
		"list_windows":            true,
		"capture_window":          true,
		"capture_window_and_view": true,
	}
	var filtered []models.Tool
	for _, tool := range BaseTools {
		if !windowToolNames[tool.Function.Name] {
			filtered = append(filtered, tool)
		}
	}
	BaseTools = filtered
	delete(FnMap, "list_windows")
	delete(FnMap, "capture_window")
	delete(FnMap, "capture_window_and_view")
}

func summarizeChat(args map[string]string) []byte {
	data, err := json.Marshal(args)
	if err != nil {
		return []byte("error: failed to marshal arguments")
	}
	return data
}

// for pw agentA
// var browserAgentSysPrompt = `You are an autonomous browser automation agent. Your goal is to complete the user's task by intelligently using browser automation

// Important: The browser may already be running from a previous task! Always check pw_is_running first before starting a new browser.

// Always provide clear feedback about what you're doing and what you found.`

func CallToolWithAgent(name string, args map[string]string) ([]byte, bool) {
	f, ok := FnMap[name]
	if !ok {
		return []byte(fmt.Sprintf("tool %s not found", name)), false
	}
	raw := f(args)
	if a := agent.Get(name); a != nil {
		return a.Process(args, raw), true
	}
	return raw, true
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
			Description: "List all available commands. Use this to discover what commands are available when unsure.",
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
	// task_done
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "task_done",
			Description: "Mark the current task as complete. Call this when you have finished the intended task and no more tool calls are needed.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"done": models.ToolArgProps{
						Type:        "string",
						Description: "set to 'true' to confirm task completion",
					},
				},
			},
		},
	},
	// run - unified command
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "run",
			Description: "Execute commands: shell, git, memory, todo. Usage: run \"<command>\". Examples: run \"ls -la\", run \"git status\", run \"memory store foo bar\", run \"memory get foo\", run \"todo create task\", run \"help\", run \"help memory\"",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"command"},
				Properties: map[string]models.ToolArgProps{
					"command": models.ToolArgProps{
						Type:        "string",
						Description: "command to execute. Use: run \"help\" for all commands, run \"help <cmd>\" for specific help. Examples: ls, cat, grep, git status, memory store, todo create, etc.",
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
}
