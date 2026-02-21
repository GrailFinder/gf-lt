package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gf-lt/config"
	"gf-lt/models"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

var httpClient = &http.Client{}

var defaultProps = map[string]float32{
	"temperature":    0.8,
	"dry_multiplier": 0.0,
	"min_p":          0.05,
	"n_predict":      -1.0,
}

func detectAPI(api string) (isCompletion, isChat, isDeepSeek, isOpenRouter bool) {
	isCompletion = strings.Contains(api, "/completion") && !strings.Contains(api, "/chat/completions")
	isChat = strings.Contains(api, "/chat/completions")
	isDeepSeek = strings.Contains(api, "deepseek.com")
	isOpenRouter = strings.Contains(api, "openrouter.ai")
	return
}

type AgentClient struct {
	cfg      *config.Config
	getToken func() string
	log      slog.Logger
}

func NewAgentClient(cfg *config.Config, log slog.Logger, gt func() string) *AgentClient {
	return &AgentClient{
		cfg:      cfg,
		getToken: gt,
		log:      log,
	}
}

func (ag *AgentClient) Log() *slog.Logger {
	return &ag.log
}

func (ag *AgentClient) FormMsg(sysprompt, msg string) (io.Reader, error) {
	b, err := ag.buildRequest(sysprompt, msg)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

// buildRequest creates the appropriate LLM request based on the current API endpoint.
func (ag *AgentClient) buildRequest(sysprompt, msg string) ([]byte, error) {
	api := ag.cfg.CurrentAPI
	model := ag.cfg.CurrentModel
	messages := []models.RoleMsg{
		{Role: "system", Content: sysprompt},
		{Role: "user", Content: msg},
	}

	// Determine API type
	isCompletion, isChat, isDeepSeek, isOpenRouter := detectAPI(api)
	ag.log.Debug("agent building request", "api", api, "isCompletion", isCompletion, "isChat", isChat, "isDeepSeek", isDeepSeek, "isOpenRouter", isOpenRouter)

	// Build prompt for completion endpoints
	if isCompletion {
		var sb strings.Builder
		for _, m := range messages {
			sb.WriteString(m.ToPrompt())
			sb.WriteString("\n")
		}
		prompt := strings.TrimSpace(sb.String())

		switch {
		case isDeepSeek:
			// DeepSeek completion
			req := models.NewDSCompletionReq(prompt, model, defaultProps["temperature"], []string{})
			req.Stream = false // Agents don't need streaming
			return json.Marshal(req)
		case isOpenRouter:
			// OpenRouter completion
			req := models.NewOpenRouterCompletionReq(model, prompt, defaultProps, []string{})
			req.Stream = false // Agents don't need streaming
			return json.Marshal(req)
		default:
			// Assume llama.cpp completion
			req := models.NewLCPReq(prompt, model, nil, defaultProps, []string{})
			req.Stream = false // Agents don't need streaming
			return json.Marshal(req)
		}
	}

	// Chat completions endpoints
	if isChat || !isCompletion {
		chatBody := &models.ChatBody{
			Model:    model,
			Stream:   false, // Agents don't need streaming
			Messages: messages,
		}

		switch {
		case isDeepSeek:
			// DeepSeek chat
			req := models.NewDSChatReq(*chatBody)
			return json.Marshal(req)
		case isOpenRouter:
			// OpenRouter chat - agents don't use reasoning by default
			req := models.NewOpenRouterChatReq(*chatBody, defaultProps, "")
			return json.Marshal(req)
		default:
			// Assume llama.cpp chat (OpenAI format)
			req := models.OpenAIReq{
				ChatBody: chatBody,
				Tools:    nil,
			}
			return json.Marshal(req)
		}
	}

	// Fallback (should not reach here)
	ag.log.Warn("unknown API, using default chat completions format", "api", api)
	chatBody := &models.ChatBody{
		Model:    model,
		Stream:   false, // Agents don't need streaming
		Messages: messages,
	}
	return json.Marshal(chatBody)
}

func (ag *AgentClient) LLMRequest(body io.Reader) ([]byte, error) {
	// Read the body for debugging (but we need to recreate it for the request)
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		ag.log.Error("failed to read request body", "error", err)
		return nil, err
	}

	req, err := http.NewRequest("POST", ag.cfg.CurrentAPI, bytes.NewReader(bodyBytes))
	if err != nil {
		ag.log.Error("failed to create request", "error", err)
		return nil, err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+ag.getToken())
	req.Header.Set("Accept-Encoding", "gzip")

	ag.log.Debug("agent LLM request", "url", ag.cfg.CurrentAPI, "body_preview", string(bodyBytes[:min(len(bodyBytes), 500)]))

	resp, err := httpClient.Do(req)
	if err != nil {
		ag.log.Error("llamacpp api request failed", "error", err, "url", ag.cfg.CurrentAPI)
		return nil, err
	}
	defer resp.Body.Close()

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		ag.log.Error("failed to read response", "error", err)
		return nil, err
	}

	if resp.StatusCode >= 400 {
		ag.log.Error("agent LLM request failed", "status", resp.StatusCode, "response", string(responseBytes[:min(len(responseBytes), 1000)]))
		return responseBytes, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(responseBytes[:min(len(responseBytes), 200)]))
	}

	// Parse response and extract text content
	text, err := extractTextFromResponse(responseBytes)
	if err != nil {
		ag.log.Error("failed to extract text from response", "error", err, "response_preview", string(responseBytes[:min(len(responseBytes), 500)]))
		// Return raw response as fallback
		return responseBytes, nil
	}

	return []byte(text), nil
}

// extractTextFromResponse parses common LLM response formats and extracts the text content.
func extractTextFromResponse(data []byte) (string, error) {
	// Try to parse as generic JSON first
	var genericResp map[string]interface{}
	if err := json.Unmarshal(data, &genericResp); err != nil {
		// Not JSON, return as string
		return string(data), nil
	}

	// Check for OpenAI chat completion format
	if choices, ok := genericResp["choices"].([]interface{}); ok && len(choices) > 0 {
		if firstChoice, ok := choices[0].(map[string]interface{}); ok {
			// Chat completion: choices[0].message.content
			if message, ok := firstChoice["message"].(map[string]interface{}); ok {
				if content, ok := message["content"].(string); ok {
					return content, nil
				}
			}
			// Completion: choices[0].text
			if text, ok := firstChoice["text"].(string); ok {
				return text, nil
			}
			// Delta format for streaming (should not happen with stream: false)
			if delta, ok := firstChoice["delta"].(map[string]interface{}); ok {
				if content, ok := delta["content"].(string); ok {
					return content, nil
				}
			}
		}
	}

	// Check for llama.cpp completion format
	if content, ok := genericResp["content"].(string); ok {
		return content, nil
	}

	// Unknown format, return pretty-printed JSON
	prettyJSON, err := json.MarshalIndent(genericResp, "", "  ")
	if err != nil {
		return string(data), nil
	}
	return string(prettyJSON), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
