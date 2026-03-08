package main

import (
	"context"
	"encoding/json"
	"fmt"
	"gf-lt/agent"
	"gf-lt/models"
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
"name":"recall",
"args": ["topic"],
"when_to_use": "when asked about topic that user previously asked to memorise"
},
{
"name":"memorise",
"args": ["topic", "data"],
"when_to_use": "when asked to memorise information under a topic"
},
{
"name":"recall_topics",
"args": [],
"when_to_use": "to see what topics are saved in memory"
},
{
"name":"websearch",
"args": ["query", "limit"],
"when_to_use": "when asked to search the web for information; returns clean summary without html,css and other web elements; limit is optional (default 3)"
},
{
"name":"rag_search",
"args": ["query", "limit"],
"when_to_use": "when asked to search the local document database for information; performs query refinement, semantic search, reranking, and synthesis; returns clean summary with sources; limit is optional (default 3)"
},
{
"name":"read_url",
"args": ["url"],
"when_to_use": "when asked to get content for specific webpage or url; returns clean summary without html,css and other web elements"
},
{
"name":"read_url_raw",
"args": ["url"],
"when_to_use": "when asked to get content for specific webpage or url; returns raw data as is without processing"
},
{
"name":"file_create",
"args": ["path", "content"],
"when_to_use": "when there is a need to create a new file with optional content"
},
{
"name":"file_read",
"args": ["path"],
"when_to_use": "when you need to read the content of a file"
},
{
"name":"file_read_image",
"args": ["path"],
"when_to_use": "when you need to read or view an image file"
},
{
"name":"file_write",
"args": ["path", "content"],
"when_to_use": "when needed to overwrite content to a file"
},
{
"name":"file_write_append",
"args": ["path", "content"],
"when_to_use": "when you need append content to a file; use sed to edit content"
},
{
"name":"file_edit",
"args": ["path", "oldString", "newString", "lineNumber"],
"when_to_use": "when you need to make targeted changes to a specific section of a file without rewriting the entire file; lineNumber is optional - if provided, only edits that specific line; if not provided, replaces all occurrences of oldString"
},
{
"name":"file_delete",
"args": ["path"],
"when_to_use": "when asked to delete a file"
},
{
"name":"file_move",
"args": ["src", "dst"],
"when_to_use": "when you need to move a file from source to destination"
},
{
"name":"file_copy",
"args": ["src", "dst"],
"when_to_use": "copy a file from source to destination"
},
{
"name":"file_list",
"args": ["path"],
"when_to_use": "list files in a directory; path is optional (default: current directory)"
},
{
"name":"execute_command",
"args": ["command", "args"],
"when_to_use": "execute a system command; args is optional; allowed commands: grep, sed, awk, find, cat, head, tail, sort, uniq, wc, ls, echo, cut, tr, cp, mv, rm, mkdir, rmdir, pwd, df, free, ps, top, du, whoami, date, uname, go"
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
		registerPlaywrightTools()
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
	registerPlaywrightTools()
}

// getWebAgentClient returns a singleton AgentClient for web agents.
func getWebAgentClient() *agent.AgentClient {
	webAgentClientOnce.Do(func() {
		if cfg == nil {
			if logger != nil {
				logger.Warn("web agent client unavailable: config not initialized")
			}
			return
		}
		if logger == nil {
			if logger != nil {
				logger.Warn("web agent client unavailable: logger not initialized")
			}
			return
		}
		getToken := func() string {
			if chunkParser == nil {
				return ""
			}
			return chunkParser.GetToken()
		}
		webAgentClient = agent.NewAgentClient(cfg, *logger, getToken)
	})
	return webAgentClient
}

// registerWebAgents registers WebAgentB instances for websearch and read_url tools.
func registerWebAgents() {
	webAgentsOnce.Do(func() {
		client := getWebAgentClient()
		// Register rag_search agent
		agent.Register("rag_search", agent.NewWebAgentB(client, ragSearchSysPrompt))
		// Register websearch agent
		agent.Register("websearch", agent.NewWebAgentB(client, webSearchSysPrompt))
		// Register read_url agent
		agent.Register("read_url", agent.NewWebAgentB(client, readURLSysPrompt))
		// Register summarize_chat agent
		agent.Register("summarize_chat", agent.NewWebAgentB(client, summarySysPrompt))
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

/*
consider cases:
- append mode (treat it like a journal appendix)
- replace mode (new info/mind invalidates old ones)
also:
- some writing can be done without consideration of previous data;
- others do;
*/
func memorise(args map[string]string) []byte {
	agent := cfg.AssistantRole
	if len(args) < 2 {
		msg := "not enough args to call memorise tool; need topic and data to remember"
		logger.Error(msg)
		return []byte(msg)
	}
	memory := &models.Memory{
		Agent:     agent,
		Topic:     args["topic"],
		Mind:      args["data"],
		UpdatedAt: time.Now(),
		CreatedAt: time.Now(),
	}
	if _, err := store.Memorise(memory); err != nil {
		logger.Error("failed to save memory", "err", err, "memoory", memory)
		return []byte("failed to save info")
	}
	msg := "info saved under the topic:" + args["topic"]
	return []byte(msg)
}

func recall(args map[string]string) []byte {
	agent := cfg.AssistantRole
	if len(args) < 1 {
		logger.Warn("not enough args to call recall tool")
		return nil
	}
	mind, err := store.Recall(agent, args["topic"])
	if err != nil {
		msg := fmt.Sprintf("failed to recall; error: %v; args: %v", err, args)
		logger.Error(msg)
		return []byte(msg)
	}
	answer := fmt.Sprintf("under the topic: %s is stored:\n%s", args["topic"], mind)
	return []byte(answer)
}

func recallTopics(args map[string]string) []byte {
	agent := cfg.AssistantRole
	topics, err := store.RecallTopics(agent)
	if err != nil {
		logger.Error("failed to use tool", "error", err, "args", args)
		return nil
	}
	joinedS := strings.Join(topics, ";")
	return []byte(joinedS)
}

// File Manipulation Tools
func fileCreate(args map[string]string) []byte {
	path, ok := args["path"]
	if !ok || path == "" {
		msg := "path not provided to file_create tool"
		logger.Error(msg)
		return []byte(msg)
	}
	path = resolvePath(path)
	content, ok := args["content"]
	if !ok {
		content = ""
	}
	if err := writeStringToFile(path, content); err != nil {
		msg := "failed to create file; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	msg := "file created successfully at " + path
	return []byte(msg)
}

func fileRead(args map[string]string) []byte {
	path, ok := args["path"]
	if !ok || path == "" {
		msg := "path not provided to file_read tool"
		logger.Error(msg)
		return []byte(msg)
	}
	path = resolvePath(path)
	content, err := readStringFromFile(path)
	if err != nil {
		msg := "failed to read file; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	result := map[string]string{
		"content": content,
		"path":    path,
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		msg := "failed to marshal result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	return jsonResult
}

func fileReadImage(args map[string]string) []byte {
	path, ok := args["path"]
	if !ok || path == "" {
		msg := "path not provided to file_read_image tool"
		logger.Error(msg)
		return []byte(msg)
	}
	path = resolvePath(path)
	dataURL, err := models.CreateImageURLFromPath(path)
	if err != nil {
		msg := "failed to read image; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	// result := map[string]any{
	// 	"type": "multimodal_content",
	// 	"parts": []map[string]string{
	// 		{"type": "text", "text": "Image at " + path},
	// 		{"type": "image_url", "url": dataURL},
	// 	},
	// }
	result := models.MultimodalToolResp{
		Type: "multimodal_content",
		Parts: []map[string]string{
			{"type": "text", "text": "Image at " + path},
			{"type": "image_url", "url": dataURL},
		},
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		msg := "failed to marshal result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	return jsonResult
}

func fileWrite(args map[string]string) []byte {
	path, ok := args["path"]
	if !ok || path == "" {
		msg := "path not provided to file_write tool"
		logger.Error(msg)
		return []byte(msg)
	}
	path = resolvePath(path)
	content, ok := args["content"]
	if !ok {
		content = ""
	}
	if err := writeStringToFile(path, content); err != nil {
		msg := "failed to write to file; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	msg := "file written successfully at " + path
	return []byte(msg)
}

func fileWriteAppend(args map[string]string) []byte {
	path, ok := args["path"]
	if !ok || path == "" {
		msg := "path not provided to file_write_append tool"
		logger.Error(msg)
		return []byte(msg)
	}
	path = resolvePath(path)
	content, ok := args["content"]
	if !ok {
		content = ""
	}
	if err := appendStringToFile(path, content); err != nil {
		msg := "failed to append to file; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	msg := "file written successfully at " + path
	return []byte(msg)
}

func fileEdit(args map[string]string) []byte {
	path, ok := args["path"]
	if !ok || path == "" {
		msg := "path not provided to file_edit tool"
		logger.Error(msg)
		return []byte(msg)
	}
	path = resolvePath(path)
	oldString, ok := args["oldString"]
	if !ok || oldString == "" {
		msg := "oldString not provided to file_edit tool"
		logger.Error(msg)
		return []byte(msg)
	}
	newString, ok := args["newString"]
	if !ok {
		newString = ""
	}
	lineNumberStr, hasLineNumber := args["lineNumber"]
	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		msg := "failed to read file: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	fileContent := string(content)
	var replacementCount int
	if hasLineNumber && lineNumberStr != "" {
		// Line-number based edit
		lineNum, err := strconv.Atoi(lineNumberStr)
		if err != nil {
			msg := "invalid lineNumber: must be a valid integer"
			logger.Error(msg)
			return []byte(msg)
		}
		lines := strings.Split(fileContent, "\n")
		if lineNum < 1 || lineNum > len(lines) {
			msg := fmt.Sprintf("lineNumber %d out of range (file has %d lines)", lineNum, len(lines))
			logger.Error(msg)
			return []byte(msg)
		}
		// Find oldString in the specific line
		targetLine := lines[lineNum-1]
		if !strings.Contains(targetLine, oldString) {
			msg := fmt.Sprintf("oldString not found on line %d", lineNum)
			logger.Error(msg)
			return []byte(msg)
		}
		lines[lineNum-1] = strings.Replace(targetLine, oldString, newString, 1)
		replacementCount = 1
		fileContent = strings.Join(lines, "\n")
	} else {
		// Replace all occurrences
		if !strings.Contains(fileContent, oldString) {
			msg := "oldString not found in file"
			logger.Error(msg)
			return []byte(msg)
		}
		fileContent = strings.ReplaceAll(fileContent, oldString, newString)
		replacementCount = strings.Count(fileContent, newString)
	}
	if err := os.WriteFile(path, []byte(fileContent), 0644); err != nil {
		msg := "failed to write file: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	msg := fmt.Sprintf("file edited successfully at %s (%d replacement(s))", path, replacementCount)
	return []byte(msg)
}

func fileDelete(args map[string]string) []byte {
	path, ok := args["path"]
	if !ok || path == "" {
		msg := "path not provided to file_delete tool"
		logger.Error(msg)
		return []byte(msg)
	}
	path = resolvePath(path)
	if err := removeFile(path); err != nil {
		msg := "failed to delete file; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	msg := "file deleted successfully at " + path
	return []byte(msg)
}

func fileMove(args map[string]string) []byte {
	src, ok := args["src"]
	if !ok || src == "" {
		msg := "source path not provided to file_move tool"
		logger.Error(msg)
		return []byte(msg)
	}
	src = resolvePath(src)
	dst, ok := args["dst"]
	if !ok || dst == "" {
		msg := "destination path not provided to file_move tool"
		logger.Error(msg)
		return []byte(msg)
	}
	dst = resolvePath(dst)
	if err := moveFile(src, dst); err != nil {
		msg := "failed to move file; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	msg := fmt.Sprintf("file moved successfully from %s to %s", src, dst)
	return []byte(msg)
}

func fileCopy(args map[string]string) []byte {
	src, ok := args["src"]
	if !ok || src == "" {
		msg := "source path not provided to file_copy tool"
		logger.Error(msg)
		return []byte(msg)
	}
	src = resolvePath(src)
	dst, ok := args["dst"]
	if !ok || dst == "" {
		msg := "destination path not provided to file_copy tool"
		logger.Error(msg)
		return []byte(msg)
	}
	dst = resolvePath(dst)
	if err := copyFile(src, dst); err != nil {
		msg := "failed to copy file; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	msg := fmt.Sprintf("file copied successfully from %s to %s", src, dst)
	return []byte(msg)
}

func fileList(args map[string]string) []byte {
	path, ok := args["path"]
	if !ok || path == "" {
		path = "." // default to current directory
	}
	path = resolvePath(path)
	files, err := listDirectory(path)
	if err != nil {
		msg := "failed to list directory; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	result := map[string]interface{}{
		"directory": path,
		"files":     files,
	}
	jsonResult, err := json.Marshal(result)
	if err != nil {
		msg := "failed to marshal result; error: " + err.Error()
		logger.Error(msg)
		return []byte(msg)
	}
	return jsonResult
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

// Command Execution Tool
func executeCommand(args map[string]string) []byte {
	commandStr := args["command"]
	if commandStr == "" {
		msg := "command not provided to execute_command tool"
		logger.Error(msg)
		return []byte(msg)
	}
	// Handle commands passed as single string with spaces (e.g., "go run main.go" or "cd /tmp")
	// Split into base command and arguments
	parts := strings.Fields(commandStr)
	if len(parts) == 0 {
		msg := "command not provided to execute_command tool"
		logger.Error(msg)
		return []byte(msg)
	}
	command := parts[0]
	cmdArgs := parts[1:]
	if !isCommandAllowed(command, cmdArgs...) {
		msg := fmt.Sprintf("command '%s' is not allowed", command)
		logger.Error(msg)
		return []byte(msg)
	}
	// Special handling for cd command - update FilePickerDir
	if command == "cd" {
		return handleCdCommand(cmdArgs)
	}
	// Execute with timeout for safety
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, cmdArgs...)
	cmd.Dir = cfg.FilePickerDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := fmt.Sprintf("command '%s' failed; error: %v; output: %s", command, err, string(output))
		logger.Error(msg)
		return []byte(msg)
	}
	// Check if output is empty and return success message
	if len(output) == 0 {
		successMsg := fmt.Sprintf("command '%s' executed successfully and exited with code 0", commandStr)
		return []byte(successMsg)
	}
	return output
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

var fnMap = map[string]fnSig{
	"recall":            recall,
	"recall_topics":     recallTopics,
	"memorise":          memorise,
	"rag_search":        ragsearch,
	"websearch":         websearch,
	"websearch_raw":     websearchRaw,
	"read_url":          readURL,
	"read_url_raw":      readURLRaw,
	"file_create":       fileCreate,
	"file_read":         fileRead,
	"file_read_image":   fileReadImage,
	"file_write":        fileWrite,
	"file_write_append": fileWriteAppend,
	"file_edit":         fileEdit,
	"file_delete":       fileDelete,
	"file_move":         fileMove,
	"file_copy":         fileCopy,
	"file_list":         fileList,
	"execute_command":   executeCommand,
	"todo_create":       todoCreate,
	"todo_read":         todoRead,
	"todo_update":       todoUpdate,
	"todo_delete":       todoDelete,
	"summarize_chat":    summarizeChat,
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
	// memorise
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "memorise",
			Description: "Save topic-data in key-value cache. Use when asked to remember something/keep in mind.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"topic", "data"},
				Properties: map[string]models.ToolArgProps{
					"topic": models.ToolArgProps{
						Type:        "string",
						Description: "topic is the key under which data is saved",
					},
					"data": models.ToolArgProps{
						Type:        "string",
						Description: "data is the value that is saved under the topic-key",
					},
				},
			},
		},
	},
	// recall
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "recall",
			Description: "Recall topic-data from key-value cache. Use when precise info about the topic is needed.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"topic"},
				Properties: map[string]models.ToolArgProps{
					"topic": models.ToolArgProps{
						Type:        "string",
						Description: "topic is the key to recall data from",
					},
				},
			},
		},
	},
	// recall_topics
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "recall_topics",
			Description: "Recall all topics from key-value cache. Use when need to know what topics are currently stored in memory.",
			Parameters: models.ToolFuncParams{
				Type:       "object",
				Required:   []string{},
				Properties: map[string]models.ToolArgProps{},
			},
		},
	},
	// file_create
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "file_create",
			Description: "Create a new file with specified content. Use when you need to create a new file.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]models.ToolArgProps{
					"path": models.ToolArgProps{
						Type:        "string",
						Description: "path where the file should be created",
					},
					"content": models.ToolArgProps{
						Type:        "string",
						Description: "content to write to the file (optional, defaults to empty string)",
					},
				},
			},
		},
	},
	// file_read
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "file_read",
			Description: "Read the content of a file. Use when you need to see the content of a file.",
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
	// file_read_image
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "file_read_image",
			Description: "Read an image file and return it for multimodal LLM viewing. Supports png, jpg, jpeg, gif, webp formats. Use when you need the LLM to see and analyze an image.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]models.ToolArgProps{
					"path": models.ToolArgProps{
						Type:        "string",
						Description: "path of the image file to read",
					},
				},
			},
		},
	},
	// file_write
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "file_write",
			Description: "Write content to a file. Will overwrite any content present.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"path", "content"},
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
	// file_write_append
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "file_write_append",
			Description: "Append content to a file.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"path", "content"},
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
	// file_edit
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "file_edit",
			Description: "Edit a specific section of a file by replacing oldString with newString. Use for targeted changes without rewriting the entire file.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"path", "oldString", "newString"},
				Properties: map[string]models.ToolArgProps{
					"path": models.ToolArgProps{
						Type:        "string",
						Description: "path of the file to edit",
					},
					"oldString": models.ToolArgProps{
						Type:        "string",
						Description: "the exact string to find and replace",
					},
					"newString": models.ToolArgProps{
						Type:        "string",
						Description: "the string to replace oldString with",
					},
					"lineNumber": models.ToolArgProps{
						Type:        "string",
						Description: "optional line number (1-indexed) to edit - if provided, only that line is edited",
					},
				},
			},
		},
	},
	// file_delete
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "file_delete",
			Description: "Delete a file. Use when you need to remove a file.",
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
	// file_move
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "file_move",
			Description: "Move a file from one location to another. Use when you need to relocate a file.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"src", "dst"},
				Properties: map[string]models.ToolArgProps{
					"src": models.ToolArgProps{
						Type:        "string",
						Description: "source path of the file to move",
					},
					"dst": models.ToolArgProps{
						Type:        "string",
						Description: "destination path where the file should be moved",
					},
				},
			},
		},
	},
	// file_copy
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "file_copy",
			Description: "Copy a file from one location to another. Use when you need to duplicate a file.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"src", "dst"},
				Properties: map[string]models.ToolArgProps{
					"src": models.ToolArgProps{
						Type:        "string",
						Description: "source path of the file to copy",
					},
					"dst": models.ToolArgProps{
						Type:        "string",
						Description: "destination path where the file should be copied",
					},
				},
			},
		},
	},
	// file_list
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "file_list",
			Description: "List files and directories in a directory. Use when you need to see what files are in a directory.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"path": models.ToolArgProps{
						Type:        "string",
						Description: "path of the directory to list (optional, defaults to current directory)",
					},
				},
			},
		},
	},
	// execute_command
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "execute_command",
			Description: "Execute a shell command safely. Use when you need to run system commands like cd grep sed awk find cat head tail sort uniq wc ls echo cut tr cp mv rm mkdir rmdir pwd df free ps top du whoami date uname go git. Git is allowed for read-only operations: status, log, diff, show, branch, reflog, rev-parse, shortlog, describe. Use 'cd /path' to change working directory.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"command"},
				Properties: map[string]models.ToolArgProps{
					"command": models.ToolArgProps{
						Type:        "string",
						Description: "command to execute with arguments (e.g., 'go run main.go', 'ls -la /tmp', 'cd /home/user'). Use a single string; arguments should be space-separated after the command.",
					},
				},
			},
		},
	},
	// todo_create
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "todo_create",
			Description: "Create a new todo item with a task. Returns the created todo with its ID.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"task"},
				Properties: map[string]models.ToolArgProps{
					"task": models.ToolArgProps{
						Type:        "string",
						Description: "the task description to add to the todo list",
					},
				},
			},
		},
	},
	// todo_read
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "todo_read",
			Description: "Read todo items. Without ID returns all todos, with ID returns specific todo.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{},
				Properties: map[string]models.ToolArgProps{
					"id": models.ToolArgProps{
						Type:        "string",
						Description: "optional id of the specific todo item to read",
					},
				},
			},
		},
	},
	// todo_update
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "todo_update",
			Description: "Update a todo item by ID with new task or status. Status must be one of: pending, in_progress, completed.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"id"},
				Properties: map[string]models.ToolArgProps{
					"id": models.ToolArgProps{
						Type:        "string",
						Description: "id of the todo item to update",
					},
					"task": models.ToolArgProps{
						Type:        "string",
						Description: "new task description (optional)",
					},
					"status": models.ToolArgProps{
						Type:        "string",
						Description: "new status: pending, in_progress, or completed (optional)",
					},
				},
			},
		},
	},
	// todo_delete
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "todo_delete",
			Description: "Delete a todo item by ID. Returns success message.",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"id"},
				Properties: map[string]models.ToolArgProps{
					"id": models.ToolArgProps{
						Type:        "string",
						Description: "id of the todo item to delete",
					},
				},
			},
		},
	},
}
