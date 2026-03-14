package main

import (
	"context"
	"encoding/json"
	"fmt"
	"gf-lt/agent"
	"gf-lt/config"
	"gf-lt/models"
	"gf-lt/storage"
	"gf-lt/tools"
	"io"
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
	toolCallRE         = regexp.MustCompile(`__tool_call__\s*([\s\S]*?)__tool_call__`)
	quotesRE           = regexp.MustCompile(`(".*?")`)
	starRE             = regexp.MustCompile(`(\*.*?\*)`)
	thinkRE            = regexp.MustCompile(`(<think>\s*([\s\S]*?)</think>)`)
	codeBlockRE        = regexp.MustCompile(`(?s)\x60{3}(?:.*?)\n(.*?)\n\s*\x60{3}\s*`)
	singleBacktickRE   = regexp.MustCompile(`\x60([^\x60]*)\x60`)
	roleRE             = regexp.MustCompile(`^(\w+):`)
	rpDefenitionSysMsg = `
For this roleplay immersion is at most importance.
Every character thinks and acts based on their personality and setting of the roleplay.
Meta discussions outside of roleplay is allowed if clearly labeled as out of character, for example: (ooc: {msg}) or <ooc>{msg}</ooc>.
`
	basicSysMsg = `Large Language Model that helps user with any of his requests.`
	toolSysMsg  = `You can do functions call if needed.
Your current tools:
<tools>
[
{
"name":"run",
"args": ["command"],
"when_to_use": "main tool: run shell, memory, git, todo. Use run \"help\" for all commands, run \"help <cmd>\" for specific help. Examples: run \"ls -la\", run \"help\", run \"help memory\", run \"git status\", run \"memory store foo bar\""
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
"name":"browser_agent",
"args": ["task"],
"when_to_use": "autonomous browser automation for complex tasks"
}
]
</tools>
To make a function call return a json object within __tool_call__ tags;
<example_request>
__tool_call__
{
"name":"recall",
"args": {"topic": "Adam's number"}
}
__tool_call__
</example_request>
<example_request>
__tool_call__
{
"name":"execute_command",
"args": {"command": "ls", "args": "-la /home"}
}
__tool_call__
</example_request>
Tool call is addressed to the tool agent, avoid sending more info than tool call itself, while making a call.
When done right, tool call will be delivered to the tool agent. tool agent will respond with the results of the call.
<example_response>
tool:
under the topic: Adam's number is stored:
559-996
</example_response>
After that you are free to respond to the user.
`
	webSearchSysPrompt = `Summarize the web search results, extracting key information and presenting a concise answer. Provide sources and URLs where relevant.`
	ragSearchSysPrompt = `Synthesize the document search results, extracting key information and presenting a concise answer. Provide sources and document IDs where relevant.`
	readURLSysPrompt   = `Extract and summarize the content from the webpage. Provide key information, main points, and any relevant details.`
	summarySysPrompt   = `Please provide a concise summary of the following conversation. Focus on key points, decisions, and actions. Provide only the summary, no additional commentary.`
	basicCard          = &models.CharCard{
		ID:        models.ComputeCardID("assistant", "basic_sys"),
		SysPrompt: basicSysMsg,
		FirstMsg:  defaultFirstMsg,
		Role:      "assistant",
		FilePath:  "basic_sys",
	}
	sysMap    = map[string]*models.CharCard{}
	roleToID  = map[string]string{}
	sysLabels = []string{"assistant"}

	webAgentClient     *agent.AgentClient
	webAgentClientOnce sync.Once
	webAgentsOnce      sync.Once
)

var windowToolSysMsg = `
Additional window tools (available only if xdotool and maim are installed):
[
{
"name":"list_windows",
"args": [],
"when_to_use": "when asked to list visible windows; returns map of window ID to window name"
},
{
"name":"capture_window",
"args": ["window"],
"when_to_use": "when asked to take a screenshot of a specific window; saves to /tmp; window can be ID or name substring; returns file path"
},
{
"name":"capture_window_and_view",
"args": ["window"],
"when_to_use": "when asked to take a screenshot of a specific window and show it; saves to /tmp and returns image for viewing; window can be ID or name substring"
}
]
`

var WebSearcher searcher.WebSurfer

var (
	windowToolsAvailable bool
	xdotoolPath          string
	maimPath             string
	modelHasVision       bool
)

func initTools() {
	sysMap[basicCard.ID] = basicCard
	roleToID["assistant"] = basicCard.ID
	// Initialize fs root directory
	tools.SetFSRoot(cfg.FilePickerDir)
	// Initialize memory store
	tools.SetMemoryStore(&memoryAdapter{store: store, cfg: cfg}, cfg.AssistantRole)
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
	checkWindowTools()
	registerWindowTools()
}

func GetCardByRole(role string) *models.CharCard {
	cardID, ok := roleToID[role]
	if !ok {
		return nil
	}
	return sysMap[cardID]
}

func checkWindowTools() {
	xdotoolPath, _ = exec.LookPath("xdotool")
	maimPath, _ = exec.LookPath("maim")
	windowToolsAvailable = xdotoolPath != "" && maimPath != ""
	if windowToolsAvailable {
		logger.Info("window tools available: xdotool and maim found")
	} else {
		if xdotoolPath == "" {
			logger.Warn("xdotool not found, window listing tools will not be available")
		}
		if maimPath == "" {
			logger.Warn("maim not found, window capture tools will not be available")
		}
	}
}

func updateToolCapabilities() {
	if !cfg.ToolUse {
		return
	}
	modelHasVision = false
	if cfg == nil || cfg.CurrentAPI == "" {
		logger.Warn("cannot determine model capabilities: cfg or CurrentAPI is nil")
		registerWindowTools()
		fnMap["browser_agent"] = runBrowserAgent
		// registerPlaywrightTools()
		return
	}
	prevHasVision := modelHasVision
	modelHasVision = ModelHasVision(cfg.CurrentAPI, cfg.CurrentModel)
	if modelHasVision {
		logger.Info("model has vision support", "model", cfg.CurrentModel, "api", cfg.CurrentAPI)
	} else {
		logger.Info("model does not have vision support", "model", cfg.CurrentModel, "api", cfg.CurrentAPI)
		if windowToolsAvailable && !prevHasVision && !modelHasVision {
			showToast("window tools", "Window capture-and-view unavailable: model lacks vision support")
		}
	}
	registerWindowTools()
	fnMap["browser_agent"] = runBrowserAgent
	// registerPlaywrightTools()
}

// getWebAgentClient returns a singleton AgentClient for web agents.
func getWebAgentClient() *agent.AgentClient {
	webAgentClientOnce.Do(func() {
		getToken := func() string {
			if chunkParser == nil {
				return ""
			}
			return chunkParser.GetToken()
		}
		webAgentClient = agent.NewAgentClient(cfg, logger, getToken)
	})
	return webAgentClient
}

// registerWebAgents registers WebAgentB instances for websearch and read_url tools.
func registerWebAgents() {
	webAgentsOnce.Do(func() {
		client := getWebAgentClient()
		// Register rag_search agent
		agent.RegisterB("rag_search", agent.NewWebAgentB(client, ragSearchSysPrompt))
		// Register websearch agent
		agent.RegisterB("websearch", agent.NewWebAgentB(client, webSearchSysPrompt))
		// Register read_url agent
		agent.RegisterB("read_url", agent.NewWebAgentB(client, readURLSysPrompt))
		// Register summarize_chat agent
		agent.RegisterB("summarize_chat", agent.NewWebAgentB(client, summarySysPrompt))
	})
}

// web search (depends on extra server)
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

// Helper functions for file operations
func resolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(cfg.FilePickerDir, p)
}

func readStringFromFile(filename string) (string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeStringToFile(filename string, data string) error {
	return os.WriteFile(filename, []byte(data), 0644)
}

func appendStringToFile(filename string, data string) error {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(data)
	return err
}

func removeFile(filename string) error {
	return os.Remove(filename)
}

func moveFile(src, dst string) error {
	// First try with os.Rename (works within same filesystem)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// If that fails (e.g., cross-filesystem), copy and delete
	return copyAndRemove(src, dst)
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	_, err = io.Copy(dstFile, srcFile)
	return err
}

func copyAndRemove(src, dst string) error {
	// Copy the file
	if err := copyFile(src, dst); err != nil {
		return err
	}
	// Remove the source file
	return os.Remove(src)
}

func listDirectory(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			files = append(files, entry.Name()+"/") // Add "/" to indicate directory
		} else {
			files = append(files, entry.Name())
		}
	}
	return files, nil
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
		return []byte(tools.FsMemory(append([]string{"store"}, rest...), ""))
	case "todo":
		// todo create|read|update|delete - route to existing todo handlers
		return []byte(handleTodoSubcommand(rest, args))
	default:
		// Everything else: shell with pipe/chaining support
		result := tools.ExecChain(commandStr)
		return []byte(result)
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
  see <file>      - view image file
  write <file>    - write content to file
  stat <file>     - get file info
  rm <file>       - delete file
  cp <src> <dst> - copy file
  mv <src> <dst> - move/rename file
  mkdir <dir>     - create directory
  pwd             - print working directory
  cd <dir>        - change directory
  
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
	case "see":
		return `see <image-file>
  View an image file for multimodal analysis.
  Supports: png, jpg, jpeg, gif, webp, svg
  Example:
    run "see screenshot.png"`
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
    run "grep -i warning log.txt"`
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
		return []byte(fmt.Sprintf("unknown todo subcommand: %s", subcmd))
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
	result := tools.ExecChain(commandStr)
	return []byte(result)
}

// handleCdCommand handles the cd command to update FilePickerDir
func handleCdCommand(args []string) []byte {
	var targetDir string
	if len(args) == 0 {
		// cd with no args goes to home directory
		homeDir, err := os.UserHomeDir()
		if err != nil {
			msg := "cd: cannot determine home directory: " + err.Error()
			logger.Error(msg)
			return []byte(msg)
		}
		targetDir = homeDir
	} else {
		targetDir = args[0]
	}

	// Resolve relative paths against current FilePickerDir
	if !filepath.IsAbs(targetDir) {
		targetDir = filepath.Join(cfg.FilePickerDir, targetDir)
	}

	// Verify the directory exists
	info, err := os.Stat(targetDir)
	if err != nil {
		msg := "cd: " + targetDir + ": " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	if !info.IsDir() {
		msg := "cd: " + targetDir + ": not a directory"
		logger.Error(msg)
		return []byte(msg)
	}

	// Update FilePickerDir
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		msg := "cd: failed to resolve path: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	cfg.FilePickerDir = absDir
	msg := "FilePickerDir changed to: " + absDir
	return []byte(msg)
}

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

var gitReadSubcommands = map[string]bool{
	"status":    true,
	"log":       true,
	"diff":      true,
	"show":      true,
	"branch":    true,
	"reflog":    true,
	"rev-parse": true,
	"shortlog":  true,
	"describe":  true,
}

func isCommandAllowed(command string, args ...string) bool {
	allowedCommands := map[string]bool{
		"cd":     true,
		"grep":   true,
		"sed":    true,
		"awk":    true,
		"find":   true,
		"cat":    true,
		"head":   true,
		"tail":   true,
		"sort":   true,
		"uniq":   true,
		"wc":     true,
		"ls":     true,
		"echo":   true,
		"cut":    true,
		"tr":     true,
		"cp":     true,
		"mv":     true,
		"rm":     true,
		"mkdir":  true,
		"rmdir":  true,
		"pwd":    true,
		"df":     true,
		"free":   true,
		"ps":     true,
		"top":    true,
		"du":     true,
		"whoami": true,
		"date":   true,
		"uname":  true,
		"git":    true,
		"go":     true,
	}
	// Allow all go subcommands (go run, go mod tidy, go test, etc.)
	if strings.HasPrefix(command, "go ") && allowedCommands["go"] {
		return true
	}
	if command == "git" && len(args) > 0 {
		return gitReadSubcommands[args[0]]
	}
	if !allowedCommands[command] {
		return false
	}
	return true
}

func summarizeChat(args map[string]string) []byte {
	if len(chatBody.Messages) == 0 {
		return []byte("No chat history to summarize.")
	}
	// Format chat history for the agent
	chatText := chatToText(chatBody.Messages, true) // include system and tool messages
	return []byte(chatText)
}

func windowIDToHex(decimalID string) string {
	id, err := strconv.ParseInt(decimalID, 10, 64)
	if err != nil {
		return decimalID
	}
	return fmt.Sprintf("0x%x", id)
}

func listWindows(args map[string]string) []byte {
	if !windowToolsAvailable {
		return []byte("window tools not available: xdotool or maim not found")
	}
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
	if !windowToolsAvailable {
		return []byte("window tools not available: xdotool or maim not found")
	}
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
	if !windowToolsAvailable {
		return []byte("window tools not available: xdotool or maim not found")
	}
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

func cmdLs(args map[string]string) []byte {
	return []byte(tools.FsLs(argsToSlice(args), ""))
}

func cmdCat(args map[string]string) []byte {
	return []byte(tools.FsCat(argsToSlice(args), ""))
}

func cmdSee(args map[string]string) []byte {
	return []byte(tools.FsSee(argsToSlice(args), ""))
}

func cmdWrite(args map[string]string) []byte {
	// write needs special handling - content might be in args or stdin
	slice := argsToSlice(args)
	// If there's a "content" key, append it
	if content, ok := args["content"]; ok && content != "" {
		slice = append(slice, content)
	}
	return []byte(tools.FsWrite(slice, ""))
}

func cmdStat(args map[string]string) []byte {
	return []byte(tools.FsStat(argsToSlice(args), ""))
}

func cmdRm(args map[string]string) []byte {
	return []byte(tools.FsRm(argsToSlice(args), ""))
}

func cmdCp(args map[string]string) []byte {
	return []byte(tools.FsCp(argsToSlice(args), ""))
}

func cmdMv(args map[string]string) []byte {
	return []byte(tools.FsMv(argsToSlice(args), ""))
}

func cmdMkdir(args map[string]string) []byte {
	return []byte(tools.FsMkdir(argsToSlice(args), ""))
}

func cmdEcho(args map[string]string) []byte {
	return []byte(tools.FsEcho(argsToSlice(args), ""))
}

func cmdTime(args map[string]string) []byte {
	return []byte(tools.FsTime(argsToSlice(args), ""))
}

func cmdGrep(args map[string]string) []byte {
	// grep needs special handling - pattern and flags
	slice := argsToSlice(args)
	// Check for pattern key
	if pattern, ok := args["pattern"]; ok && pattern != "" {
		slice = append([]string{pattern}, slice...)
	}
	return []byte(tools.FsGrep(slice, ""))
}

func cmdHead(args map[string]string) []byte {
	slice := argsToSlice(args)
	return []byte(tools.FsHead(slice, ""))
}

func cmdTail(args map[string]string) []byte {
	slice := argsToSlice(args)
	return []byte(tools.FsTail(slice, ""))
}

func cmdWc(args map[string]string) []byte {
	slice := argsToSlice(args)
	return []byte(tools.FsWc(slice, ""))
}

func cmdSort(args map[string]string) []byte {
	slice := argsToSlice(args)
	return []byte(tools.FsSort(slice, ""))
}

func cmdUniq(args map[string]string) []byte {
	slice := argsToSlice(args)
	return []byte(tools.FsUniq(slice, ""))
}

func cmdGit(args map[string]string) []byte {
	slice := argsToSlice(args)
	// Check for subcommand key
	if subcmd, ok := args["subcommand"]; ok && subcmd != "" {
		slice = append([]string{subcmd}, slice...)
	}
	return []byte(tools.FsGit(slice, ""))
}

func cmdPwd(args map[string]string) []byte {
	return []byte(tools.FsPwd(argsToSlice(args), ""))
}

func cmdCd(args map[string]string) []byte {
	return []byte(tools.FsCd(argsToSlice(args), ""))
}

func cmdMemory(args map[string]string) []byte {
	return []byte(tools.FsMemory(argsToSlice(args), ""))
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

var fnMap = map[string]fnSig{
	"memory":        cmdMemory,
	"rag_search":    ragsearch,
	"websearch":     websearch,
	"websearch_raw": websearchRaw,
	"read_url":      readURL,
	"read_url_raw":  readURLRaw,
	// Unix-style file commands (replacing file_* tools)
	"ls":    cmdLs,
	"cat":   cmdCat,
	"see":   cmdSee,
	"write": cmdWrite,
	"stat":  cmdStat,
	"rm":    cmdRm,
	"cp":    cmdCp,
	"mv":    cmdMv,
	"mkdir": cmdMkdir,
	"pwd":   cmdPwd,
	"cd":    cmdCd,
	// Unified run command
	"run":            runCmd,
	"summarize_chat": summarizeChat,
}

func removeWindowToolsFromBaseTools() {
	windowToolNames := map[string]bool{
		"list_windows":            true,
		"capture_window":          true,
		"capture_window_and_view": true,
	}
	var filtered []models.Tool
	for _, tool := range baseTools {
		if !windowToolNames[tool.Function.Name] {
			filtered = append(filtered, tool)
		}
	}
	baseTools = filtered
	delete(fnMap, "list_windows")
	delete(fnMap, "capture_window")
	delete(fnMap, "capture_window_and_view")
}

func removePlaywrightToolsFromBaseTools() {
	playwrightToolNames := map[string]bool{
		"pw_start":               true,
		"pw_stop":                true,
		"pw_is_running":          true,
		"pw_navigate":            true,
		"pw_click":               true,
		"pw_click_at":            true,
		"pw_fill":                true,
		"pw_extract_text":        true,
		"pw_screenshot":          true,
		"pw_screenshot_and_view": true,
		"pw_wait_for_selector":   true,
		"pw_drag":                true,
	}
	var filtered []models.Tool
	for _, tool := range baseTools {
		if !playwrightToolNames[tool.Function.Name] {
			filtered = append(filtered, tool)
		}
	}
	baseTools = filtered
	delete(fnMap, "pw_start")
	delete(fnMap, "pw_stop")
	delete(fnMap, "pw_is_running")
	delete(fnMap, "pw_navigate")
	delete(fnMap, "pw_click")
	delete(fnMap, "pw_click_at")
	delete(fnMap, "pw_fill")
	delete(fnMap, "pw_extract_text")
	delete(fnMap, "pw_screenshot")
	delete(fnMap, "pw_screenshot_and_view")
	delete(fnMap, "pw_wait_for_selector")
	delete(fnMap, "pw_drag")
}

func registerWindowTools() {
	removeWindowToolsFromBaseTools()
	if windowToolsAvailable {
		fnMap["list_windows"] = listWindows
		fnMap["capture_window"] = captureWindow
		windowTools := []models.Tool{
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "list_windows",
					Description: "List all visible windows with their IDs and names. Returns a map of window ID to window name.",
					Parameters: models.ToolFuncParams{
						Type:       "object",
						Required:   []string{},
						Properties: map[string]models.ToolArgProps{},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "capture_window",
					Description: "Capture a screenshot of a specific window and save it to /tmp. Requires window parameter (window ID or name substring).",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{"window"},
						Properties: map[string]models.ToolArgProps{
							"window": models.ToolArgProps{
								Type:        "string",
								Description: "window ID or window name (partial match)",
							},
						},
					},
				},
			},
		}
		if modelHasVision {
			fnMap["capture_window_and_view"] = captureWindowAndView
			windowTools = append(windowTools, models.Tool{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "capture_window_and_view",
					Description: "Capture a screenshot of a specific window, save it to /tmp, and return the image for viewing. Requires window parameter (window ID or name substring).",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{"window"},
						Properties: map[string]models.ToolArgProps{
							"window": models.ToolArgProps{
								Type:        "string",
								Description: "window ID or window name (partial match)",
							},
						},
					},
				},
			})
		}
		baseTools = append(baseTools, windowTools...)
		toolSysMsg += windowToolSysMsg
	}
}

var browserAgentSysPrompt = `You are an autonomous browser automation agent. Your goal is to complete the user's task by intelligently using browser automation tools.

Important: The browser may already be running from a previous task! Always check pw_is_running first before starting a new browser.

Available tools:
- pw_start: Start browser (only if not already running)
- pw_stop: Stop browser (only when you're truly done and browser is no longer needed)
- pw_is_running: Check if browser is running
- pw_navigate: Go to a URL
- pw_click: Click an element by CSS selector
- pw_fill: Type text into an input
- pw_extract_text: Get text from page/element
- pw_screenshot: Take a screenshot (returns file path)
- pw_screenshot_and_view: Take screenshot with image for viewing
- pw_wait_for_selector: Wait for element to appear
- pw_drag: Drag mouse from one point to another
- pw_click_at: Click at X,Y coordinates
- pw_get_html: Get HTML content
- pw_get_dom: Get structured DOM tree
- pw_search_elements: Search for elements by text or selector

Workflow:
1. First, check if browser is already running (pw_is_running)
2. Only start browser if not already running (pw_start)
3. Navigate to required pages (pw_navigate)
4. Interact with elements as needed (click, fill, etc.)
5. Extract information or take screenshots as requested
6. IMPORTANT: Do NOT stop the browser when done! Leave it running so the user can continue interacting with the page in subsequent requests.

Always provide clear feedback about what you're doing and what you found.`

func runBrowserAgent(args map[string]string) []byte {
	task, ok := args["task"]
	if !ok || task == "" {
		return []byte(`{"error": "task argument is required"}`)
	}
	client := getWebAgentClient()
	pwAgent := agent.NewPWAgent(client, browserAgentSysPrompt)
	pwAgent.SetTools(agent.GetPWTools())
	return pwAgent.ProcessTask(task)
}

func registerPlaywrightTools() {
	removePlaywrightToolsFromBaseTools()
	if cfg != nil && cfg.PlaywrightEnabled {
		fnMap["pw_start"] = pwStart
		fnMap["pw_stop"] = pwStop
		fnMap["pw_is_running"] = pwIsRunning
		fnMap["pw_navigate"] = pwNavigate
		fnMap["pw_click"] = pwClick
		fnMap["pw_click_at"] = pwClickAt
		fnMap["pw_fill"] = pwFill
		fnMap["pw_extract_text"] = pwExtractText
		fnMap["pw_screenshot"] = pwScreenshot
		fnMap["pw_screenshot_and_view"] = pwScreenshotAndView
		fnMap["pw_wait_for_selector"] = pwWaitForSelector
		fnMap["pw_drag"] = pwDrag
		fnMap["pw_get_html"] = pwGetHTML
		fnMap["pw_get_dom"] = pwGetDOM
		fnMap["pw_search_elements"] = pwSearchElements
		playwrightTools := []models.Tool{
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_start",
					Description: "Start a Playwright browser instance. Call this first before using other pw_ tools. Uses headless mode by default (set PlaywrightHeadless=false in config for GUI).",
					Parameters: models.ToolFuncParams{
						Type:       "object",
						Required:   []string{},
						Properties: map[string]models.ToolArgProps{},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_stop",
					Description: "Stop the Playwright browser instance. Call when done with browser automation.",
					Parameters: models.ToolFuncParams{
						Type:       "object",
						Required:   []string{},
						Properties: map[string]models.ToolArgProps{},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_is_running",
					Description: "Check if Playwright browser is currently running.",
					Parameters: models.ToolFuncParams{
						Type:       "object",
						Required:   []string{},
						Properties: map[string]models.ToolArgProps{},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_navigate",
					Description: "Navigate to a URL in the browser.",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{"url"},
						Properties: map[string]models.ToolArgProps{
							"url": models.ToolArgProps{
								Type:        "string",
								Description: "URL to navigate to",
							},
						},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_click",
					Description: "Click on an element using CSS selector. Use 'index' for multiple matches (default 0).",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{"selector"},
						Properties: map[string]models.ToolArgProps{
							"selector": models.ToolArgProps{
								Type:        "string",
								Description: "CSS selector for the element to click",
							},
							"index": models.ToolArgProps{
								Type:        "string",
								Description: "optional index for multiple matches (default 0)",
							},
						},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_fill",
					Description: "Fill an input field with text using CSS selector.",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{"selector", "text"},
						Properties: map[string]models.ToolArgProps{
							"selector": models.ToolArgProps{
								Type:        "string",
								Description: "CSS selector for the input element",
							},
							"text": models.ToolArgProps{
								Type:        "string",
								Description: "text to fill into the input",
							},
							"index": models.ToolArgProps{
								Type:        "string",
								Description: "optional index for multiple matches (default 0)",
							},
						},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_extract_text",
					Description: "Extract text content from the page or specific elements using CSS selector. Use 'body' for all page text.",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{"selector"},
						Properties: map[string]models.ToolArgProps{
							"selector": models.ToolArgProps{
								Type:        "string",
								Description: "CSS selector (use 'body' for all page text)",
							},
						},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_screenshot",
					Description: "Take a screenshot of the page or a specific element. Returns file path to saved image.",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{},
						Properties: map[string]models.ToolArgProps{
							"selector": models.ToolArgProps{
								Type:        "string",
								Description: "optional CSS selector for element to screenshot",
							},
							"full_page": models.ToolArgProps{
								Type:        "string",
								Description: "optional: 'true' to capture full page (default false)",
							},
						},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_screenshot_and_view",
					Description: "Take a screenshot and return the image for viewing. Use when model needs to see the screenshot.",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{},
						Properties: map[string]models.ToolArgProps{
							"selector": models.ToolArgProps{
								Type:        "string",
								Description: "optional CSS selector for element to screenshot",
							},
							"full_page": models.ToolArgProps{
								Type:        "string",
								Description: "optional: 'true' to capture full page (default false)",
							},
						},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_wait_for_selector",
					Description: "Wait for an element to appear on the page.",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{"selector"},
						Properties: map[string]models.ToolArgProps{
							"selector": models.ToolArgProps{
								Type:        "string",
								Description: "CSS selector to wait for",
							},
							"timeout": models.ToolArgProps{
								Type:        "string",
								Description: "optional timeout in ms (default 30000)",
							},
						},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_drag",
					Description: "Drag the mouse from one point to another.",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{"x1", "y1", "x2", "y2"},
						Properties: map[string]models.ToolArgProps{
							"x1": models.ToolArgProps{
								Type:        "string",
								Description: "starting X coordinate",
							},
							"y1": models.ToolArgProps{
								Type:        "string",
								Description: "starting Y coordinate",
							},
							"x2": models.ToolArgProps{
								Type:        "string",
								Description: "ending X coordinate",
							},
							"y2": models.ToolArgProps{
								Type:        "string",
								Description: "ending Y coordinate",
							},
						},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_get_html",
					Description: "Get the HTML content of the page or a specific element.",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{},
						Properties: map[string]models.ToolArgProps{
							"selector": models.ToolArgProps{
								Type:        "string",
								Description: "optional CSS selector (default: body)",
							},
						},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_get_dom",
					Description: "Get a structured DOM representation of an element with tag, attributes, text, and children.",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{},
						Properties: map[string]models.ToolArgProps{
							"selector": models.ToolArgProps{
								Type:        "string",
								Description: "optional CSS selector (default: body)",
							},
						},
					},
				},
			},
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "pw_search_elements",
					Description: "Search for elements by text content or CSS selector. Returns matching elements with their tags, text, and HTML.",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{},
						Properties: map[string]models.ToolArgProps{
							"text": models.ToolArgProps{
								Type:        "string",
								Description: "text to search for in elements",
							},
							"selector": models.ToolArgProps{
								Type:        "string",
								Description: "CSS selector to search for",
							},
						},
					},
				},
			},
		}
		baseTools = append(baseTools, playwrightTools...)
		toolSysMsg += browserToolSysMsg
		agent.RegisterPWTool("pw_start", pwStart)
		agent.RegisterPWTool("pw_stop", pwStop)
		agent.RegisterPWTool("pw_is_running", pwIsRunning)
		agent.RegisterPWTool("pw_navigate", pwNavigate)
		agent.RegisterPWTool("pw_click", pwClick)
		agent.RegisterPWTool("pw_click_at", pwClickAt)
		agent.RegisterPWTool("pw_fill", pwFill)
		agent.RegisterPWTool("pw_extract_text", pwExtractText)
		agent.RegisterPWTool("pw_screenshot", pwScreenshot)
		agent.RegisterPWTool("pw_screenshot_and_view", pwScreenshotAndView)
		agent.RegisterPWTool("pw_wait_for_selector", pwWaitForSelector)
		agent.RegisterPWTool("pw_drag", pwDrag)
		agent.RegisterPWTool("pw_get_html", pwGetHTML)
		agent.RegisterPWTool("pw_get_dom", pwGetDOM)
		agent.RegisterPWTool("pw_search_elements", pwSearchElements)
		browserAgentTool := []models.Tool{
			{
				Type: "function",
				Function: models.ToolFunc{
					Name:        "browser_agent",
					Description: "Autonomous browser automation agent. Use for complex multi-step browser tasks like 'go to website, login, and take screenshot'. The agent will plan and execute steps automatically using browser tools.",
					Parameters: models.ToolFuncParams{
						Type:     "object",
						Required: []string{"task"},
						Properties: map[string]models.ToolArgProps{
							"task": {Type: "string", Description: "The task to accomplish, e.g., 'go to github.com and take a screenshot of the homepage'"},
						},
					},
				},
			},
		}
		baseTools = append(baseTools, browserAgentTool...)
		fnMap["browser_agent"] = runBrowserAgent
	}
}

// callToolWithAgent calls the tool and applies any registered agent.
func callToolWithAgent(name string, args map[string]string) []byte {
	registerWebAgents()
	f, ok := fnMap[name]
	if !ok {
		return []byte(fmt.Sprintf("tool %s not found", name))
	}
	raw := f(args)
	if a := agent.Get(name); a != nil {
		return a.Process(args, raw)
	}
	return raw
}

// openai style def
var baseTools = []models.Tool{
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
	// run - unified command
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "memory",
			Description: "Memory management. Usage: memory store <topic> <data> | memory get <topic> | memory list | memory forget <topic>",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"subcommand"},
				Properties: map[string]models.ToolArgProps{
					"subcommand": models.ToolArgProps{
						Type:        "string",
						Description: "subcommand: store, get, list, topics, forget, delete",
					},
					"topic": models.ToolArgProps{
						Type:        "string",
						Description: "topic/key for memory",
					},
					"data": models.ToolArgProps{
						Type:        "string",
						Description: "data to store",
					},
				},
			},
		},
	},
	// Unix-style file commands
	// ls
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "ls",
			Description: "List files in a directory. Usage: ls [dir]",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"path": models.ToolArgProps{
						Type:        "string",
						Description: "directory to list (optional, defaults to current directory)",
					},
				},
			},
		},
	},
	// cat
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "cat",
			Description: "Read file content. Usage: cat <path>. Use -b flag for base64 output (for binary files).",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]models.ToolArgProps{
					"path": models.ToolArgProps{
						Type:        "string",
						Description: "path of the file to read",
					},
				},
			},
		},
	},
	// see
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "see",
			Description: "View an image file and return it for multimodal LLM viewing. Supports png, jpg, jpeg, gif, webp, svg.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]models.ToolArgProps{
					"path": models.ToolArgProps{
						Type:        "string",
						Description: "path of the image file to view",
					},
				},
			},
		},
	},
	// write
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "write",
			Description: "Write content to a file. Will overwrite any content present. Usage: write <path> [content]. Use -b flag for base64 input (for binary files).",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]models.ToolArgProps{
					"path": models.ToolArgProps{
						Type:        "string",
						Description: "path of the file to write to",
					},
					"content": models.ToolArgProps{
						Type:        "string",
						Description: "content to write to the file",
					},
				},
			},
		},
	},
	// stat
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "stat",
			Description: "Get file information (size, type, modified time). Usage: stat <path>",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]models.ToolArgProps{
					"path": models.ToolArgProps{
						Type:        "string",
						Description: "path of the file to get info for",
					},
				},
			},
		},
	},
	// rm
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "rm",
			Description: "Delete a file. Usage: rm <path>",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]models.ToolArgProps{
					"path": models.ToolArgProps{
						Type:        "string",
						Description: "path of the file to delete",
					},
				},
			},
		},
	},
	// cp
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "cp",
			Description: "Copy a file. Usage: cp <src> <dst>",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"src", "dst"},
				Properties: map[string]models.ToolArgProps{
					"src": models.ToolArgProps{
						Type:        "string",
						Description: "source file path",
					},
					"dst": models.ToolArgProps{
						Type:        "string",
						Description: "destination file path",
					},
				},
			},
		},
	},
	// mv
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "mv",
			Description: "Move/rename a file. Usage: mv <src> <dst>",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"src", "dst"},
				Properties: map[string]models.ToolArgProps{
					"src": models.ToolArgProps{
						Type:        "string",
						Description: "source file path",
					},
					"dst": models.ToolArgProps{
						Type:        "string",
						Description: "destination file path",
					},
				},
			},
		},
	},
	// mkdir
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "mkdir",
			Description: "Create a directory. Usage: mkdir <dir>",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"dir"},
				Properties: map[string]models.ToolArgProps{
					"dir": models.ToolArgProps{
						Type:        "string",
						Description: "directory path to create",
					},
				},
			},
		},
	},
	// pwd
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "pwd",
			Description: "Print working directory. Returns the current directory path.",
			Parameters: models.ToolFuncParams{
				Type:       "object",
				Required:   []string{},
				Properties: map[string]models.ToolArgProps{},
			},
		},
	},
	// cd
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "cd",
			Description: "Change working directory. Usage: cd <dir>",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"dir"},
				Properties: map[string]models.ToolArgProps{
					"dir": models.ToolArgProps{
						Type:        "string",
						Description: "directory to change to",
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
	// echo
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "echo",
			Description: "Echo back the input. Usage: echo [args] or pipe stdin",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"args": models.ToolArgProps{
						Type:        "string",
						Description: "arguments to echo",
					},
				},
			},
		},
	},
	// time
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "time",
			Description: "Return the current time.",
			Parameters: models.ToolFuncParams{
				Type:       "object",
				Required:   []string{},
				Properties: map[string]models.ToolArgProps{},
			},
		},
	},
	// grep
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "grep",
			Description: "Filter lines matching a pattern. Usage: grep [-i] [-v] [-c] <pattern>. -i: ignore case, -v: invert match, -c: count matches.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"pattern"},
				Properties: map[string]models.ToolArgProps{
					"pattern": models.ToolArgProps{
						Type:        "string",
						Description: "pattern to search for",
					},
				},
			},
		},
	},
	// head
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "head",
			Description: "Show first N lines. Usage: head [n] or head -n <n>. Default: 10",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"n": models.ToolArgProps{
						Type:        "string",
						Description: "number of lines (optional, default 10)",
					},
				},
			},
		},
	},
	// tail
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "tail",
			Description: "Show last N lines. Usage: tail [n] or tail -n <n>. Default: 10",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"n": models.ToolArgProps{
						Type:        "string",
						Description: "number of lines (optional, default 10)",
					},
				},
			},
		},
	},
	// wc
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "wc",
			Description: "Count lines, words, chars. Usage: wc [-l] [-w] [-c]. -l: lines, -w: words, -c: chars.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"flag": models.ToolArgProps{
						Type:        "string",
						Description: "optional flag: -l, -w, or -c",
					},
				},
			},
		},
	},
	// sort
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "sort",
			Description: "Sort lines. Usage: sort [-r] [-n]. -r: reverse, -n: numeric sort.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"reverse": models.ToolArgProps{
						Type:        "string",
						Description: "use -r for reverse sort",
					},
					"numeric": models.ToolArgProps{
						Type:        "string",
						Description: "use -n for numeric sort",
					},
				},
			},
		},
	},
	// uniq
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "uniq",
			Description: "Remove duplicate lines. Usage: uniq [-c] to show count.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"count": models.ToolArgProps{
						Type:        "string",
						Description: "use -c to show count of occurrences",
					},
				},
			},
		},
	},
	// git (read-only)
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "git",
			Description: "Execute read-only git commands. Allowed: status, log, diff, show, branch, reflog, rev-parse, shortlog, describe.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"subcommand"},
				Properties: map[string]models.ToolArgProps{
					"subcommand": models.ToolArgProps{
						Type:        "string",
						Description: "git subcommand (status, log, diff, show, branch, reflog, rev-parse, shortlog, describe)",
					},
				},
			},
		},
	},
}
