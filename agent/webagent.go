package agent

import (
	"fmt"
	"log/slog"
)

// WebAgentB is a simple agent that applies formatting functions
type WebAgentB struct {
	*AgentClient
	sysprompt string
	log       slog.Logger
}

// NewWebAgentB creates a WebAgentB that uses the given formatting function
func NewWebAgentB(sysprompt string) *WebAgentB {
	return &WebAgentB{sysprompt: sysprompt}
}

// Process applies the formatting function to raw output
func (a *WebAgentB) Process(args map[string]string, rawOutput []byte) []byte {
	msg, err := a.FormMsg(a.sysprompt,
		fmt.Sprintf("request:\n%+v\ntool response:\n%v", args, string(rawOutput)))
	if err != nil {
		a.log.Error("failed to process the request", "error", err)
		return []byte("failed to process the request; err: " + err.Error())
	}
	resp, err := a.LLMRequest(msg)
	if err != nil {
		a.log.Error("failed to process the request", "error", err)
		return []byte("failed to process the request; err: " + err.Error())
	}
	return resp
}
