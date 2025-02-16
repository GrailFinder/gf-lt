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

func initChunkParser() {
	chunkParser = LlamaCPPeer{}
	if strings.Contains(cfg.CurrentAPI, "v1") {
		logger.Debug("chosen /v1/chat parser")
		chunkParser = OpenAIer{}
		return
	}
	logger.Debug("chosen llamacpp /completion parser")
}

type LlamaCPPeer struct {
}
type OpenAIer struct {
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
	// if cfg.ToolUse && msg != "" && !resume {
	if !resume {
		botMsgStart := "\n" + cfg.AssistantRole + ":\n"
		prompt += botMsgStart
	}
	// if cfg.ThinkUse && msg != "" && !cfg.ToolUse {
	if cfg.ThinkUse && !cfg.ToolUse {
		prompt += "<think>"
	}
	logger.Debug("checking prompt for /completion", "tool_use", cfg.ToolUse,
		"msg", msg, "resume", resume, "prompt", prompt)
	payload := models.NewLCPReq(prompt, cfg, defaultLCPProps)
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
