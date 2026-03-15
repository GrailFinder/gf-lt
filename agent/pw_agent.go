package agent

import (
	"encoding/json"
	"gf-lt/models"
	"strings"
)

// PWAgent: is AgenterA type agent (enclosed with tool chaining)
// sysprompt explain tools and how to plan for execution
type PWAgent struct {
	*AgentClient
	sysprompt string
}

// NewPWAgent creates a PWAgent with the given client and system prompt
func NewPWAgent(client *AgentClient, sysprompt string) *PWAgent {
	return &PWAgent{AgentClient: client, sysprompt: sysprompt}
}

// SetTools sets the tools available to the agent
func (a *PWAgent) SetTools(tools []models.Tool) {
	a.tools = tools
}

func (a *PWAgent) ProcessTask(task string) []byte {
	req, err := a.FormFirstMsg(a.sysprompt, task)
	if err != nil {
		a.Log().Error("PWAgent failed to process the request", "error", err)
		return []byte("PWAgent failed to process the request; err: " + err.Error())
	}
	toolCallLimit := 10
	for i := 0; i < toolCallLimit; i++ {
		resp, err := a.LLMRequest(req)
		if err != nil {
			a.Log().Error("failed to process the request", "error", err)
			return []byte("failed to process the request; err: " + err.Error())
		}
		execTool, toolCallID, hasToolCall := findToolCall(resp)
		if !hasToolCall {
			return resp
		}

		a.setToolCallOnLastMessage(resp, toolCallID)

		toolResp := string(execTool())
		req, err = a.FormMsgWithToolCallID(toolResp, toolCallID)
		if err != nil {
			a.Log().Error("failed to form next message", "error", err)
			return []byte("failed to form next message; err: " + err.Error())
		}
	}
	return nil
}

func (a *PWAgent) setToolCallOnLastMessage(resp []byte, toolCallID string) {
	if toolCallID == "" {
		return
	}
	var genericResp map[string]interface{}
	if err := json.Unmarshal(resp, &genericResp); err != nil {
		return
	}
	var name string
	var args map[string]string
	if choices, ok := genericResp["choices"].([]interface{}); ok && len(choices) > 0 {
		if firstChoice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := firstChoice["message"].(map[string]interface{}); ok {
				if toolCalls, ok := message["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
					if tc, ok := toolCalls[0].(map[string]interface{}); ok {
						if fn, ok := tc["function"].(map[string]interface{}); ok {
							name, _ = fn["name"].(string)
							argsStr, _ := fn["arguments"].(string)
							_ = json.Unmarshal([]byte(argsStr), &args)
						}
					}
				}
			}
		}
	}
	if name == "" {
		content, _ := genericResp["content"].(string)
		name = extractToolNameFromText(content)
	}
	lastIdx := len(a.chatBody.Messages) - 1
	if lastIdx >= 0 {
		a.chatBody.Messages[lastIdx].ToolCallID = toolCallID
		if name != "" {
			argsJSON, _ := json.Marshal(args)
			a.chatBody.Messages[lastIdx].ToolCall = &models.ToolCall{
				ID:   toolCallID,
				Name: name,
				Args: string(argsJSON),
			}
		}
	}
}

func extractToolNameFromText(text string) string {
	jsStr := toolCallRE.FindString(text)
	if jsStr == "" {
		return ""
	}
	jsStr = strings.TrimSpace(jsStr)
	jsStr = strings.TrimPrefix(jsStr, "__tool_call__")
	jsStr = strings.TrimSuffix(jsStr, "__tool_call__")
	jsStr = strings.TrimSpace(jsStr)
	start := strings.Index(jsStr, "{")
	end := strings.LastIndex(jsStr, "}")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	jsStr = jsStr[start : end+1]
	var fc models.FuncCall
	if err := json.Unmarshal([]byte(jsStr), &fc); err != nil {
		return ""
	}
	return fc.Name
}
