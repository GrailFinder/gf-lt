package main

import (
	"context"
	"encoding/json"
	"fmt"
	"gf-lt/config"
	"gf-lt/extra"
	"gf-lt/models"
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
"args": ["topic", "info"],
"when_to_use": "when asked to memorise something"
},
{
"name":"recall_topics",
"args": [],
"when_to_use": "to see what topics are saved in memory"
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
	toolCard = &models.CharCard{
		SysPrompt: toolSysMsg,
		FirstMsg:  defaultFirstMsg,
		Role:      "",
		FilePath:  "",
	}
	// sysMap    = map[string]string{"basic_sys": basicSysMsg, "tool_sys": toolSysMsg}
	sysMap    = map[string]*models.CharCard{"basic_sys": basicCard, "tool_sys": toolCard}
	sysLabels = []string{"basic_sys", "tool_sys"}
)

func populateTools(cfg config.Config) {
	// if we have access to some server with funcs we can populate funcs (tools|toolbelt?) with it
	// there must be a better way
	if cfg.SearchAPI == "" || cfg.SearchDescribe == "" {
		return
	}
	resp, err := httpClient.Get(cfg.SearchDescribe)
	if err != nil {
		logger.Error("failed to get websearch tool description",
			"link", cfg.SearchDescribe, "error", err)
		return
	}
	descResp := models.Tool{}
	if err := json.NewDecoder(resp.Body).Decode(&descResp); err != nil {
		logger.Error("failed to unmarshal websearch tool description",
			"link", cfg.SearchDescribe, "error", err)
		return
	}
	fnMap["web_search"] = websearch
	baseTools = append(baseTools, descResp)
	logger.Info("added web_search tool", "tool", descResp)
	return
}

// {"type":"function","function":{"name":"web_search","description":"Perform a web search to find information on varioust topics","parameters":{"type":"object","properties":{"num_results":{"type":"integer","description":"Maximum number of results to return (default: 10)"},"query":{"type":"string","description":"The search query to find information about"},"search_type":{"type":"string","description":"Type of search to perform: 'api' for SearXNG API search or 'scraper' for web scraping (default: 'scraper')"}},"required":["query"]}}}

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
	// // external
	// payload, err := json.Marshal(args)
	// if err != nil {
	// 	logger.Error("failed to marshal web_search arguments", "error", err)
	// 	msg := fmt.Sprintf("failed to marshal web_search arguments; error: %s\n", err)
	// 	return []byte(msg)
	// }
	// req, err := http.NewRequest("POST", cfg.SearchAPI, bytes.NewReader(payload))
	// if err != nil {
	// 	logger.Error("failed to build an http request", "error", err)
	// 	msg := fmt.Sprintf("failed to build an http request; error: %s\n", err)
	// 	return []byte(msg)
	// }
	// resp, err := httpClient.Do(req)
	// if err != nil {
	// 	logger.Error("failed to execute http request", "error", err)
	// 	msg := fmt.Sprintf("failed to execute http request; error: %s\n", err)
	// 	return []byte(msg)
	// }
	// defer resp.Body.Close()
	// data, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	logger.Error("failed to read response body", "error", err)
	// 	msg := fmt.Sprintf("failed to read response body; error: %s\n", err)
	// 	return []byte(msg)
	// }
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

// func fullMemoryLoad() {}

type fnSig func(map[string]string) []byte

var fnMap = map[string]fnSig{
	"recall":        recall,
	"recall_topics": recallTopics,
	"memorise":      memorise,
	"websearch":     websearch,
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
}
