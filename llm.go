package main

import (
	"bytes"
	"encoding/json"
	"gf-lt/models"
	"io"
	"strings"
)

type ChunkParser interface {
	ParseChunk([]byte) (*models.TextChunk, error)
	FormMsg(msg, role string, cont bool) (io.Reader, error)
	GetToken() string
}

func choseChunkParser() {
	chunkParser = LlamaCPPeer{}
	switch cfg.CurrentAPI {
	case "http://localhost:8080/completion":
		chunkParser = LlamaCPPeer{}
		logger.Debug("chosen llamacppeer", "link", cfg.CurrentAPI)
		return
	case "http://localhost:8080/v1/chat/completions":
		chunkParser = OpenAIer{}
		logger.Debug("chosen openair", "link", cfg.CurrentAPI)
		return
	case "https://api.deepseek.com/beta/completions":
		chunkParser = DeepSeekerCompletion{}
		logger.Debug("chosen deepseekercompletio", "link", cfg.CurrentAPI)
		return
	case "https://api.deepseek.com/chat/completions":
		chunkParser = DeepSeekerChat{}
		logger.Debug("chosen deepseekerchat", "link", cfg.CurrentAPI)
		return
	case "https://openrouter.ai/api/v1/completions":
		chunkParser = OpenRouterCompletion{}
		logger.Debug("chosen openroutercompletion", "link", cfg.CurrentAPI)
		return
	case "https://openrouter.ai/api/v1/chat/completions":
		chunkParser = OpenRouterChat{}
		logger.Debug("chosen openrouterchat", "link", cfg.CurrentAPI)
		return
	default:
		chunkParser = LlamaCPPeer{}
	}
}

type LlamaCPPeer struct {
}
type OpenAIer struct {
}
type DeepSeekerCompletion struct {
}
type DeepSeekerChat struct {
}
type OpenRouterCompletion struct {
	Model string
}
type OpenRouterChat struct {
	Model string
}

func (lcp LlamaCPPeer) GetToken() string {
	return ""
}

func (lcp LlamaCPPeer) FormMsg(msg, role string, resume bool) (io.Reader, error) {
	logger.Debug("formmsg llamacppeer", "link", cfg.CurrentAPI)
	if msg != "" { // otherwise let the bot to continue
		newMsg := models.RoleMsg{Role: role, Content: msg}
		chatBody.Messages = append(chatBody.Messages, newMsg)
		// if rag
		if cfg.RAGEnabled {
			ragResp, err := chatRagUse(newMsg.Content)
			if err != nil {
				logger.Error("failed to form a rag msg", "error", err)
				return nil, err
			}
			ragMsg := models.RoleMsg{Role: cfg.ToolRole, Content: ragResp}
			chatBody.Messages = append(chatBody.Messages, ragMsg)
		}
	}
	if cfg.ToolUse && !resume {
		// add to chat body
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{Role: cfg.ToolRole, Content: toolSysMsg})
	}
	messages := make([]string, len(chatBody.Messages))
	for i, m := range chatBody.Messages {
		messages[i] = m.ToPrompt()
	}
	prompt := strings.Join(messages, "\n")
	// strings builder?
	if !resume {
		botPersona := cfg.AssistantRole
		if cfg.WriteNextMsgAsCompletionAgent != "" {
			botPersona = cfg.WriteNextMsgAsCompletionAgent
		}
		botMsgStart := "\n" + botPersona + ":\n"
		prompt += botMsgStart
	}
	if cfg.ThinkUse && !cfg.ToolUse {
		prompt += "<think>"
	}
	logger.Debug("checking prompt for /completion", "tool_use", cfg.ToolUse,
		"msg", msg, "resume", resume, "prompt", prompt)
	var payload any
	payload = models.NewLCPReq(prompt, defaultLCPProps, chatBody.MakeStopSlice())
	if strings.Contains(chatBody.Model, "deepseek") { // TODO: why?
		payload = models.NewDSCompletionReq(prompt, chatBody.Model,
			defaultLCPProps["temp"], chatBody.MakeStopSlice())
	}
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (lcp LlamaCPPeer) ParseChunk(data []byte) (*models.TextChunk, error) {
	llmchunk := models.LlamaCPPResp{}
	resp := &models.TextChunk{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return nil, err
	}
	resp.Chunk = llmchunk.Content
	if llmchunk.Stop {
		if llmchunk.Content != "" {
			logger.Error("text inside of finish llmchunk", "chunk", llmchunk)
		}
		resp.Finished = true
	}
	return resp, nil
}

func (op OpenAIer) GetToken() string {
	return ""
}

func (op OpenAIer) ParseChunk(data []byte) (*models.TextChunk, error) {
	llmchunk := models.LLMRespChunk{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return nil, err
	}
	resp := &models.TextChunk{
		Chunk: llmchunk.Choices[len(llmchunk.Choices)-1].Delta.Content,
	}
	if len(llmchunk.Choices[len(llmchunk.Choices)-1].Delta.ToolCalls) > 0 {
		resp.ToolChunk = llmchunk.Choices[len(llmchunk.Choices)-1].Delta.ToolCalls[0].Function.Arguments
		fname := llmchunk.Choices[len(llmchunk.Choices)-1].Delta.ToolCalls[0].Function.Name
		if fname != "" {
			resp.FuncName = fname
		}
	}
	if llmchunk.Choices[len(llmchunk.Choices)-1].FinishReason == "stop" {
		if resp.Chunk != "" {
			logger.Error("text inside of finish llmchunk", "chunk", llmchunk)
		}
		resp.Finished = true
	}
	if resp.ToolChunk != "" {
		resp.ToolResp = true
	}
	return resp, nil
}

func (op OpenAIer) FormMsg(msg, role string, resume bool) (io.Reader, error) {
	logger.Debug("formmsg openaier", "link", cfg.CurrentAPI)
	if msg != "" { // otherwise let the bot continue
		newMsg := models.RoleMsg{Role: role, Content: msg}
		chatBody.Messages = append(chatBody.Messages, newMsg)
	}
	req := models.OpenAIReq{
		ChatBody: chatBody,
		Tools:    nil,
	}
	if cfg.ToolUse && !resume && role != cfg.ToolRole {
		req.Tools = baseTools // set tools to use
	}
	data, err := json.Marshal(req)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// deepseek
func (ds DeepSeekerCompletion) ParseChunk(data []byte) (*models.TextChunk, error) {
	llmchunk := models.DSCompletionResp{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return nil, err
	}
	resp := &models.TextChunk{
		Chunk: llmchunk.Choices[0].Text,
	}
	if llmchunk.Choices[0].FinishReason != "" {
		if resp.Chunk != "" {
			logger.Error("text inside of finish llmchunk", "chunk", llmchunk)
		}
		resp.Finished = true
	}
	return resp, nil
}

func (ds DeepSeekerCompletion) GetToken() string {
	return cfg.DeepSeekToken
}

func (ds DeepSeekerCompletion) FormMsg(msg, role string, resume bool) (io.Reader, error) {
	logger.Debug("formmsg deepseekercompletion", "link", cfg.CurrentAPI)
	if msg != "" { // otherwise let the bot to continue
		newMsg := models.RoleMsg{Role: role, Content: msg}
		chatBody.Messages = append(chatBody.Messages, newMsg)
		// if rag
		if cfg.RAGEnabled {
			ragResp, err := chatRagUse(newMsg.Content)
			if err != nil {
				logger.Error("failed to form a rag msg", "error", err)
				return nil, err
			}
			ragMsg := models.RoleMsg{Role: cfg.ToolRole, Content: ragResp}
			chatBody.Messages = append(chatBody.Messages, ragMsg)
		}
	}
	if cfg.ToolUse && !resume {
		// add to chat body
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{Role: cfg.ToolRole, Content: toolSysMsg})
	}
	messages := make([]string, len(chatBody.Messages))
	for i, m := range chatBody.Messages {
		messages[i] = m.ToPrompt()
	}
	prompt := strings.Join(messages, "\n")
	// strings builder?
	if !resume {
		botPersona := cfg.AssistantRole
		if cfg.WriteNextMsgAsCompletionAgent != "" {
			botPersona = cfg.WriteNextMsgAsCompletionAgent
		}
		botMsgStart := "\n" + botPersona + ":\n"
		prompt += botMsgStart
	}
	if cfg.ThinkUse && !cfg.ToolUse {
		prompt += "<think>"
	}
	logger.Debug("checking prompt for /completion", "tool_use", cfg.ToolUse,
		"msg", msg, "resume", resume, "prompt", prompt)
	payload := models.NewDSCompletionReq(prompt, chatBody.Model,
		defaultLCPProps["temp"], chatBody.MakeStopSlice())
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (ds DeepSeekerChat) ParseChunk(data []byte) (*models.TextChunk, error) {
	llmchunk := models.DSChatStreamResp{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return nil, err
	}
	resp := &models.TextChunk{}
	if llmchunk.Choices[0].FinishReason != "" {
		if llmchunk.Choices[0].Delta.Content != "" {
			logger.Error("text inside of finish llmchunk", "chunk", llmchunk)
		}
		resp.Chunk = llmchunk.Choices[0].Delta.Content
		resp.Finished = true
	} else {
		if llmchunk.Choices[0].Delta.ReasoningContent != "" {
			resp.Chunk = llmchunk.Choices[0].Delta.ReasoningContent
		} else {
			resp.Chunk = llmchunk.Choices[0].Delta.Content
		}
	}
	return resp, nil
}

func (ds DeepSeekerChat) GetToken() string {
	return cfg.DeepSeekToken
}

func (ds DeepSeekerChat) FormMsg(msg, role string, resume bool) (io.Reader, error) {
	logger.Debug("formmsg deepseekerchat", "link", cfg.CurrentAPI)
	if cfg.ToolUse && !resume {
		// prompt += "\n" + cfg.ToolRole + ":\n" + toolSysMsg
		// add to chat body
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{Role: cfg.ToolRole, Content: toolSysMsg})
	}
	if msg != "" { // otherwise let the bot continue
		newMsg := models.RoleMsg{Role: role, Content: msg}
		chatBody.Messages = append(chatBody.Messages, newMsg)
		// if rag
		if cfg.RAGEnabled {
			ragResp, err := chatRagUse(newMsg.Content)
			if err != nil {
				logger.Error("failed to form a rag msg", "error", err)
				return nil, err
			}
			ragMsg := models.RoleMsg{Role: cfg.ToolRole, Content: ragResp}
			chatBody.Messages = append(chatBody.Messages, ragMsg)
		}
	}
	// Create copy of chat body with standardized user role
	// modifiedBody := *chatBody
	bodyCopy := &models.ChatBody{
		Messages: make([]models.RoleMsg, len(chatBody.Messages)),
		Model:    chatBody.Model,
		Stream:   chatBody.Stream,
	}
	// modifiedBody.Messages = make([]models.RoleMsg, len(chatBody.Messages))
	for i, msg := range chatBody.Messages {
		logger.Debug("checking roles", "#", i, "role", msg.Role)
		if msg.Role == cfg.UserRole || i == 1 {
			bodyCopy.Messages[i].Role = "user"
			logger.Debug("replaced role in body", "#", i)
		} else {
			bodyCopy.Messages[i] = msg
		}
	}
	dsBody := models.NewDSCharReq(*bodyCopy)
	data, err := json.Marshal(dsBody)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// openrouter
func (or OpenRouterCompletion) ParseChunk(data []byte) (*models.TextChunk, error) {
	llmchunk := models.OpenRouterCompletionResp{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return nil, err
	}
	resp := &models.TextChunk{
		Chunk: llmchunk.Choices[len(llmchunk.Choices)-1].Text,
	}
	if llmchunk.Choices[len(llmchunk.Choices)-1].FinishReason == "stop" {
		if resp.Chunk != "" {
			logger.Error("text inside of finish llmchunk", "chunk", llmchunk)
		}
		resp.Finished = true
	}
	return resp, nil
}

func (or OpenRouterCompletion) GetToken() string {
	return cfg.OpenRouterToken
}

func (or OpenRouterCompletion) FormMsg(msg, role string, resume bool) (io.Reader, error) {
	logger.Debug("formmsg openroutercompletion", "link", cfg.CurrentAPI)
	if msg != "" { // otherwise let the bot to continue
		newMsg := models.RoleMsg{Role: role, Content: msg}
		chatBody.Messages = append(chatBody.Messages, newMsg)
		// if rag
		if cfg.RAGEnabled {
			ragResp, err := chatRagUse(newMsg.Content)
			if err != nil {
				logger.Error("failed to form a rag msg", "error", err)
				return nil, err
			}
			ragMsg := models.RoleMsg{Role: cfg.ToolRole, Content: ragResp}
			chatBody.Messages = append(chatBody.Messages, ragMsg)
		}
	}
	if cfg.ToolUse && !resume {
		// add to chat body
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{Role: cfg.ToolRole, Content: toolSysMsg})
	}
	messages := make([]string, len(chatBody.Messages))
	for i, m := range chatBody.Messages {
		messages[i] = m.ToPrompt()
	}
	prompt := strings.Join(messages, "\n")
	// strings builder?
	if !resume {
		botPersona := cfg.AssistantRole
		if cfg.WriteNextMsgAsCompletionAgent != "" {
			botPersona = cfg.WriteNextMsgAsCompletionAgent
		}
		botMsgStart := "\n" + botPersona + ":\n"
		prompt += botMsgStart
	}
	if cfg.ThinkUse && !cfg.ToolUse {
		prompt += "<think>"
	}
	ss := chatBody.MakeStopSlice()
	logger.Debug("checking prompt for /completion", "tool_use", cfg.ToolUse,
		"msg", msg, "resume", resume, "prompt", prompt, "stop_strings", ss)
	payload := models.NewOpenRouterCompletionReq(chatBody.Model, prompt, defaultLCPProps, ss)
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// chat
func (or OpenRouterChat) ParseChunk(data []byte) (*models.TextChunk, error) {
	llmchunk := models.OpenRouterChatResp{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return nil, err
	}
	resp := &models.TextChunk{
		Chunk: llmchunk.Choices[len(llmchunk.Choices)-1].Delta.Content,
	}
	if llmchunk.Choices[len(llmchunk.Choices)-1].FinishReason == "stop" {
		if resp.Chunk != "" {
			logger.Error("text inside of finish llmchunk", "chunk", llmchunk)
		}
		resp.Finished = true
	}
	return resp, nil
}

func (or OpenRouterChat) GetToken() string {
	return cfg.OpenRouterToken
}

func (or OpenRouterChat) FormMsg(msg, role string, resume bool) (io.Reader, error) {
	logger.Debug("formmsg open router completion", "link", cfg.CurrentAPI)
	if cfg.ToolUse && !resume {
		// prompt += "\n" + cfg.ToolRole + ":\n" + toolSysMsg
		// add to chat body
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{Role: cfg.ToolRole, Content: toolSysMsg})
	}
	if msg != "" { // otherwise let the bot continue
		newMsg := models.RoleMsg{Role: role, Content: msg}
		chatBody.Messages = append(chatBody.Messages, newMsg)
		// if rag
		if cfg.RAGEnabled {
			ragResp, err := chatRagUse(newMsg.Content)
			if err != nil {
				logger.Error("failed to form a rag msg", "error", err)
				return nil, err
			}
			ragMsg := models.RoleMsg{Role: cfg.ToolRole, Content: ragResp}
			chatBody.Messages = append(chatBody.Messages, ragMsg)
		}
	}
	// Create copy of chat body with standardized user role
	// modifiedBody := *chatBody
	bodyCopy := &models.ChatBody{
		Messages: make([]models.RoleMsg, len(chatBody.Messages)),
		Model:    chatBody.Model,
		Stream:   chatBody.Stream,
	}
	// modifiedBody.Messages = make([]models.RoleMsg, len(chatBody.Messages))
	for i, msg := range chatBody.Messages {
		logger.Debug("checking roles", "#", i, "role", msg.Role)
		if msg.Role == cfg.UserRole || i == 1 {
			bodyCopy.Messages[i].Role = "user"
			logger.Debug("replaced role in body", "#", i)
		} else {
			bodyCopy.Messages[i] = msg
		}
	}
	dsBody := models.NewDSCharReq(*bodyCopy)
	data, err := json.Marshal(dsBody)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}
