package main

import (
	"bytes"
	"encoding/json"
	"gf-lt/models"
	"io"
	"strings"
)

type ChunkParser interface {
	ParseChunk([]byte) (string, bool, error)
	FormMsg(msg, role string, cont bool) (io.Reader, error)
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
	default:
		chunkParser = LlamaCPPeer{}
	}
	// if strings.Contains(cfg.CurrentAPI, "chat") {
	// 	logger.Debug("chosen chat parser")
	// 	chunkParser = OpenAIer{}
	// 	return
	// }
	// logger.Debug("chosen llamacpp /completion parser")
}

type LlamaCPPeer struct {
}
type OpenAIer struct {
}
type DeepSeekerCompletion struct {
}
type DeepSeekerChat struct {
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
		botMsgStart := "\n" + cfg.AssistantRole + ":\n"
		prompt += botMsgStart
	}
	if cfg.ThinkUse && !cfg.ToolUse {
		prompt += "<think>"
	}
	logger.Debug("checking prompt for /completion", "tool_use", cfg.ToolUse,
		"msg", msg, "resume", resume, "prompt", prompt)
	var payload any
	payload = models.NewLCPReq(prompt, cfg, defaultLCPProps, chatBody.MakeStopSlice())
	if strings.Contains(chatBody.Model, "deepseek") {
		payload = models.NewDSCompletionReq(prompt, chatBody.Model,
			defaultLCPProps["temp"], cfg, chatBody.MakeStopSlice())
	}
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (lcp LlamaCPPeer) ParseChunk(data []byte) (string, bool, error) {
	llmchunk := models.LlamaCPPResp{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return "", false, err
	}
	if llmchunk.Stop {
		if llmchunk.Content != "" {
			logger.Error("text inside of finish llmchunk", "chunk", llmchunk)
		}
		return llmchunk.Content, true, nil
	}
	return llmchunk.Content, false, nil
}

func (op OpenAIer) ParseChunk(data []byte) (string, bool, error) {
	llmchunk := models.LLMRespChunk{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return "", false, err
	}
	content := llmchunk.Choices[len(llmchunk.Choices)-1].Delta.Content
	if llmchunk.Choices[len(llmchunk.Choices)-1].FinishReason == "stop" {
		if content != "" {
			logger.Error("text inside of finish llmchunk", "chunk", llmchunk)
		}
		return content, true, nil
	}
	return content, false, nil
}

func (op OpenAIer) FormMsg(msg, role string, resume bool) (io.Reader, error) {
	logger.Debug("formmsg openaier", "link", cfg.CurrentAPI)
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
	data, err := json.Marshal(chatBody)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// deepseek
func (ds DeepSeekerCompletion) ParseChunk(data []byte) (string, bool, error) {
	llmchunk := models.DSCompletionResp{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return "", false, err
	}
	if llmchunk.Choices[0].FinishReason != "" {
		if llmchunk.Choices[0].Text != "" {
			logger.Error("text inside of finish llmchunk", "chunk", llmchunk)
		}
		return llmchunk.Choices[0].Text, true, nil
	}
	return llmchunk.Choices[0].Text, false, nil
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
		botMsgStart := "\n" + cfg.AssistantRole + ":\n"
		prompt += botMsgStart
	}
	if cfg.ThinkUse && !cfg.ToolUse {
		prompt += "<think>"
	}
	logger.Debug("checking prompt for /completion", "tool_use", cfg.ToolUse,
		"msg", msg, "resume", resume, "prompt", prompt)
	payload := models.NewDSCompletionReq(prompt, chatBody.Model,
		defaultLCPProps["temp"], cfg, chatBody.MakeStopSlice())
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (ds DeepSeekerChat) ParseChunk(data []byte) (string, bool, error) {
	llmchunk := models.DSChatStreamResp{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return "", false, err
	}
	if llmchunk.Choices[0].FinishReason != "" {
		if llmchunk.Choices[0].Delta.Content != "" {
			logger.Error("text inside of finish llmchunk", "chunk", llmchunk)
		}
		return llmchunk.Choices[0].Delta.Content, true, nil
	}
	if llmchunk.Choices[0].Delta.ReasoningContent != "" {
		return llmchunk.Choices[0].Delta.ReasoningContent, false, nil
	}
	return llmchunk.Choices[0].Delta.Content, false, nil
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
