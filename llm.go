package main

import (
	"bytes"
	"encoding/json"
	"gf-lt/models"
	"gf-lt/tools"
	"io"
	"net/http"
	"regexp"
	"strings"

	_ "gf-lt/mcp"
)

var pendingImageAttachments []string // Global variable to track image attachments for next message
var lastImg string                   // for ctrl+j
var mediaMarker string               // llama.cpp media marker, fetched from /props
var cachedMediaMarkerModel string    // model name for which mediaMarker was fetched

// containsToolSysMsg checks if the tools.ToolSysMsg already exists in the chat body
func containsToolSysMsg() bool {
	for i := range chatBody.Messages {
		if (chatBody.Messages[i].Role == cfg.ToolRole || chatBody.Messages[i].Role == "system") && chatBody.Messages[i].Content == tools.ToolSysMsg {
			return true
		}
	}
	return false
}

// removeToolGuide removes tool guide from the first system message
func removeToolGuide(messages []models.RoleMsg) []models.RoleMsg {
	if len(messages) == 0 {
		return messages
	}
	// If first message is not system, insert a new system message
	if messages[0].Role != "system" {
		messages = append([]models.RoleMsg{{Role: "system", Content: ""}}, messages...)
	}
	re := regexp.MustCompile(`(?s)<tool_guide>.*?</tool_guide>\n*\n?`)
	messages[0].Content = re.ReplaceAllString(messages[0].Content, "")
	return messages
}

// prependToolGuide adds tool guide to the first system message
func prependToolGuide(messages []models.RoleMsg, toolGuide string) []models.RoleMsg {
	if len(messages) == 0 {
		messages = append(messages, models.RoleMsg{Role: "system", Content: ""})
	}
	// If first message is not system, insert a new system message at position 0
	if messages[0].Role != "system" {
		messages = append([]models.RoleMsg{{Role: "system", Content: ""}}, messages...)
	}
	messages[0].Content = toolGuide + "\n\n" + messages[0].Content
	return messages
}

// AddImageAttachment appends an image to be attached to the next message sent to the LLM
func AddImageAttachment(imagePath string) {
	pendingImageAttachments = append(pendingImageAttachments, imagePath)
	lastImg = imagePath
}

// RemoveLastImageAttachment removes the last added image from pending attachments
func RemoveLastImageAttachment() {
	if len(pendingImageAttachments) > 0 {
		pendingImageAttachments = pendingImageAttachments[:len(pendingImageAttachments)-1]
		if len(pendingImageAttachments) > 0 {
			lastImg = pendingImageAttachments[len(pendingImageAttachments)-1]
		} else {
			lastImg = ""
		}
	}
}

// ClearImageAttachments clears all pending image attachments and updates UI
func ClearImageAttachments() {
	pendingImageAttachments = []string{}
	lastImg = ""
}

// SetImageAttachment (deprecated, kept for backward compat) appends an image
func SetImageAttachment(imagePath string) {
	AddImageAttachment(imagePath)
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

// fetchMediaMarker queries the llama.cpp /props endpoint to get the media marker
// for the current model. Results are cached per model to avoid repeated calls.
// Runs in a goroutine to avoid blocking the TUI.
func fetchMediaMarker() {
	if !isLocalLlamacpp() {
		return
	}
	if cachedMediaMarkerModel == chatBody.Model && mediaMarker != "" {
		return
	}
	// Capture values before launching goroutine to avoid races
	currentModel := chatBody.Model
	currentAPI := cfg.CurrentAPI
	go func() {
		baseURL := currentAPI
		if strings.Contains(baseURL, "/completion") {
			baseURL = strings.TrimSuffix(baseURL, "/completion")
		} else if strings.Contains(baseURL, "/v1/chat/completions") {
			baseURL = strings.TrimSuffix(baseURL, "/v1/chat/completions")
		} else {
			return
		}
		propsURL := baseURL + "/props?model=" + currentModel
		req, err := http.NewRequest("GET", propsURL, nil)
		if err != nil {
			logger.Warn("failed to create props request", "error", err)
			return
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			logger.Warn("failed to fetch props", "error", err, "url", propsURL)
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.Warn("failed to read props response", "error", err)
			return
		}
		var props map[string]any
		if err := json.Unmarshal(body, &props); err != nil {
			logger.Warn("failed to parse props response", "error", err)
			return
		}
		if marker, ok := props["media_marker"].(string); ok && marker != "" {
			mediaMarker = marker
			cachedMediaMarkerModel = currentModel
			logger.Info("fetched media_marker", "marker", marker, "model", currentModel)
		}
	}()
}

// choseChunkParser selects the appropriate chunk parser based on the current API
func choseChunkParser() {
	chunkParser = LCPCompletion{}
	switch cfg.CurrentAPI {
	case "http://localhost:8080/completion", "http://127.0.0.1:8080/completion":
		chunkParser = LCPCompletion{}
		logger.Debug("chosen lcpcompletion", "link", cfg.CurrentAPI)
		fetchMediaMarker()
		return
	case "http://localhost:8080/v1/chat/completions", "http://127.0.0.1:8080/v1/chat/completions":
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
		logger.Warn("unexpected case, assuming llama.cpp on non default address", "link", cfg.CurrentAPI)
		if strings.Contains(cfg.CurrentAPI, "chat") {
			chunkParser = LCPChat{}
			return
		}
		chunkParser = LCPCompletion{}
		fetchMediaMarker()
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
	localImageAttachments := pendingImageAttachments
	var multimodalData []string
	if msg != "" { // otherwise Let the bot to continue
		var newMsg models.RoleMsg
		if len(localImageAttachments) > 0 {
			newMsg = models.NewMultimodalMsg(role, []any{})
			newMsg.AddTextPart(msg)
			for _, imgPath := range localImageAttachments {
				imageURL, err := models.CreateImageURLFromPath(imgPath)
				if err != nil {
					logger.Error("failed to create image URL from path for completion",
						"error", err, "path", imgPath)
					continue
				}
				newMsg.AddImagePart(imageURL, imgPath)
			}
			pendingImageAttachments = []string{} // Clear after use
		} else { // not a multimodal msg or image passed in tool call
			newMsg = models.RoleMsg{Role: role, Content: msg}
		}
		newMsg = *processMessageTag(&newMsg)
		chatBody.Messages = append(chatBody.Messages, newMsg)
	}
	// sending description of the tools and how to use them
	if cfg.ToolUse && !resume && role == cfg.UserRole && !containsToolSysMsg() {
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{Role: "system", Content: tools.ToolSysMsg})
	}
	filteredMessages, botPersona := filterMessagesForCurrentCharacter(chatBody.Messages)
	// Build prompt and extract images inline as we process each message
	messages := make([]string, len(filteredMessages))
	for i := range filteredMessages {
		m := stripThinkingFromMsg(&filteredMessages[i])
		messages[i] = m.ToPrompt()
		// Extract images from this message and add marker inline
		if len(m.ContentParts) > 0 {
			for _, part := range m.ContentParts {
				var imgURL string
				// Check for struct type
				if imgPart, ok := part.(models.ImageContentPart); ok {
					imgURL = imgPart.ImageURL.URL
				} else if partMap, ok := part.(map[string]any); ok {
					// Check for map type (from JSON unmarshaling)
					if partType, exists := partMap["type"]; exists && partType == "image_url" {
						if imgURLMap, ok := partMap["image_url"].(map[string]any); ok {
							if url, ok := imgURLMap["url"].(string); ok {
								imgURL = url
							}
						}
					}
				}
				if imgURL != "" {
					// Extract base64 part from data URL (e.g., "data:image/jpeg;base64,...")
					parts := strings.SplitN(imgURL, ",", 2)
					if len(parts) == 2 {
						multimodalData = append(multimodalData, parts[1])
						marker := mediaMarker
						if marker == "" {
							marker = "<__media__>"
						}
						messages[i] += " " + marker
					}
				}
			}
		}
	}
	prompt := strings.Join(messages, "\n")
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
	if len(llmchunk.Choices) == 0 {
		logger.Warn("LCPChat empty chunk choices", "raw_data", string(data), "chunk", llmchunk)
		return &models.TextChunk{}, nil
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
	// Capture the image attachment paths at the beginning to avoid race conditions
	// with API rotation that might clear the global variable
	localImageAttachments := pendingImageAttachments
	if msg != "" { // otherwise let the bot continue
		// Create the message with support for multimodal content
		var newMsg models.RoleMsg
		// Check if we have images to add to this message
		if len(localImageAttachments) > 0 {
			// Create a multimodal message with text and all images
			newMsg = models.NewMultimodalMsg(role, []interface{}{})
			// Add the text content
			newMsg.AddTextPart(msg)
			// Add all image contents
			for _, imgPath := range localImageAttachments {
				imageURL, err := models.CreateImageURLFromPath(imgPath)
				if err != nil {
					logger.Error("failed to create image URL from path", "error", err, "path", imgPath)
					continue
				}
				newMsg.AddImagePart(imageURL, imgPath)
			}
		} else {
			// Create a simple text message
			newMsg = models.NewRoleMsg(role, msg)
		}
		// Clear the global image attachments after processing in this API call
		pendingImageAttachments = []string{}
		newMsg = *processMessageTag(&newMsg)
		chatBody.Messages = append(chatBody.Messages, newMsg)
		logger.Debug("LCPChat FormMsg: added message to chatBody", "role", newMsg.Role,
			"content_len", len(newMsg.Content), "message_count_after_add", len(chatBody.Messages))
	}
	// sending tool instructions for chat endpoints
	// Update chatBody.Messages with tool guide (persist to stored messages)
	chatBody.Messages = removeToolGuide(chatBody.Messages)
	if cfg.ToolUse && !resume && role == cfg.UserRole {
		chatBody.Messages = prependToolGuide(chatBody.Messages, tools.ToolSysMsgChat)
	}
	filteredMessages, _ := filterMessagesForCurrentCharacter(chatBody.Messages)
	// openai /v1/chat does not support custom roles; needs to be user, assistant, system
	// Add persona suffix to the last user message to indicate who the assistant should reply as
	bodyCopy := &models.ChatBody{
		Messages: make([]models.RoleMsg, len(filteredMessages)),
		Model:    chatBody.Model,
		Stream:   chatBody.Stream,
	}
	for i := range filteredMessages {
		strippedMsg := *stripThinkingFromMsg(&filteredMessages[i])
		switch strippedMsg.Role {
		case cfg.UserRole:
			bodyCopy.Messages[i] = strippedMsg
			bodyCopy.Messages[i].Role = "user"
		case cfg.AssistantRole:
			bodyCopy.Messages[i] = strippedMsg
			bodyCopy.Messages[i].Role = "assistant"
		case cfg.ToolRole:
			bodyCopy.Messages[i] = strippedMsg
			bodyCopy.Messages[i].Role = "tool"
		default:
			bodyCopy.Messages[i] = strippedMsg
		}
		// Clear ToolCalls - they're stored in chat history for display but not sent to LLM
		// bodyCopy.Messages[i].ToolCall = nil
	}
	// Clean null/empty messages to prevent API issues
	bodyCopy.Messages = consolidateAssistantMessages(bodyCopy.Messages)
	req := models.OpenAIReq{
		ChatBody: bodyCopy,
		Tools:    nil,
	}
	if cfg.ToolUse && !resume && role != cfg.ToolRole {
		var allTools []any
		for _, t := range tools.BaseTools {
			allTools = append(allTools, t)
		}
		if mcpManager != nil && mcpManager.HasTools() {
			allTools = append(allTools, mcpManager.GetOpenAITools()...)
		}
		req.Tools = allTools
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
	if len(llmchunk.Choices) == 0 {
		logger.Warn("empty chunk choices", "raw_data", string(data), "chunk", llmchunk)
		return &models.TextChunk{}, nil
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
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{Role: "system", Content: tools.ToolSysMsg})
	}
	filteredMessages, botPersona := filterMessagesForCurrentCharacter(chatBody.Messages)
	messages := make([]string, len(filteredMessages))
	for i := range filteredMessages {
		messages[i] = stripThinkingFromMsg(&filteredMessages[i]).ToPrompt()
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
	if len(llmchunk.Choices) == 0 {
		logger.Warn("empty chunk choices", "raw_data", string(data), "chunk", llmchunk)
		return resp, nil
	}
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
	// sending tool instructions for chat endpoints
	// Update chatBody.Messages with tool guide (persist to stored messages)
	chatBody.Messages = removeToolGuide(chatBody.Messages)
	if cfg.ToolUse && !resume && role == cfg.UserRole {
		chatBody.Messages = prependToolGuide(chatBody.Messages, tools.ToolSysMsgChat)
	}
	filteredMessages, _ := filterMessagesForCurrentCharacter(chatBody.Messages)
	// Create copy of chat body with standardized user role
	// Add persona suffix to the last user message to indicate who the assistant should reply as
	bodyCopy := &models.ChatBody{
		Messages: make([]models.RoleMsg, len(filteredMessages)),
		Model:    chatBody.Model,
		Stream:   chatBody.Stream,
	}
	for i := range filteredMessages {
		strippedMsg := *stripThinkingFromMsg(&filteredMessages[i])
		switch strippedMsg.Role {
		case cfg.UserRole:
			if i == 1 {
				bodyCopy.Messages[i] = strippedMsg
				bodyCopy.Messages[i].Role = "user"
			} else {
				bodyCopy.Messages[i] = strippedMsg
			}
		case cfg.AssistantRole:
			bodyCopy.Messages[i] = strippedMsg
			bodyCopy.Messages[i].Role = "assistant"
		case cfg.ToolRole:
			bodyCopy.Messages[i] = strippedMsg
			bodyCopy.Messages[i].Role = "tool"
		default:
			bodyCopy.Messages[i] = strippedMsg
		}
		// Clear ToolCalls - they're stored in chat history for display but not sent to LLM
		// bodyCopy.Messages[i].ToolCall = nil
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
	if len(llmchunk.Choices) == 0 {
		logger.Warn("empty chunk choices", "raw_data", string(data), "chunk", llmchunk)
		return &models.TextChunk{}, nil
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
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{Role: "system", Content: tools.ToolSysMsg})
	}
	filteredMessages, botPersona := filterMessagesForCurrentCharacter(chatBody.Messages)
	messages := make([]string, len(filteredMessages))
	for i := range filteredMessages {
		messages[i] = stripThinkingFromMsg(&filteredMessages[i]).ToPrompt()
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
	if len(llmchunk.Choices) == 0 {
		logger.Warn("empty chunk choices", "raw_data", string(data), "chunk", llmchunk)
		return &models.TextChunk{}, nil
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
	// Capture the image attachment paths at the beginning to avoid race conditions
	// with API rotation that might clear the global variable
	localImageAttachments := pendingImageAttachments
	if msg != "" { // otherwise let the bot continue
		var newMsg models.RoleMsg
		// Check if we have images to add to this message
		if len(localImageAttachments) > 0 {
			// Create a multimodal message with text and all images
			newMsg = models.NewMultimodalMsg(role, []interface{}{})
			// Add the text content
			newMsg.AddTextPart(msg)
			// Add all image contents
			for _, imgPath := range localImageAttachments {
				imageURL, err := models.CreateImageURLFromPath(imgPath)
				if err != nil {
					logger.Error("failed to create image URL from path", "error", err, "path", imgPath)
					continue
				}
				newMsg.AddImagePart(imageURL, imgPath)
			}
		} else {
			// Create a simple text message
			newMsg = models.NewRoleMsg(role, msg)
		}
		// Clear the global image attachments after processing in this API call
		pendingImageAttachments = []string{}
		newMsg = *processMessageTag(&newMsg)
		chatBody.Messages = append(chatBody.Messages, newMsg)
	}
	// sending tool instructions for chat endpoints
	// Update chatBody.Messages with tool guide (persist to stored messages)
	chatBody.Messages = removeToolGuide(chatBody.Messages)
	if cfg.ToolUse && !resume && role == cfg.UserRole {
		chatBody.Messages = prependToolGuide(chatBody.Messages, tools.ToolSysMsgChat)
	}
	filteredMessages, _ := filterMessagesForCurrentCharacter(chatBody.Messages)
	// Create copy of chat body with standardized user role
	// Add persona suffix to the last user message to indicate who the assistant should reply as
	bodyCopy := &models.ChatBody{
		Messages: make([]models.RoleMsg, len(filteredMessages)),
		Model:    chatBody.Model,
		Stream:   chatBody.Stream,
	}
	for i := range filteredMessages {
		strippedMsg := *stripThinkingFromMsg(&filteredMessages[i])
		switch strippedMsg.Role {
		case cfg.UserRole:
			bodyCopy.Messages[i] = strippedMsg
			bodyCopy.Messages[i].Role = "user"
		case cfg.AssistantRole:
			bodyCopy.Messages[i] = strippedMsg
			bodyCopy.Messages[i].Role = "assistant"
		case cfg.ToolRole:
			bodyCopy.Messages[i] = strippedMsg
			bodyCopy.Messages[i].Role = "tool"
		default:
			bodyCopy.Messages[i] = strippedMsg
		}
		// Clear ToolCalls - they're stored in chat history for display but not sent to LLM
		// literally deletes data that we need
		// bodyCopy.Messages[i].ToolCall = nil
	}
	// Clean null/empty messages to prevent API issues
	bodyCopy.Messages = consolidateAssistantMessages(bodyCopy.Messages)
	orBody := models.NewOpenRouterChatReq(*bodyCopy, defaultLCPProps, cfg.ReasoningEffort)
	if cfg.ToolUse && !resume && role != cfg.ToolRole {
		var allTools []any
		for _, t := range tools.BaseTools {
			allTools = append(allTools, t)
		}
		if mcpManager != nil && mcpManager.HasTools() {
			allTools = append(allTools, mcpManager.GetOpenAITools()...)
		}
		orBody.Tools = allTools
	}
	data, err := json.Marshal(orBody)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil, err
	}
	return bytes.NewReader(data), nil
}
