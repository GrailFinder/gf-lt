package main

import (
	"elefant/models"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	// TODO: form that message based on existing funcs
	basicSysMsg = `Large Language Model that helps user with any of his requests.`
	toolCallRE  = regexp.MustCompile(`__tool_call__\s*([\s\S]*?)__tool_call__`)
	toolSysMsg  = `You're a helpful assistant.
# Tools
You can do functions call if needed.
Your current tools:
<tools>
[
{
"name":"recall",
"args": "topic",
"when_to_use": "when asked about topic that user previously asked to memorise"
},
{
"name":"memorise",
"args": ["topic", "info"],
"when_to_use": "when asked to memorise something"
},
{
"name":"recall_topics",
"args": null,
"when_to_use": "to see what topics are saved in memory"
}
]
</tools>
To make a function call return a json object within __tool_call__ tags;
Example:
__tool_call__
{
"name":"recall",
"args": "Adam"
}
__tool_call__
Tool call is addressed to the tool agent, avoid sending more info than tool call itself, while making a call.
When done right, tool call will be delivered to the tool agent. tool agent will respond with the results of the call.
After that you are free to respond to the user.
`
	systemMsg = toolSysMsg
	sysMap    = map[string]string{"basic_sys": basicSysMsg, "tool_sys": toolSysMsg}
	sysLabels = []string{"cancel", "basic_sys", "tool_sys"}
)

/*
consider cases:
- append mode (treat it like a journal appendix)
- replace mode (new info/mind invalidates old ones)
also:
- some writing can be done without consideration of previous data;
- others do;
*/
func memorise(args ...string) []byte {
	agent := cfg.AssistantRole
	if len(args) < 2 {
		msg := "not enough args to call memorise tool; need topic and data to remember"
		logger.Error(msg)
		return []byte(msg)
	}
	memory := &models.Memory{
		Agent:     agent,
		Topic:     args[0],
		Mind:      args[1],
		UpdatedAt: time.Now(),
	}
	if _, err := store.Memorise(memory); err != nil {
		logger.Error("failed to save memory", "err", err, "memoory", memory)
		return []byte("failed to save info")
	}
	msg := "info saved under the topic:" + args[0]
	return []byte(msg)
}

func recall(args ...string) []byte {
	agent := cfg.AssistantRole
	if len(args) < 1 {
		logger.Warn("not enough args to call recall tool")
		return nil
	}
	mind, err := store.Recall(agent, args[0])
	if err != nil {
		msg := fmt.Sprintf("failed to recall; error: %v; args: %v", err, args)
		logger.Error(msg)
		return []byte(msg)
	}
	return []byte(mind)
}

func recallTopics(args ...string) []byte {
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

type fnSig func(...string) []byte

var fnMap = map[string]fnSig{
	"recall":        recall,
	"recall_topics": recallTopics,
	"memorise":      memorise,
}
