package main

import (
	"context"
	"encoding/json"
	"fmt"
	"gf-lt/extra"
	"gf-lt/models"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	toolCallRE         = regexp.MustCompile(`__tool_call__\s*([\s\S]*?)__tool_call__`)
	quotesRE           = regexp.MustCompile(`(".*?")`)
	starRE             = regexp.MustCompile(`(\*.*?\*)`)
	thinkRE            = regexp.MustCompile(`(<think>\s*([\s\S]*?)</think>)`)
	codeBlockRE        = regexp.MustCompile(`(?s)\x60{3}(?:.*?)\n(.*?)\n\s*\x60{3}\s*`)
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
"when_to_use": "when asked to search the web for information; limit is optional (default 3)"
},
{
"name":"file_create",
"args": ["path", "content"],
"when_to_use": "when asked to create a new file with optional content"
},
{
"name":"file_read",
"args": ["path"],
"when_to_use": "when asked to read the content of a file"
},
{
"name":"file_write",
"args": ["path", "content", "mode"],
"when_to_use": "when asked to write content to a file; mode is optional (overwrite or append, default: overwrite)"
},
{
"name":"file_delete",
"args": ["path"],
"when_to_use": "when asked to delete a file"
},
{
"name":"file_move",
"args": ["src", "dst"],
"when_to_use": "when asked to move a file from source to destination"
},
{
"name":"file_copy",
"args": ["src", "dst"],
"when_to_use": "when asked to copy a file from source to destination"
},
{
"name":"file_list",
"args": ["path"],
"when_to_use": "when asked to list files in a directory; path is optional (default: current directory)"
},
{
"name":"execute_command",
"args": ["command", "args"],
"when_to_use": "when asked to execute a system command; args is optional; allowed commands: grep, sed, awk, find, cat, head, tail, sort, uniq, wc, ls, echo, cut, tr, cp, mv, rm, mkdir, rmdir, pwd, df, free, ps, top, du, whoami, date, uname"
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
	basicCard = &models.CharCard{
		SysPrompt: basicSysMsg,
		FirstMsg:  defaultFirstMsg,
		Role:      "",
		FilePath:  "",
	}
	// toolCard = &models.CharCard{
	// 	SysPrompt: toolSysMsg,
	// 	FirstMsg:  defaultFirstMsg,
	// 	Role:      "",
	// 	FilePath:  "",
	// }
	// sysMap    = map[string]string{"basic_sys": basicSysMsg, "tool_sys": toolSysMsg}
	sysMap    = map[string]*models.CharCard{"basic_sys": basicCard}
	sysLabels = []string{"basic_sys"}
)

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
	resp, err := extra.WebSearcher.Search(context.Background(), query, limit)
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

// retrieves url content (text)
func readURL(args map[string]string) []byte {
	// make http request return bytes
	link, ok := args["url"]
	if !ok || link == "" {
		msg := "linknot provided to read_url tool"
		logger.Error(msg)
		return []byte(msg)
	}
	resp, err := extra.WebSearcher.RetrieveFromLink(context.Background(), link)
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

func fileWrite(args map[string]string) []byte {
	path, ok := args["path"]
	if !ok || path == "" {
		msg := "path not provided to file_write tool"
		logger.Error(msg)
		return []byte(msg)
	}

	content, ok := args["content"]
	if !ok {
		content = ""
	}

	mode, ok := args["mode"]
	if !ok || mode == "" {
		mode = "overwrite"
	}

	switch mode {
	case "overwrite":
		if err := writeStringToFile(path, content); err != nil {
			msg := "failed to write to file; error: " + err.Error()
			logger.Error(msg)
			return []byte(msg)
		}
	case "append":
		if err := appendStringToFile(path, content); err != nil {
			msg := "failed to append to file; error: " + err.Error()
			logger.Error(msg)
			return []byte(msg)
		}
	default:
		msg := "invalid mode; use 'overwrite' or 'append'"
		logger.Error(msg)
		return []byte(msg)
	}

	msg := "file written successfully at " + path
	return []byte(msg)
}

func fileDelete(args map[string]string) []byte {
	path, ok := args["path"]
	if !ok || path == "" {
		msg := "path not provided to file_delete tool"
		logger.Error(msg)
		return []byte(msg)
	}

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

	dst, ok := args["dst"]
	if !ok || dst == "" {
		msg := "destination path not provided to file_move tool"
		logger.Error(msg)
		return []byte(msg)
	}

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

	dst, ok := args["dst"]
	if !ok || dst == "" {
		msg := "destination path not provided to file_copy tool"
		logger.Error(msg)
		return []byte(msg)
	}

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
	command, ok := args["command"]
	if !ok || command == "" {
		msg := "command not provided to execute_command tool"
		logger.Error(msg)
		return []byte(msg)
	}

	if !isCommandAllowed(command) {
		msg := fmt.Sprintf("command '%s' is not allowed", command)
		logger.Error(msg)
		return []byte(msg)
	}

	// Get arguments - handle both single arg and multiple args
	var cmdArgs []string
	if args["args"] != "" {
		// If args is provided as a single string, split by spaces
		cmdArgs = strings.Fields(args["args"])
	} else {
		// If individual args are provided, collect them
		argNum := 1
		for {
			argKey := fmt.Sprintf("arg%d", argNum)
			if argValue, exists := args[argKey]; exists && argValue != "" {
				cmdArgs = append(cmdArgs, argValue)
			} else {
				break
			}
			argNum++
		}
	}

	// Execute with timeout for safety
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, cmdArgs...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := fmt.Sprintf("command '%s' failed; error: %v; output: %s", command, err, string(output))
		logger.Error(msg)
		return []byte(msg)
	}

	return output
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

// Global todo list storage
var globalTodoList = TodoList{
	Items: []TodoItem{},
}

func getTodoList() TodoList {
	return globalTodoList
}

func setTodoList(todoList TodoList) {
	globalTodoList = todoList
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
	id, ok := args["id"]
	if ok && id != "" {
		// Find specific todo by ID
		for _, item := range globalTodoList.Items {
			if item.ID == id {
				result := map[string]interface{}{
					"todo": item,
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

	// Return all todos if no ID specified
	result := map[string]interface{}{
		"todos": globalTodoList.Items,
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

func isCommandAllowed(command string) bool {
	allowedCommands := map[string]bool{
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
	}
	return allowedCommands[command]
}

type fnSig func(map[string]string) []byte

var fnMap = map[string]fnSig{
	"recall":          recall,
	"recall_topics":   recallTopics,
	"memorise":        memorise,
	"websearch":       websearch,
	"read_url":        readURL,
	"file_create":     fileCreate,
	"file_read":       fileRead,
	"file_write":      fileWrite,
	"file_delete":     fileDelete,
	"file_move":       fileMove,
	"file_copy":       fileCopy,
	"file_list":       fileList,
	"execute_command": executeCommand,
	"todo_create":     todoCreate,
	"todo_read":       todoRead,
	"todo_update":     todoUpdate,
	"todo_delete":     todoDelete,
}

// openai style def
var baseTools = []models.Tool{
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
			Description: "Retrieves text content of given link.",
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

	// file_write
	models.Tool{
		Type: "function",
		Function: models.ToolFunc{
			Name:        "file_write",
			Description: "Write content to a file. Use when you want to create or modify a file (overwrite or append).",
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
					"mode": models.ToolArgProps{
						Type:        "string",
						Description: "write mode: 'overwrite' to replace entire file content, 'append' to add to the end (defaults to 'overwrite')",
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
			Description: "Execute a shell command safely. Use when you need to run system commands like grep sed awk find cat head tail sort uniq wc ls echo cut tr cp mv rm mkdir rmdir pwd df free ps top du whoami date uname",
			Parameters: models.ToolFuncParams{
				Type:     "object",
				Required: []string{"command"},
				Properties: map[string]models.ToolArgProps{
					"command": models.ToolArgProps{
						Type:        "string",
						Description: "command to execute (only commands from whitelist are allowed: grep sed awk find cat head tail sort uniq wc ls echo cut tr cp mv rm mkdir rmdir pwd df free ps top du whoami date uname",
					},
					"args": models.ToolArgProps{
						Type:        "string",
						Description: "command arguments as a single string (e.g., '-la {path}')",
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
						Description: "new status for the todo: pending, in_progress, or completed (optional)",
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
