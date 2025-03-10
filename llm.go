package main

import (
	"bytes"
	"elefant/models"
	"encoding/json"
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
	case "http://localhost:8080/v1/chat/completions":
		chunkParser = OpenAIer{}
	case "https://api.deepseek.com/beta/completions":
		chunkParser = DeepSeekerCompletion{}
	case "https://api.deepseek.com/chat/completions":
		chunkParser = DeepSeekerChat{}
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
	payload = models.NewLCPReq(prompt, cfg, defaultLCPProps)
	if strings.Contains(chatBody.Model, "deepseek") {
		payload = models.NewDSCompletionReq(prompt, chatBody.Model,
			defaultLCPProps["temp"], cfg)
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
		defaultLCPProps["temp"], cfg)
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (ds DeepSeekerChat) ParseChunk(data []byte) (string, bool, error) {
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

func (ds DeepSeekerChat) FormMsg(msg, role string, resume bool) (io.Reader, error) {
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
	modifiedBody := *chatBody
	modifiedBody.Messages = make([]models.RoleMsg, len(chatBody.Messages))
	for i, msg := range chatBody.Messages {
		if msg.Role == cfg.UserRole {
			modifiedBody.Messages[i].Role = "user"
		} else {
			modifiedBody.Messages[i] = msg
		}
	}
	models.NewDSCharReq(&modifiedBody)
	data, err := json.Marshal(chatBody)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}
