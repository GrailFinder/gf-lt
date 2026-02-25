package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"gf-lt/models"
	"io"
	"strings"
)

var imageAttachmentPath string // Global variable to track image attachment for next message
var lastImg string             // for ctrl+j

// containsToolSysMsg checks if the toolSysMsg already exists in the chat body
func containsToolSysMsg() bool {
	for _, msg := range chatBody.Messages {
		if msg.Role == cfg.ToolRole && msg.Content == toolSysMsg {
			return true
		}
	}
	return false
}

// SetImageAttachment sets an image to be attached to the next message sent to the LLM
func SetImageAttachment(imagePath string) {
	imageAttachmentPath = imagePath
	lastImg = imagePath
}

// ClearImageAttachment clears any pending image attachment and updates UI
func ClearImageAttachment() {
	imageAttachmentPath = ""
}

// filterMessagesForCurrentCharacter filters messages based on char-specific context.
// Returns filtered messages and the bot persona role (target character).
func filterMessagesForCurrentCharacter(messages []models.RoleMsg) ([]models.RoleMsg, string) {
	botPersona := cfg.AssistantRole
	if cfg.WriteNextMsgAsCompletionAgent != "" {
		botPersona = cfg.WriteNextMsgAsCompletionAgent
	}
	if cfg == nil || !cfg.CharSpecificContextEnabled {
		return messages, botPersona
	}
	// get last message (written by user) and checck if it has a tag
	lm := messages[len(messages)-1]
	recipient, ok := getValidKnowToRecipient(&lm)
	if ok && recipient != "" {
		botPersona = recipient
	}
	filtered := filterMessagesForCharacter(messages, botPersona)
	return filtered, botPersona
}

type ChunkParser interface {
	ParseChunk([]byte) (*models.TextChunk, error)
	FormMsg(msg, role string, cont bool) (io.Reader, error)
	GetToken() string
	GetAPIType() models.APIType
}

func choseChunkParser() {
	chunkParser = LCPCompletion{}
	switch cfg.CurrentAPI {
	case "http://localhost:8080/completion":
		chunkParser = LCPCompletion{}
		logger.Debug("chosen lcpcompletion", "link", cfg.CurrentAPI)
		return
	case "http://localhost:8080/v1/chat/completions":
		chunkParser = LCPChat{}
		logger.Debug("chosen lcpchat", "link", cfg.CurrentAPI)
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
		chunkParser = LCPCompletion{}
	}
}

type LCPCompletion struct {
}
type LCPChat struct {
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

func (lcp LCPCompletion) GetAPIType() models.APIType {
	return models.APITypeCompletion
}

func (lcp LCPCompletion) GetToken() string {
	return ""
}

func (lcp LCPCompletion) FormMsg(msg, role string, resume bool) (io.Reader, error) {
	logger.Debug("formmsg lcpcompletion", "link", cfg.CurrentAPI)
	localImageAttachmentPath := imageAttachmentPath
	var multimodalData []string
	if localImageAttachmentPath != "" {
		imageURL, err := models.CreateImageURLFromPath(localImageAttachmentPath)
		if err != nil {
			logger.Error("failed to create image URL from path for completion",
				"error", err, "path", localImageAttachmentPath)
			return nil, err
		}
		// Extract base64 part from data URL (e.g., "data:image/jpeg;base64,...")
		parts := strings.SplitN(imageURL, ",", 2)
		if len(parts) == 2 {
			multimodalData = append(multimodalData, parts[1])
		} else {
			logger.Error("invalid image data URL format", "url", imageURL)
			return nil, errors.New("invalid image data URL format")
		}
		imageAttachmentPath = "" // Clear the attachment after use
	}
	if msg != "" { // otherwise let the bot to continue
		newMsg := models.RoleMsg{Role: role, Content: msg}
		newMsg = *processMessageTag(&newMsg)
		chatBody.Messages = append(chatBody.Messages, newMsg)
	}
	// sending description of the tools and how to use them
	if cfg.ToolUse && !resume && role == cfg.UserRole && !containsToolSysMsg() {
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{Role: cfg.ToolRole, Content: toolSysMsg})
	}
	filteredMessages, botPersona := filterMessagesForCurrentCharacter(chatBody.Messages)
	messages := make([]string, len(filteredMessages))
	for i, m := range filteredMessages {
		messages[i] = stripThinkingFromMsg(&m).ToPrompt()
	}
	prompt := strings.Join(messages, "\n")
	// Add multimodal media markers to the prompt text when multimodal data is present
	// This is required by llama.cpp multimodal models so they know where to insert media
	if len(multimodalData) > 0 {
		// Add a media marker for each item in the multimodal data
		var sb strings.Builder
		sb.WriteString(prompt)
		for range multimodalData {
			sb.WriteString(" <__media__>") // llama.cpp default multimodal marker
		}
		prompt = sb.String()
	}
	// needs to be after <__media__> if there are images
	if !resume {
		botMsgStart := "\n" + botPersona + ":\n"
		prompt += botMsgStart
	}
	logger.Debug("checking prompt for /completion", "tool_use", cfg.ToolUse,
		"msg", msg, "resume", resume, "prompt", prompt, "multimodal_data_count", len(multimodalData))
	payload := models.NewLCPReq(prompt, chatBody.Model, multimodalData,
		defaultLCPProps, chatBody.MakeStopSliceExcluding("", listChatRoles()))
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (lcp LCPCompletion) ParseChunk(data []byte) (*models.TextChunk, error) {
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

func (lcp LCPChat) GetAPIType() models.APIType {
	return models.APITypeChat
}

func (lcp LCPChat) GetToken() string {
	return ""
}

func (op LCPChat) ParseChunk(data []byte) (*models.TextChunk, error) {
	llmchunk := models.LLMRespChunk{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return nil, err
	}

	// Handle multiple choices safely
	if len(llmchunk.Choices) == 0 {
		logger.Warn("LCPChat ParseChunk: no choices in response", "data", string(data))
		return &models.TextChunk{Finished: true}, nil
	}
	lastChoice := llmchunk.Choices[len(llmchunk.Choices)-1]
	resp := &models.TextChunk{
		Chunk:     lastChoice.Delta.Content,
		Reasoning: lastChoice.Delta.ReasoningContent,
	}
	// Check for tool calls in all choices, not just the last one
	for _, choice := range llmchunk.Choices {
		if len(choice.Delta.ToolCalls) > 0 {
			toolCall := choice.Delta.ToolCalls[0]
			resp.ToolChunk = toolCall.Function.Arguments
			fname := toolCall.Function.Name
			if fname != "" {
				resp.FuncName = fname
			}
			// Capture the tool call ID if available
			resp.ToolID = toolCall.ID
			break // Process only the first tool call
		}
	}
	if lastChoice.FinishReason == "stop" {
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

func (op LCPChat) FormMsg(msg, role string, resume bool) (io.Reader, error) {
	logger.Debug("formmsg lcpchat", "link", cfg.CurrentAPI)
	// Capture the image attachment path at the beginning to avoid race conditions
	// with API rotation that might clear the global variable
	localImageAttachmentPath := imageAttachmentPath
	if msg != "" { // otherwise let the bot continue
		// Create the message with support for multimodal content
		var newMsg models.RoleMsg
		// Check if we have an image to add to this message
		if localImageAttachmentPath != "" {
			// Create a multimodal message with both text and image
			newMsg = models.NewMultimodalMsg(role, []interface{}{})
			// Add the text content
			newMsg.AddTextPart(msg)
			// Add the image content
			imageURL, err := models.CreateImageURLFromPath(localImageAttachmentPath)
			if err != nil {
				logger.Error("failed to create image URL from path", "error", err, "path", localImageAttachmentPath)
				// If image processing fails, fall back to simple text message
				newMsg = models.NewRoleMsg(role, msg)
			} else {
				newMsg.AddImagePart(imageURL, localImageAttachmentPath)
			}
			// Only clear the global image attachment after successfully processing it in this API call
			imageAttachmentPath = "" // Clear the attachment after use
		} else {
			// Create a simple text message
			newMsg = models.NewRoleMsg(role, msg)
		}
		newMsg = *processMessageTag(&newMsg)
		chatBody.Messages = append(chatBody.Messages, newMsg)
		logger.Debug("LCPChat FormMsg: added message to chatBody", "role", newMsg.Role,
			"content_len", len(newMsg.Content), "message_count_after_add", len(chatBody.Messages))
	}
	filteredMessages, _ := filterMessagesForCurrentCharacter(chatBody.Messages)
	// openai /v1/chat does not support custom roles; needs to be user, assistant, system
	// Add persona suffix to the last user message to indicate who the assistant should reply as
	bodyCopy := &models.ChatBody{
		Messages: make([]models.RoleMsg, len(filteredMessages)),
		Model:    chatBody.Model,
		Stream:   chatBody.Stream,
	}
	for i, msg := range filteredMessages {
		strippedMsg := *stripThinkingFromMsg(&msg)
		if strippedMsg.Role == cfg.UserRole {
			bodyCopy.Messages[i] = strippedMsg
			bodyCopy.Messages[i].Role = "user"
		} else {
			bodyCopy.Messages[i] = strippedMsg
		}
	}
	// Clean null/empty messages to prevent API issues
	bodyCopy.Messages = consolidateAssistantMessages(bodyCopy.Messages)
	req := models.OpenAIReq{
		ChatBody: bodyCopy,
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
func (ds DeepSeekerCompletion) GetAPIType() models.APIType {
	return models.APITypeCompletion
}

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
	if err := deepseekModelValidator(); err != nil {
		return nil, err
	}
	if msg != "" { // otherwise let the bot to continue
		newMsg := models.RoleMsg{Role: role, Content: msg}
		newMsg = *processMessageTag(&newMsg)
		chatBody.Messages = append(chatBody.Messages, newMsg)
	}
	// sending description of the tools and how to use them
	if cfg.ToolUse && !resume && role == cfg.UserRole && !containsToolSysMsg() {
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{Role: cfg.ToolRole, Content: toolSysMsg})
	}
	filteredMessages, botPersona := filterMessagesForCurrentCharacter(chatBody.Messages)
	messages := make([]string, len(filteredMessages))
	for i, m := range filteredMessages {
		messages[i] = stripThinkingFromMsg(&m).ToPrompt()
	}
	prompt := strings.Join(messages, "\n")
	// strings builder?
	if !resume {
		botMsgStart := "\n" + botPersona + ":\n"
		prompt += botMsgStart
	}
	logger.Debug("checking prompt for /completion", "tool_use", cfg.ToolUse,
		"msg", msg, "resume", resume, "prompt", prompt)
	payload := models.NewDSCompletionReq(prompt, chatBody.Model,
		defaultLCPProps["temp"],
		chatBody.MakeStopSliceExcluding("", listChatRoles()))
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (ds DeepSeekerChat) GetAPIType() models.APIType {
	return models.APITypeChat
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
	if err := deepseekModelValidator(); err != nil {
		return nil, err
	}
	if msg != "" { // otherwise let the bot continue
		newMsg := models.RoleMsg{Role: role, Content: msg}
		newMsg = *processMessageTag(&newMsg)
		chatBody.Messages = append(chatBody.Messages, newMsg)
	}
	// Create copy of chat body with standardized user role
	filteredMessages, _ := filterMessagesForCurrentCharacter(chatBody.Messages)
	// Add persona suffix to the last user message to indicate who the assistant should reply as
	bodyCopy := &models.ChatBody{
		Messages: make([]models.RoleMsg, len(filteredMessages)),
		Model:    chatBody.Model,
		Stream:   chatBody.Stream,
	}
	for i, msg := range filteredMessages {
		strippedMsg := *stripThinkingFromMsg(&msg)
		if strippedMsg.Role == cfg.UserRole || i == 1 {
			bodyCopy.Messages[i] = strippedMsg
			bodyCopy.Messages[i].Role = "user"
		} else {
			bodyCopy.Messages[i] = strippedMsg
		}
	}
	// Clean null/empty messages to prevent API issues
	bodyCopy.Messages = consolidateAssistantMessages(bodyCopy.Messages)
	dsBody := models.NewDSChatReq(*bodyCopy)
	data, err := json.Marshal(dsBody)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// openrouter
func (or OpenRouterCompletion) GetAPIType() models.APIType {
	return models.APITypeCompletion
}

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
		newMsg = *processMessageTag(&newMsg)
		chatBody.Messages = append(chatBody.Messages, newMsg)
	}
	// sending description of the tools and how to use them
	if cfg.ToolUse && !resume && role == cfg.UserRole && !containsToolSysMsg() {
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{Role: cfg.ToolRole, Content: toolSysMsg})
	}
	filteredMessages, botPersona := filterMessagesForCurrentCharacter(chatBody.Messages)
	messages := make([]string, len(filteredMessages))
	for i, m := range filteredMessages {
		messages[i] = stripThinkingFromMsg(&m).ToPrompt()
	}
	prompt := strings.Join(messages, "\n")
	// strings builder?
	if !resume {
		botMsgStart := "\n" + botPersona + ":\n"
		prompt += botMsgStart
	}
	stopSlice := chatBody.MakeStopSliceExcluding("", listChatRoles())
	logger.Debug("checking prompt for /completion", "tool_use", cfg.ToolUse,
		"msg", msg, "resume", resume, "prompt", prompt, "stop_strings", stopSlice)
	payload := models.NewOpenRouterCompletionReq(chatBody.Model, prompt,
		defaultLCPProps, stopSlice)
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// chat
func (or OpenRouterChat) GetAPIType() models.APIType {
	return models.APITypeChat
}

func (or OpenRouterChat) ParseChunk(data []byte) (*models.TextChunk, error) {
	llmchunk := models.OpenRouterChatResp{}
	if err := json.Unmarshal(data, &llmchunk); err != nil {
		logger.Error("failed to decode", "error", err, "line", string(data))
		return nil, err
	}
	lastChoice := llmchunk.Choices[len(llmchunk.Choices)-1]
	resp := &models.TextChunk{
		Chunk:     lastChoice.Delta.Content,
		Reasoning: lastChoice.Delta.Reasoning,
	}
	// Handle tool calls similar to LCPChat
	if len(lastChoice.Delta.ToolCalls) > 0 {
		toolCall := lastChoice.Delta.ToolCalls[0]
		resp.ToolChunk = toolCall.Function.Arguments
		fname := toolCall.Function.Name
		if fname != "" {
			resp.FuncName = fname
		}
		// Capture the tool call ID if available
		resp.ToolID = toolCall.ID
	}
	if resp.ToolChunk != "" {
		resp.ToolResp = true
	}
	if lastChoice.FinishReason == "stop" {
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
	// Capture the image attachment path at the beginning to avoid race conditions
	// with API rotation that might clear the global variable
	localImageAttachmentPath := imageAttachmentPath
	if msg != "" { // otherwise let the bot continue
		var newMsg models.RoleMsg
		// Check if we have an image to add to this message
		if localImageAttachmentPath != "" {
			// Create a multimodal message with both text and image
			newMsg = models.NewMultimodalMsg(role, []interface{}{})
			// Add the text content
			newMsg.AddTextPart(msg)
			// Add the image content
			imageURL, err := models.CreateImageURLFromPath(localImageAttachmentPath)
			if err != nil {
				logger.Error("failed to create image URL from path", "error", err, "path", localImageAttachmentPath)
				// If image processing fails, fall back to simple text message
				newMsg = models.NewRoleMsg(role, msg)
			} else {
				newMsg.AddImagePart(imageURL, localImageAttachmentPath)
			}
			// Only clear the global image attachment after successfully processing it in this API call
			imageAttachmentPath = "" // Clear the attachment after use
		} else {
			// Create a simple text message
			newMsg = models.NewRoleMsg(role, msg)
		}
		newMsg = *processMessageTag(&newMsg)
		chatBody.Messages = append(chatBody.Messages, newMsg)
	}
	// Create copy of chat body with standardized user role
	filteredMessages, _ := filterMessagesForCurrentCharacter(chatBody.Messages)
	// Add persona suffix to the last user message to indicate who the assistant should reply as
	bodyCopy := &models.ChatBody{
		Messages: make([]models.RoleMsg, len(filteredMessages)),
		Model:    chatBody.Model,
		Stream:   chatBody.Stream,
	}
	for i, msg := range filteredMessages {
		strippedMsg := *stripThinkingFromMsg(&msg)
		bodyCopy.Messages[i] = strippedMsg
		// Standardize role if it's a user role
		if bodyCopy.Messages[i].Role == cfg.UserRole {
			bodyCopy.Messages[i] = strippedMsg
			bodyCopy.Messages[i].Role = "user"
		}
	}
	// Clean null/empty messages to prevent API issues
	bodyCopy.Messages = consolidateAssistantMessages(bodyCopy.Messages)
	orBody := models.NewOpenRouterChatReq(*bodyCopy, defaultLCPProps, cfg.ReasoningEffort)
	if cfg.ToolUse && !resume && role != cfg.ToolRole {
		orBody.Tools = baseTools // set tools to use
	}
	data, err := json.Marshal(orBody)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}
