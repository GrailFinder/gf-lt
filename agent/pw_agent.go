package agent

// PWAgent: is AgenterA type agent (enclosed with tool chaining)
// sysprompt explain tools and how to plan for execution
type PWAgent struct {
	*AgentClient
	sysprompt string
}

// NewWebAgentB creates a WebAgentB that uses the given formatting function
func NewPWAgent(client *AgentClient, sysprompt string) *PWAgent {
	return &PWAgent{AgentClient: client, sysprompt: sysprompt}
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
		toolCall, hasToolCall := findToolCall(resp)
		if !hasToolCall {
			return resp
		}
		// check resp for tool calls
		// make tool call
		// add tool call resp to body
		// send new request too lmm
		tooResp := toolCall(resp)
		req, err = a.FormMsg(toolResp)
	}
	return nil
}
