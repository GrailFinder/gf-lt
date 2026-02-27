package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gf-lt/config"
	"gf-lt/models"
	"gf-lt/rag"
	"gf-lt/storage"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	httpClient      = &http.Client{}
	cfg             *config.Config
	logger          *slog.Logger
	logLevel        = new(slog.LevelVar)
	ctx, cancel     = context.WithCancel(context.Background())
	activeChatName  string
	chatRoundChan   = make(chan *models.ChatRoundReq, 1)
	chunkChan       = make(chan string, 10)
	openAIToolChan  = make(chan string, 10)
	streamDone      = make(chan bool, 1)
	chatBody        *models.ChatBody
	store           storage.FullRepo
	defaultFirstMsg = "Hello! What can I do for you?"
	defaultStarter  = []models.RoleMsg{}
	interruptResp   = false
	ragger          *rag.RAG
	chunkParser     ChunkParser
	lastToolCall    *models.FuncCall
	lastRespStats   *models.ResponseStats
	//nolint:unused // TTS_ENABLED conditionally uses this
	orator          Orator
	asr             STT
	localModelsMu   sync.RWMutex
	defaultLCPProps = map[string]float32{
		"temperature":    0.8,
		"dry_multiplier": 0.0,
		"min_p":          0.05,
		"n_predict":      -1.0,
	}
	ORFreeModels = []string{
		"google/gemini-2.0-flash-exp:free",
		"deepseek/deepseek-chat-v3-0324:free",
		"mistralai/mistral-small-3.2-24b-instruct:free",
		"qwen/qwen3-14b:free",
		"google/gemma-3-27b-it:free",
		"meta-llama/llama-3.3-70b-instruct:free",
	}
	LocalModels = []string{}
)

// parseKnownToTag extracts known_to list from content using configured tag.
// Returns cleaned content and list of character names.
func parseKnownToTag(content string) []string {
	if cfg == nil || !cfg.CharSpecificContextEnabled {
		return nil
	}
	tag := cfg.CharSpecificContextTag
	if tag == "" {
		tag = "@"
	}
	// Pattern: tag + list + "@"
	pattern := regexp.QuoteMeta(tag) + `(.*?)@`
	re := regexp.MustCompile(pattern)
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	// There may be multiple tags; we combine all.
	var knownTo []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		// Remove the entire matched tag from content
		list := strings.TrimSpace(match[1])
		if list == "" {
			continue
		}
		strings.SplitSeq(list, ",")
		// parts := strings.Split(list, ",")
		// for _, p := range parts {
		for p := range strings.SplitSeq(list, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				knownTo = append(knownTo, p)
			}
		}
	}
	// Also remove any leftover trailing "__" that might be orphaned? Not needed.
	return knownTo
}

// processMessageTag processes a message for known_to tag and sets KnownTo field.
// It also ensures the sender's role is included in KnownTo.
// If KnownTo already set (e.g., from DB), preserves it unless new tag found.
func processMessageTag(msg *models.RoleMsg) *models.RoleMsg {
	if cfg == nil || !cfg.CharSpecificContextEnabled {
		return msg
	}
	// If KnownTo already set, assume tag already processed (content cleaned).
	// However, we still check for new tags (maybe added later).
	knownTo := parseKnownToTag(msg.GetText())
	// If tag found, replace KnownTo with new list (merge with existing?)
	// For simplicity, if knownTo is not nil, replace.
	if knownTo == nil {
		return msg
	}
	msg.KnownTo = knownTo
	if msg.Role == "" {
		return msg
	}
	if !slices.Contains(msg.KnownTo, msg.Role) {
		msg.KnownTo = append(msg.KnownTo, msg.Role)
	}
	return msg
}

// filterMessagesForCharacter returns messages visible to the specified character.
// If CharSpecificContextEnabled is false, returns all messages.
func filterMessagesForCharacter(messages []models.RoleMsg, character string) []models.RoleMsg {
	if cfg == nil || !cfg.CharSpecificContextEnabled || character == "" {
		return messages
	}
	if character == "system" { // system sees every message
		return messages
	}
	filtered := make([]models.RoleMsg, 0, len(messages))
	for i := range messages {
		// If KnownTo is nil or empty, message is visible to all
		// system msg cannot be filtered
		if len(messages[i].KnownTo) == 0 || messages[i].Role == "system" {
			filtered = append(filtered, messages[i])
			continue
		}
		if slices.Contains(messages[i].KnownTo, character) {
			// Check if character is in KnownTo lis
			filtered = append(filtered, messages[i])
		}
	}
	return filtered
}

func cleanToolCalls(messages []models.RoleMsg) []models.RoleMsg {
	// If AutoCleanToolCallsFromCtx is false, keep tool call messages in context
	if cfg != nil && !cfg.AutoCleanToolCallsFromCtx {
		return consolidateAssistantMessages(messages)
	}
	cleaned := make([]models.RoleMsg, 0, len(messages))
	for i := range messages {
		// recognize the message as the tool call and remove it
		// tool call in last msg should stay
		if messages[i].ToolCallID == "" || i == len(messages)-1 {
			cleaned = append(cleaned, messages[i])
		}
	}
	return consolidateAssistantMessages(cleaned)
}

// consolidateAssistantMessages merges consecutive assistant messages into a single message
func consolidateAssistantMessages(messages []models.RoleMsg) []models.RoleMsg {
	if len(messages) == 0 {
		return messages
	}
	consolidated := make([]models.RoleMsg, 0, len(messages))
	currentAssistantMsg := models.RoleMsg{}
	isBuildingAssistantMsg := false
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		// assistant role only
		if msg.Role == cfg.AssistantRole {
			// If this is an assistant message, start or continue building
			if !isBuildingAssistantMsg {
				// Start accumulating assistant message
				currentAssistantMsg = msg.Copy()
				isBuildingAssistantMsg = true
			} else {
				// Continue accumulating - append content to the current assistant message
				if currentAssistantMsg.IsContentParts() || msg.IsContentParts() {
					// Handle structured content
					if !currentAssistantMsg.IsContentParts() {
						// Preserve the original ToolCallID before conversion
						originalToolCallID := currentAssistantMsg.ToolCallID
						// Convert existing content to content parts
						currentAssistantMsg = models.NewMultimodalMsg(currentAssistantMsg.Role, []interface{}{models.TextContentPart{Type: "text", Text: currentAssistantMsg.Content}})
						// Restore the original ToolCallID to preserve tool call linking
						currentAssistantMsg.ToolCallID = originalToolCallID
					}
					if msg.IsContentParts() {
						currentAssistantMsg.ContentParts = append(currentAssistantMsg.ContentParts, msg.GetContentParts()...)
					} else if msg.Content != "" {
						currentAssistantMsg.AddTextPart(msg.Content)
					}
				} else {
					// Simple string content
					if currentAssistantMsg.Content != "" {
						currentAssistantMsg.Content += "\n" + msg.Content
					} else {
						currentAssistantMsg.Content = msg.Content
					}
					// ToolCallID is already preserved since we're not creating a new message object when just concatenating content
				}
			}
		} else {
			// This is not an assistant message
			// If we were building an assistant message, add it to the result
			if isBuildingAssistantMsg {
				consolidated = append(consolidated, currentAssistantMsg)
				isBuildingAssistantMsg = false
			}
			// Add the non-assistant message
			consolidated = append(consolidated, msg)
		}
	}
	// Don't forget the last assistant message if we were building one
	if isBuildingAssistantMsg {
		consolidated = append(consolidated, currentAssistantMsg)
	}
	return consolidated
}

// GetLogLevel returns the current log level as a string
func GetLogLevel() string {
	level := logLevel.Level()
	switch level {
	case slog.LevelDebug:
		return "Debug"
	case slog.LevelInfo:
		return "Info"
	case slog.LevelWarn:
		return "Warn"
	default:
		// For any other values, return "Info" as default
		return "Info"
	}
}

func createClient(connectTimeout time.Duration) *http.Client {
	// Custom transport with connection timeout
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Create a dialer with connection timeout
			dialer := &net.Dialer{
				Timeout:   connectTimeout,
				KeepAlive: 30 * time.Second, // Optional
			}
			return dialer.DialContext(ctx, network, addr)
		},
		// Other transport settings (optional)
		TLSHandshakeTimeout:   connectTimeout,
		ResponseHeaderTimeout: connectTimeout,
	}
	// Client with no overall timeout (or set to streaming-safe duration)
	return &http.Client{
		Transport: transport,
		Timeout:   0, // No overall timeout (for streaming)
	}
}

func warmUpModel() {
	u, err := url.Parse(cfg.CurrentAPI)
	if err != nil {
		return
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return
	}
	// Check if model is already loaded
	loaded, err := isModelLoaded(chatBody.Model)
	if err != nil {
		logger.Debug("failed to check model status", "model", chatBody.Model, "error", err)
		// Continue with warmup attempt anyway
	}
	if loaded {
		if err := notifyUser("model already loaded", "Model "+chatBody.Model+" is already loaded."); err != nil {
			logger.Debug("failed to notify user", "error", err)
		}
		return
	}
	go func() {
		var data []byte
		var err error
		switch {
		case strings.HasSuffix(cfg.CurrentAPI, "/completion"):
			// Old completion endpoint
			req := models.NewLCPReq(".", chatBody.Model, nil, map[string]float32{
				"temperature":    0.8,
				"dry_multiplier": 0.0,
				"min_p":          0.05,
				"n_predict":      0,
			}, []string{})
			req.Stream = false
			data, err = json.Marshal(req)
		case strings.Contains(cfg.CurrentAPI, "/v1/chat/completions"):
			// OpenAI-compatible chat endpoint
			req := models.OpenAIReq{
				ChatBody: &models.ChatBody{
					Model: chatBody.Model,
					Messages: []models.RoleMsg{
						{Role: "system", Content: "."},
					},
					Stream: false,
				},
				Tools: nil,
			}
			data, err = json.Marshal(req)
		default:
			// Unknown local endpoint, skip
			return
		}
		if err != nil {
			logger.Debug("failed to marshal warmup request", "error", err)
			return
		}
		resp, err := httpClient.Post(cfg.CurrentAPI, "application/json", bytes.NewReader(data))
		if err != nil {
			logger.Debug("warmup request failed", "error", err)
			return
		}
		resp.Body.Close()
		// Start monitoring for model load completion
		monitorModelLoad(chatBody.Model)
	}()
}

// nolint
func fetchDSBalance() *models.DSBalance {
	url := "https://api.deepseek.com/user/balance"
	method := "GET"
	// nolint
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		logger.Warn("failed to create request", "error", err)
		return nil
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", "Bearer "+cfg.DeepSeekToken)
	res, err := httpClient.Do(req)
	if err != nil {
		logger.Warn("failed to make request", "error", err)
		return nil
	}
	defer res.Body.Close()
	resp := models.DSBalance{}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil
	}
	return &resp
}

func fetchORModels(free bool) ([]string, error) {
	resp, err := http.Get("https://openrouter.ai/api/v1/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err := fmt.Errorf("failed to fetch or models; status: %s", resp.Status)
		return nil, err
	}
	data := &models.ORModels{}
	if err := json.NewDecoder(resp.Body).Decode(data); err != nil {
		return nil, err
	}
	freeModels := data.ListModels(free)
	return freeModels, nil
}

func fetchLCPModels() ([]string, error) {
	resp, err := http.Get(cfg.FetchModelNameAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err := fmt.Errorf("failed to fetch or models; status: %s", resp.Status)
		return nil, err
	}
	data := &models.LCPModels{}
	if err := json.NewDecoder(resp.Body).Decode(data); err != nil {
		return nil, err
	}
	localModels := data.ListModels()
	return localModels, nil
}

// fetchLCPModelsWithLoadStatus returns models with "(loaded)" indicator for loaded models
func fetchLCPModelsWithLoadStatus() ([]string, error) {
	models, err := fetchLCPModelsWithStatus()
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(models.Data))
	li := 0 // loaded index
	for i, m := range models.Data {
		modelName := m.ID
		if m.Status.Value == "loaded" {
			modelName = "(loaded) " + modelName
			li = i
		}
		result = append(result, modelName)
	}
	if li == 0 {
		return result, nil // no loaded models
	}
	loadedModel := result[li]
	result = append(result[:li], result[li+1:]...)
	return slices.Concat([]string{loadedModel}, result), nil
}

// fetchLCPModelsWithStatus returns the full LCPModels struct including status information.
func fetchLCPModelsWithStatus() (*models.LCPModels, error) {
	resp, err := http.Get(cfg.FetchModelNameAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err := fmt.Errorf("failed to fetch llama.cpp models; status: %s", resp.Status)
		return nil, err
	}
	data := &models.LCPModels{}
	if err := json.NewDecoder(resp.Body).Decode(data); err != nil {
		return nil, err
	}
	return data, nil
}

// isModelLoaded checks if the given model ID is currently loaded in llama.cpp server.
func isModelLoaded(modelID string) (bool, error) {
	models, err := fetchLCPModelsWithStatus()
	if err != nil {
		return false, err
	}
	for _, m := range models.Data {
		if m.ID == modelID {
			return m.Status.Value == "loaded", nil
		}
	}
	return false, nil
}

// monitorModelLoad starts a goroutine that periodically checks if the specified model is loaded.
func monitorModelLoad(modelID string) {
	go func() {
		timeout := time.After(2 * time.Minute) // max wait 2 minutes
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-timeout:
				logger.Debug("model load monitoring timeout", "model", modelID)
				return
			case <-ticker.C:
				loaded, err := isModelLoaded(modelID)
				if err != nil {
					logger.Debug("failed to check model status", "model", modelID, "error", err)
					continue
				}
				if loaded {
					if err := notifyUser("model loaded", "Model "+modelID+" is now loaded and ready."); err != nil {
						logger.Debug("failed to notify user", "error", err)
					}
					refreshChatDisplay()
					return
				}
			}
		}
	}()
}

// extractDetailedErrorFromBytes extracts detailed error information from response body bytes
func extractDetailedErrorFromBytes(body []byte, statusCode int) string {
	// Try to parse as JSON to extract detailed error information
	var errorResponse map[string]any
	if err := json.Unmarshal(body, &errorResponse); err == nil {
		// Check if it's an error response with detailed information
		if errorData, ok := errorResponse["error"]; ok {
			if errorMap, ok := errorData.(map[string]any); ok {
				var errorMsg string
				if msg, ok := errorMap["message"]; ok {
					errorMsg = fmt.Sprintf("%v", msg)
				}
				var details []string
				if code, ok := errorMap["code"]; ok {
					details = append(details, fmt.Sprintf("Code: %v", code))
				}
				if metadata, ok := errorMap["metadata"]; ok {
					// Handle metadata which might contain raw error details
					if metadataMap, ok := metadata.(map[string]any); ok {
						if raw, ok := metadataMap["raw"]; ok {
							// Parse the raw error string if it's JSON
							var rawError map[string]any
							if rawStr, ok := raw.(string); ok && json.Unmarshal([]byte(rawStr), &rawError) == nil {
								if rawErrorData, ok := rawError["error"]; ok {
									if rawErrorMap, ok := rawErrorData.(map[string]any); ok {
										if rawMsg, ok := rawErrorMap["message"]; ok {
											return fmt.Sprintf("API Error: %s", rawMsg)
										}
									}
								}
							}
						}
					}
					details = append(details, fmt.Sprintf("Metadata: %v", metadata))
				}
				if len(details) > 0 {
					return fmt.Sprintf("API Error: %s (%s)", errorMsg, strings.Join(details, ", "))
				}
				return "API Error: " + errorMsg
			}
		}
	}
	// If not a structured error response, return the raw body with status
	return fmt.Sprintf("HTTP Status: %d, Response Body: %s", statusCode, string(body))
}

func finalizeRespStats(tokenCount int, startTime time.Time) {
	duration := time.Since(startTime).Seconds()
	var tps float64
	if duration > 0 {
		tps = float64(tokenCount) / duration
	}
	lastRespStats = &models.ResponseStats{
		Tokens:       tokenCount,
		Duration:     duration,
		TokensPerSec: tps,
	}
}

// sendMsgToLLM expects streaming resp
func sendMsgToLLM(body io.Reader) {
	choseChunkParser()
	// openrouter does not respect stop strings, so we have to cut the message ourselves
	stopStrings := chatBody.MakeStopSliceExcluding("", listChatRoles())
	req, err := http.NewRequest("POST", cfg.CurrentAPI, body)
	if err != nil {
		logger.Error("newreq error", "error", err)
		if err := notifyUser("error", "apicall failed:"+err.Error()); err != nil {
			logger.Error("failed to notify", "error", err)
		}
		streamDone <- true
		return
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+chunkParser.GetToken())
	req.Header.Set("Accept-Encoding", "gzip")
	// nolint
	resp, err := httpClient.Do(req)
	if err != nil {
		logger.Error("llamacpp api", "error", err)
		if err := notifyUser("error", "apicall failed:"+err.Error()); err != nil {
			logger.Error("failed to notify", "error", err)
		}
		streamDone <- true
		return
	}
	// Check if the initial response is an error before starting to stream
	if resp.StatusCode >= 400 {
		// Read the response body to get detailed error information
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.Error("failed to read error response body", "error", err, "status_code", resp.StatusCode)
			detailedError := fmt.Sprintf("HTTP Status: %d, Failed to read response body: %v", resp.StatusCode, err)
			if err := notifyUser("API Error", detailedError); err != nil {
				logger.Error("failed to notify", "error", err)
			}
			resp.Body.Close()
			streamDone <- true
			return
		}
		// Parse the error response for detailed information
		detailedError := extractDetailedErrorFromBytes(bodyBytes, resp.StatusCode)
		logger.Error("API returned error status", "status_code", resp.StatusCode, "detailed_error", detailedError)
		if err := notifyUser("API Error", detailedError); err != nil {
			logger.Error("failed to notify", "error", err)
		}
		resp.Body.Close()
		streamDone <- true
		return
	}
	//
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	counter := uint32(0)
	tokenCount := 0
	startTime := time.Now()
	hasReasoning := false
	reasoningSent := false
	defer func() {
		finalizeRespStats(tokenCount, startTime)
	}()
	for {
		var (
			answerText string
			chunk      *models.TextChunk
		)
		counter++
		// to stop from spiriling in infinity read of bad bytes that happens with poor connection
		if cfg.ChunkLimit > 0 && counter > cfg.ChunkLimit {
			logger.Warn("response hit chunk limit", "limit", cfg.ChunkLimit)
			streamDone <- true
			break
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			// Check if this is an EOF error and if the response contains detailed error information
			if err == io.EOF {
				// For streaming responses, we may have already consumed the error body
				// So we'll use the original status code to provide context
				detailedError := fmt.Sprintf("Streaming connection closed unexpectedly (Status: %d). This may indicate an API error. Check your API provider and model settings.", resp.StatusCode)
				logger.Error("error reading response body", "error", err, "detailed_error", detailedError,
					"status_code", resp.StatusCode, "user_role", cfg.UserRole, "parser", chunkParser, "link", cfg.CurrentAPI)
				if err := notifyUser("API Error", detailedError); err != nil {
					logger.Error("failed to notify", "error", err)
				}
			} else {
				logger.Error("error reading response body", "error", err, "line", string(line),
					"user_role", cfg.UserRole, "parser", chunkParser, "link", cfg.CurrentAPI)
				// if err.Error() != "EOF" {
				if err := notifyUser("API error", err.Error()); err != nil {
					logger.Error("failed to notify", "error", err)
				}
			}
			streamDone <- true
			break
			// }
			// continue
		}
		if len(line) <= 1 {
			if interruptResp {
				goto interrupt // get unstuck from bad connection
			}
			continue // skip \n
		}
		// starts with -> data:
		line = line[6:]
		logger.Debug("debugging resp", "line", string(line))
		if bytes.Equal(line, []byte("[DONE]\n")) {
			streamDone <- true
			break
		}
		if bytes.Equal(line, []byte("ROUTER PROCESSING\n")) {
			continue
		}
		chunk, err = chunkParser.ParseChunk(line)
		if err != nil {
			logger.Error("error parsing response body", "error", err,
				"line", string(line), "url", cfg.CurrentAPI)
			if err := notifyUser("LLM Response Error", "Failed to parse LLM response: "+err.Error()); err != nil {
				logger.Error("failed to notify user", "error", err)
			}
			streamDone <- true
			break
		}
		// // problem: this catches any mention of the word 'error'
		// Handle error messages in response content
		// example needed, since llm could use the word error in the normal msg
		// if string(line) != "" && strings.Contains(strings.ToLower(string(line)), "error") {
		// 	logger.Error("API error response detected", "line", line, "url", cfg.CurrentAPI)
		// 	streamDone <- true
		// 	break
		// }
		if chunk.Finished {
			// Close the thinking block if we were streaming reasoning and haven't closed it yet
			if hasReasoning && !reasoningSent {
				chunkChan <- "</think>"
				tokenCount++
			}
			if chunk.Chunk != "" {
				logger.Warn("text inside of finish llmchunk", "chunk", chunk, "counter", counter)
				answerText = strings.ReplaceAll(chunk.Chunk, "\n\n", "\n")
				chunkChan <- answerText
				tokenCount++
			}
			streamDone <- true
			break
		}
		if counter == 0 {
			chunk.Chunk = strings.TrimPrefix(chunk.Chunk, " ")
		}
		// Handle reasoning chunks - stream them immediately as they arrive
		if chunk.Reasoning != "" && !reasoningSent {
			if !hasReasoning {
				// First reasoning chunk - send opening tag
				chunkChan <- "<think>"
				tokenCount++
				hasReasoning = true
			}
			// Stream reasoning content immediately
			answerText = strings.ReplaceAll(chunk.Reasoning, "\n\n", "\n")
			if answerText != "" {
				chunkChan <- answerText
				tokenCount++
			}
		}
		// When we get content and have been streaming reasoning, close the thinking block
		if chunk.Chunk != "" && hasReasoning && !reasoningSent {
			// Close the thinking block before sending actual content
			chunkChan <- "</think>"
			tokenCount++
			reasoningSent = true
		}
		// bot sends way too many \n
		answerText = strings.ReplaceAll(chunk.Chunk, "\n\n", "\n")
		// Accumulate text to check for stop strings that might span across chunks
		// check if chunk is in stopstrings => stop
		// this check is needed only for openrouter /v1/completion, since it does not respect stop slice
		if chunkParser.GetAPIType() == models.APITypeCompletion &&
			slices.Contains(stopStrings, answerText) {
			logger.Debug("stop string detected on client side for completion endpoint", "stop_string", answerText)
			streamDone <- true
			break
		}
		if answerText != "" {
			chunkChan <- answerText
			tokenCount++
		}
		openAIToolChan <- chunk.ToolChunk
		if chunk.FuncName != "" {
			lastToolCall.Name = chunk.FuncName
			// Store the tool call ID for the response
			lastToolCall.ID = chunk.ToolID
		}
	interrupt:
		if interruptResp { // read bytes, so it would not get into beginning of the next req
			interruptResp = false
			logger.Info("interrupted bot response", "chunk_counter", counter)
			streamDone <- true
			break
		}
	}
}

func roleToIcon(role string) string {
	return "<" + role + ">: "
}

func chatWatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case chatRoundReq := <-chatRoundChan:
			if err := chatRound(chatRoundReq); err != nil {
				logger.Error("failed to chatRound", "err", err)
			}
		}
	}
}

// inpired by https://github.com/rivo/tview/issues/225
func showSpinner() {
	spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	var i int
	botPersona := cfg.AssistantRole
	if cfg.WriteNextMsgAsCompletionAgent != "" {
		botPersona = cfg.WriteNextMsgAsCompletionAgent
	}
	for botRespMode || toolRunningMode {
		time.Sleep(400 * time.Millisecond)
		spin := i % len(spinners)
		app.QueueUpdateDraw(func() {
			switch {
			case toolRunningMode:
				textArea.SetTitle(spinners[spin] + " tool")
			case botRespMode:
				textArea.SetTitle(spinners[spin] + " " + botPersona + " (F6 to interrupt)")
			default:
				textArea.SetTitle(spinners[spin] + " input")
			}
		})
		i++
	}
	app.QueueUpdateDraw(func() {
		textArea.SetTitle("input")
	})
}

func chatRound(r *models.ChatRoundReq) error {
	botRespMode = true
	go showSpinner()
	updateStatusLine()
	botPersona := cfg.AssistantRole
	if cfg.WriteNextMsgAsCompletionAgent != "" {
		botPersona = cfg.WriteNextMsgAsCompletionAgent
	}
	defer func() {
		botRespMode = false
		ClearImageAttachment()
	}()
	// check that there is a model set to use if is not local
	choseChunkParser()
	reader, err := chunkParser.FormMsg(r.UserMsg, r.Role, r.Resume)
	if reader == nil || err != nil {
		logger.Error("empty reader from msgs", "role", r.Role, "error", err)
		return err
	}
	if cfg.SkipLLMResp {
		return nil
	}
	go sendMsgToLLM(reader)
	logger.Debug("looking at vars in chatRound", "msg", r.UserMsg, "regen", r.Regen, "resume", r.Resume)
	msgIdx := len(chatBody.Messages)
	if !r.Resume {
		// Add empty message to chatBody immediately so it persists during Alt+T toggle
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{
			Role: botPersona, Content: "",
		})
		nl := "\n\n"
		prevText := textView.GetText(true)
		if strings.HasSuffix(prevText, nl) {
			nl = ""
		} else if strings.HasSuffix(prevText, "\n") {
			nl = "\n"
		}
		fmt.Fprintf(textView, "%s[-:-:b](%d) %s[-:-:-]\n", nl, msgIdx, roleToIcon(botPersona))
	} else {
		msgIdx = len(chatBody.Messages) - 1
	}
	respText := strings.Builder{}
	toolResp := strings.Builder{}
	// Variables for handling thinking blocks during streaming
	inThinkingBlock := false
	thinkingBuffer := strings.Builder{}
	justExitedThinkingCollapsed := false
out:
	for {
		select {
		case chunk := <-chunkChan:
			// Handle thinking blocks during streaming
			if strings.HasPrefix(chunk, "<think>") && !inThinkingBlock {
				// Start of thinking block
				inThinkingBlock = true
				thinkingBuffer.Reset()
				thinkingBuffer.WriteString(chunk)
				if thinkingCollapsed {
					// Show placeholder immediately when thinking starts in collapsed mode
					fmt.Fprint(textView, "[yellow::i][thinking... (press Alt+T to expand)][-:-:-]")
					if scrollToEndEnabled {
						textView.ScrollToEnd()
					}
					respText.WriteString(chunk)
					continue
				}
			} else if inThinkingBlock {
				thinkingBuffer.WriteString(chunk)
				if strings.Contains(chunk, "</think>") {
					// End of thinking block
					inThinkingBlock = false
					if thinkingCollapsed {
						// Thinking already displayed as placeholder, just update respText
						respText.WriteString(chunk)
						justExitedThinkingCollapsed = true
						if scrollToEndEnabled {
							textView.ScrollToEnd()
						}
						continue
					}
					// If not collapsed, fall through to normal display
				} else if thinkingCollapsed {
					// Still in thinking block and collapsed - just buffer, don't display
					respText.WriteString(chunk)
					continue
				}
				// If not collapsed, fall through to normal display
			}
			// Add spacing after collapsed thinking block before real response
			if justExitedThinkingCollapsed {
				chunk = "\n\n" + chunk
				justExitedThinkingCollapsed = false
			}
			fmt.Fprint(textView, chunk)
			respText.WriteString(chunk)
			// Update the message in chatBody.Messages so it persists during Alt+T
			chatBody.Messages[msgIdx].Content = respText.String()
			if scrollToEndEnabled {
				textView.ScrollToEnd()
			}
			// Send chunk to audio stream handler
			if cfg.TTS_ENABLED {
				TTSTextChan <- chunk
			}
		case toolChunk := <-openAIToolChan:
			fmt.Fprint(textView, toolChunk)
			toolResp.WriteString(toolChunk)
			if scrollToEndEnabled {
				textView.ScrollToEnd()
			}
		case <-streamDone:
			for len(chunkChan) > 0 {
				chunk := <-chunkChan
				fmt.Fprint(textView, chunk)
				respText.WriteString(chunk)
				if scrollToEndEnabled {
					textView.ScrollToEnd()
				}
				if cfg.TTS_ENABLED {
					TTSTextChan <- chunk
				}
			}
			if cfg.TTS_ENABLED {
				TTSFlushChan <- true
			}
			break out
		}
	}
	var msgStats *models.ResponseStats
	if lastRespStats != nil {
		msgStats = &models.ResponseStats{
			Tokens:       lastRespStats.Tokens,
			Duration:     lastRespStats.Duration,
			TokensPerSec: lastRespStats.TokensPerSec,
		}
		lastRespStats = nil
	}
	botRespMode = false
	if r.Resume {
		chatBody.Messages[len(chatBody.Messages)-1].Content += respText.String()
		updatedMsg := chatBody.Messages[len(chatBody.Messages)-1]
		processedMsg := processMessageTag(&updatedMsg)
		chatBody.Messages[len(chatBody.Messages)-1] = *processedMsg
		if msgStats != nil && chatBody.Messages[len(chatBody.Messages)-1].Role != cfg.ToolRole {
			chatBody.Messages[len(chatBody.Messages)-1].Stats = msgStats
		}
	} else {
		chatBody.Messages[msgIdx].Content = respText.String()
		processedMsg := processMessageTag(&chatBody.Messages[msgIdx])
		chatBody.Messages[msgIdx] = *processedMsg
		if msgStats != nil && chatBody.Messages[msgIdx].Role != cfg.ToolRole {
			chatBody.Messages[msgIdx].Stats = msgStats
		}
		stopTTSIfNotForUser(&chatBody.Messages[msgIdx])
	}
	cleanChatBody()
	refreshChatDisplay()
	updateStatusLine()
	// bot msg is done;
	// now check it for func call
	// logChat(activeChatName, chatBody.Messages)
	if err := updateStorageChat(activeChatName, chatBody.Messages); err != nil {
		logger.Warn("failed to update storage", "error", err, "name", activeChatName)
	}
	if findCall(respText.String(), toolResp.String()) {
		return nil
	}
	// Check if this message was sent privately to specific characters
	// If so, trigger those characters to respond if that char is not controlled by user
	// perhaps we should have narrator role to determine which char is next to act
	if cfg.AutoTurn {
		lastMsg := chatBody.Messages[len(chatBody.Messages)-1]
		if len(lastMsg.KnownTo) > 0 {
			triggerPrivateMessageResponses(&lastMsg)
		}
	}
	return nil
}

// cleanChatBody removes messages with null or empty content to prevent API issues
func cleanChatBody() {
	if chatBody == nil || chatBody.Messages == nil {
		return
	}
	// Tool request cleaning is now configurable via AutoCleanToolCallsFromCtx (default false)
	// /completion msg where part meant for user and other part tool call
	chatBody.Messages = cleanToolCalls(chatBody.Messages)
	chatBody.Messages = consolidateAssistantMessages(chatBody.Messages)
}

// convertJSONToMapStringString unmarshals JSON into map[string]interface{} and converts all values to strings.
func convertJSONToMapStringString(jsonStr string) (map[string]string, error) {
	// Extract JSON object from string - models may output extra text after JSON
	jsonStr = extractJSON(jsonStr)
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, err
	}
	result := make(map[string]string, len(raw))
	for k, v := range raw {
		switch val := v.(type) {
		case string:
			result[k] = val
		case float64:
			result[k] = strconv.FormatFloat(val, 'f', -1, 64)
		case int, int64, int32:
			// json.Unmarshal converts numbers to float64, but handle other integer types if they appear
			result[k] = fmt.Sprintf("%v", val)
		case bool:
			result[k] = strconv.FormatBool(val)
		case nil:
			result[k] = ""
		default:
			result[k] = fmt.Sprintf("%v", val)
		}
	}
	return result, nil
}

// extractJSON finds the first { and last } to extract only the JSON object
// This handles cases where models output extra text after JSON
func extractJSON(s string) string {
	// Try direct parse first - if it works, return as-is
	var dummy map[string]interface{}
	if err := json.Unmarshal([]byte(s), &dummy); err == nil {
		return s
	}
	// Otherwise find JSON boundaries
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

// unmarshalFuncCall unmarshals a JSON tool call, converting numeric arguments to strings.
func unmarshalFuncCall(jsonStr string) (*models.FuncCall, error) {
	type tempFuncCall struct {
		ID   string                 `json:"id,omitempty"`
		Name string                 `json:"name"`
		Args map[string]interface{} `json:"args"`
	}
	var temp tempFuncCall
	if err := json.Unmarshal([]byte(jsonStr), &temp); err != nil {
		return nil, err
	}
	fc := &models.FuncCall{
		ID:   temp.ID,
		Name: temp.Name,
		Args: make(map[string]string, len(temp.Args)),
	}
	for k, v := range temp.Args {
		switch val := v.(type) {
		case string:
			fc.Args[k] = val
		case float64:
			fc.Args[k] = strconv.FormatFloat(val, 'f', -1, 64)
		case int, int64, int32:
			fc.Args[k] = fmt.Sprintf("%v", val)
		case bool:
			fc.Args[k] = strconv.FormatBool(val)
		case nil:
			fc.Args[k] = ""
		default:
			fc.Args[k] = fmt.Sprintf("%v", val)
		}
	}
	return fc, nil
}

// findCall: adds chatRoundReq into the chatRoundChan and returns true if does
func findCall(msg, toolCall string) bool {
	var fc *models.FuncCall
	if toolCall != "" {
		// HTML-decode the tool call string to handle encoded characters like &lt; -> <=
		decodedToolCall := html.UnescapeString(toolCall)
		openAIToolMap, err := convertJSONToMapStringString(decodedToolCall)
		if err != nil {
			logger.Error("failed to unmarshal openai tool call", "call", decodedToolCall, "error", err)
			// Ensure lastToolCall.ID is set for the error response (already set from chunk)
			// Send error response to LLM so it can retry or handle the error
			toolResponseMsg := models.RoleMsg{
				Role:       cfg.ToolRole,
				Content:    fmt.Sprintf("Error processing tool call: %v. Please check the JSON format and try again.", err),
				ToolCallID: lastToolCall.ID, // Use the stored tool call ID
			}
			chatBody.Messages = append(chatBody.Messages, toolResponseMsg)
			// Clear the stored tool call ID after using it (no longer needed)
			// Trigger the assistant to continue processing with the error message
			crr := &models.ChatRoundReq{
				Role: cfg.AssistantRole,
			}
			// provoke next llm msg after failed tool call
			chatRoundChan <- crr
			// chatRound("", cfg.AssistantRole, tv, false, false)
			return true
		}
		lastToolCall.Args = openAIToolMap
		fc = lastToolCall
		// NOTE: We do NOT override lastToolCall.ID from arguments.
		// The ID should come from the streaming response (chunk.ToolID) set earlier.
		// Some tools like todo_create have "id" in their arguments which is NOT the tool call ID.
	} else {
		jsStr := toolCallRE.FindString(msg)
		if jsStr == "" { // no tool call case
			return false
		}
		prefix := "__tool_call__\n"
		suffix := "\n__tool_call__"
		jsStr = strings.TrimSuffix(strings.TrimPrefix(jsStr, prefix), suffix)
		// HTML-decode the JSON string to handle encoded characters like &lt; -> <=
		decodedJsStr := html.UnescapeString(jsStr)
		var err error
		fc, err = unmarshalFuncCall(decodedJsStr)
		if err != nil {
			logger.Error("failed to unmarshal tool call", "error", err, "json_string", decodedJsStr)
			// Send error response to LLM so it can retry or handle the error
			toolResponseMsg := models.RoleMsg{
				Role:    cfg.ToolRole,
				Content: fmt.Sprintf("Error processing tool call: %v. Please check the JSON format and try again.", err),
			}
			chatBody.Messages = append(chatBody.Messages, toolResponseMsg)
			logger.Debug("findCall: added tool error response", "role", toolResponseMsg.Role, "content_len", len(toolResponseMsg.Content), "message_count_after_add", len(chatBody.Messages))
			// Trigger the assistant to continue processing with the error message
			// chatRound("", cfg.AssistantRole, tv, false, false)
			crr := &models.ChatRoundReq{
				Role: cfg.AssistantRole,
			}
			// provoke next llm msg after failed tool call
			chatRoundChan <- crr
			return true
		}
		// Update lastToolCall with parsed function call
		lastToolCall.ID = fc.ID
		lastToolCall.Name = fc.Name
		lastToolCall.Args = fc.Args
	}
	// we got here => last msg recognized as a tool call (correct or not)
	// Use the tool call ID from streaming response (lastToolCall.ID)
	// Don't generate random ID - the ID should match between assistant message and tool response
	lastMsgIdx := len(chatBody.Messages) - 1
	if lastToolCall.ID != "" {
		chatBody.Messages[lastMsgIdx].ToolCallID = lastToolCall.ID
	}
	// Store tool call info in the assistant message
	// Convert Args map to JSON string for storage
	argsJSON, _ := json.Marshal(lastToolCall.Args)
	chatBody.Messages[lastMsgIdx].ToolCalls = []models.ToolCall{
		{
			ID:   lastToolCall.ID,
			Name: lastToolCall.Name,
			Args: string(argsJSON),
		},
	}
	// call a func
	_, ok := fnMap[fc.Name]
	if !ok {
		m := fc.Name + " is not implemented"
		// Create tool response message with the proper tool_call_id
		toolResponseMsg := models.RoleMsg{
			Role:       cfg.ToolRole,
			Content:    m,
			ToolCallID: lastToolCall.ID, // Use the stored tool call ID
		}
		chatBody.Messages = append(chatBody.Messages, toolResponseMsg)
		logger.Debug("findCall: added tool not implemented response", "role", toolResponseMsg.Role, "content_len", len(toolResponseMsg.Content), "tool_call_id", toolResponseMsg.ToolCallID, "message_count_after_add", len(chatBody.Messages))
		// Clear the stored tool call ID after using it
		lastToolCall.ID = ""
		// Trigger the assistant to continue processing with the new tool response
		// by calling chatRound with empty content to continue the assistant's response
		crr := &models.ChatRoundReq{
			Role: cfg.AssistantRole,
		}
		// failed to find tool
		chatRoundChan <- crr
		return true
	}
	// Show tool call progress indicator before execution
	fmt.Fprintf(textView, "\n[yellow::i][tool: %s...][-:-:-]", fc.Name)
	toolRunningMode = true
	resp := callToolWithAgent(fc.Name, fc.Args)
	toolRunningMode = false
	toolMsg := string(resp)
	logger.Info("llm used a tool call", "tool_name", fc.Name, "too_args", fc.Args, "id", fc.ID, "tool_resp", toolMsg)
	fmt.Fprintf(textView, "%s[-:-:b](%d) <%s>: [-:-:-]\n%s\n",
		"\n\n", len(chatBody.Messages), cfg.ToolRole, toolMsg)
	// Create tool response message with the proper tool_call_id
	// Mark shell commands as always visible
	isShellCommand := fc.Name == "execute_command"
	toolResponseMsg := models.RoleMsg{
		Role:           cfg.ToolRole,
		Content:        toolMsg,
		ToolCallID:     lastToolCall.ID,
		IsShellCommand: isShellCommand,
	}
	chatBody.Messages = append(chatBody.Messages, toolResponseMsg)
	logger.Debug("findCall: added actual tool response", "role", toolResponseMsg.Role, "content_len", len(toolResponseMsg.Content), "tool_call_id", toolResponseMsg.ToolCallID, "message_count_after_add", len(chatBody.Messages))
	// Clear the stored tool call ID after using it
	lastToolCall.ID = ""
	// Trigger the assistant to continue processing with the new tool response
	// by calling chatRound with empty content to continue the assistant's response
	crr := &models.ChatRoundReq{
		Role: cfg.AssistantRole,
	}
	chatRoundChan <- crr
	return true
}

func chatToTextSlice(messages []models.RoleMsg, showSys bool) []string {
	resp := make([]string, len(messages))
	for i := range messages {
		// Handle tool call indicators (assistant messages with tool call but empty content)
		if (messages[i].Role == cfg.AssistantRole || messages[i].Role == "assistant") && messages[i].ToolCallID != "" && messages[i].Content == "" && len(messages[i].ToolCalls) > 0 {
			// This is a tool call indicator - show collapsed
			if toolCollapsed {
				toolName := messages[i].ToolCalls[0].Name
				resp[i] = fmt.Sprintf("[yellow::i][tool call: %s (press Ctrl+T to expand)][-:-:-]", toolName)
			} else {
				// Show full tool call info
				toolName := messages[i].ToolCalls[0].Name
				resp[i] = fmt.Sprintf("[yellow::i][tool call: %s][-:-:-]\nargs: %s", toolName, messages[i].ToolCalls[0].Args)
			}
			continue
		}
		// Handle tool responses
		if messages[i].Role == cfg.ToolRole || messages[i].Role == "tool" {
			// Always show shell commands
			if messages[i].IsShellCommand {
				resp[i] = messages[i].ToText(i)
				continue
			}
			// Hide non-shell tool responses when collapsed
			if toolCollapsed {
				continue
			}
			// When expanded, show tool responses
			resp[i] = messages[i].ToText(i)
			continue
		}
		// INFO: skips system msg when showSys is false
		if !showSys && messages[i].Role == "system" {
			continue
		}
		resp[i] = messages[i].ToText(i)
	}
	return resp
}

func chatToText(messages []models.RoleMsg, showSys bool) string {
	s := chatToTextSlice(messages, showSys)
	text := strings.Join(s, "\n")
	// Collapse thinking blocks if enabled
	if thinkingCollapsed {
		text = thinkRE.ReplaceAllStringFunc(text, func(match string) string {
			// Extract content between <think> and </think>
			start := len("<think>")
			end := len(match) - len("</think>")
			if start < end && start < len(match) {
				content := match[start:end]
				return fmt.Sprintf("[yellow::i][thinking... (%d chars) (press Alt+T to expand)][-:-:-]", len(content))
			}
			return "[yellow::i][thinking... (press Alt+T to expand)][-:-:-]"
		})
		// Handle incomplete thinking blocks (during streaming when </think> hasn't arrived yet)
		if strings.Contains(text, "<think>") && !strings.Contains(text, "</think>") {
			// Find the incomplete thinking block and replace it
			startIdx := strings.Index(text, "<think>")
			if startIdx != -1 {
				content := text[startIdx+len("<think>"):]
				placeholder := fmt.Sprintf("[yellow::i][thinking... (%d chars) (press Alt+T to expand)][-:-:-]", len(content))
				text = text[:startIdx] + placeholder
			}
		}
	}
	return text
}

func addNewChat(chatName string) {
	id, err := store.ChatGetMaxID()
	if err != nil {
		logger.Error("failed to get max chat id from db;", "id:", id)
		// INFO: will rewrite first chat
	}
	chat := &models.Chat{
		ID:        id + 1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Agent:     cfg.AssistantRole,
	}
	if chatName == "" {
		chatName = fmt.Sprintf("%d_%s", chat.ID, cfg.AssistantRole)
	}
	chat.Name = chatName
	chatMap[chat.Name] = chat
	activeChatName = chat.Name
}

func applyCharCard(cc *models.CharCard, loadHistory bool) {
	cfg.AssistantRole = cc.Role
	history, err := loadAgentsLastChat(cfg.AssistantRole)
	if err != nil || !loadHistory {
		// too much action for err != nil; loadAgentsLastChat needs to be split up
		history = []models.RoleMsg{
			{Role: "system", Content: cc.SysPrompt},
			{Role: cfg.AssistantRole, Content: cc.FirstMsg},
		}
		logger.Warn("failed to load last agent chat;", "agent", cc.Role, "err", err, "new_history", history)
		addNewChat("")
	}
	chatBody.Messages = history
}

func charToStart(agentName string, keepSysP bool) bool {
	cc, ok := sysMap[agentName]
	if !ok {
		return false
	}
	applyCharCard(cc, keepSysP)
	return true
}

func updateModelLists() {
	var err error
	if cfg.OpenRouterToken != "" {
		ORFreeModels, err = fetchORModels(true)
		if err != nil {
			logger.Warn("failed to fetch or models", "error", err)
		}
	}
	// if llama.cpp started after gf-lt?
	localModelsMu.Lock()
	LocalModels, err = fetchLCPModels()
	localModelsMu.Unlock()
	if err != nil {
		logger.Warn("failed to fetch llama.cpp models", "error", err)
	}
}

func refreshLocalModelsIfEmpty() {
	localModelsMu.RLock()
	if len(LocalModels) > 0 {
		localModelsMu.RUnlock()
		return
	}
	localModelsMu.RUnlock()
	// try to fetch
	models, err := fetchLCPModels()
	if err != nil {
		logger.Warn("failed to fetch llama.cpp models", "error", err)
		return
	}
	localModelsMu.Lock()
	LocalModels = models
	localModelsMu.Unlock()
}

func summarizeAndStartNewChat() {
	if len(chatBody.Messages) == 0 {
		_ = notifyUser("info", "No chat history to summarize")
		return
	}
	_ = notifyUser("info", "Summarizing chat history...")
	// Call the summarize_chat tool via agent
	summaryBytes := callToolWithAgent("summarize_chat", map[string]string{})
	summary := string(summaryBytes)
	if summary == "" {
		_ = notifyUser("error", "Failed to generate summary")
		return
	}
	// Start a new chat
	startNewChat(true)
	// Inject summary as a tool call response
	toolMsg := models.RoleMsg{
		Role:       cfg.ToolRole,
		Content:    summary,
		ToolCallID: "",
	}
	chatBody.Messages = append(chatBody.Messages, toolMsg)
	// Update UI
	textView.SetText(chatToText(chatBody.Messages, cfg.ShowSys))
	colorText()
	// Update storage
	if err := updateStorageChat(activeChatName, chatBody.Messages); err != nil {
		logger.Warn("failed to update storage after injecting summary", "error", err)
	}
	_ = notifyUser("info", "Chat summarized and new chat started with summary as tool response")
}

func init() {
	// ctx, cancel := context.WithCancel(context.Background())
	var err error
	cfg, err = config.LoadConfig("config.toml")
	if err != nil {
		fmt.Println("failed to load config.toml", err)
		cancel()
		os.Exit(1)
		return
	}
	// Set image base directory for path display
	baseDir := cfg.FilePickerDir
	if baseDir == "" || baseDir == "." {
		// Resolve "." to current working directory
		if wd, err := os.Getwd(); err == nil {
			baseDir = wd
		}
	}
	models.SetImageBaseDir(baseDir)
	defaultStarter = []models.RoleMsg{
		{Role: "system", Content: basicSysMsg},
		{Role: cfg.AssistantRole, Content: defaultFirstMsg},
	}
	logfile, err := os.OpenFile(cfg.LogFile,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Error("failed to open log file", "error", err, "filename", cfg.LogFile)
		cancel()
		os.Exit(1)
		return
	}
	// load cards
	basicCard.Role = cfg.AssistantRole
	// toolCard.Role = cfg.AssistantRole
	//
	logLevel.Set(slog.LevelInfo)
	logger = slog.New(slog.NewTextHandler(logfile, &slog.HandlerOptions{Level: logLevel}))
	store = storage.NewProviderSQL(cfg.DBPATH, logger)
	if store == nil {
		cancel()
		os.Exit(1)
		return
	}
	ragger = rag.New(logger, store, cfg)
	// https://github.com/coreydaley/ggerganov-llama.cpp/blob/master/examples/server/README.md
	// load all chats in memory
	if _, err := loadHistoryChats(); err != nil {
		logger.Error("failed to load chat", "error", err)
		cancel()
		os.Exit(1)
		return
	}
	lastToolCall = &models.FuncCall{}
	lastChat := loadOldChatOrGetNew()
	chatBody = &models.ChatBody{
		Model:    "modelname",
		Stream:   true,
		Messages: lastChat,
	}
	choseChunkParser()
	httpClient = createClient(time.Second * 90)
	if cfg.TTS_ENABLED {
		orator = NewOrator(logger, cfg)
	}
	if cfg.STT_ENABLED {
		asr = NewSTT(logger, cfg)
	}
	// Initialize scrollToEndEnabled based on config
	scrollToEndEnabled = cfg.AutoScrollEnabled
	go updateModelLists()
	go chatWatcher(ctx)
}

func getValidKnowToRecipient(msg *models.RoleMsg) (string, bool) {
	if cfg == nil || !cfg.CharSpecificContextEnabled {
		return "", false
	}
	// case where all roles are in the tag => public message
	cr := listChatRoles()
	slices.Sort(cr)
	slices.Sort(msg.KnownTo)
	if slices.Equal(cr, msg.KnownTo) {
		logger.Info("got msg with tag mentioning every role")
		return "", false
	}
	// Check each character in the KnownTo list
	for _, recipient := range msg.KnownTo {
		if recipient == msg.Role || recipient == cfg.ToolRole {
			// weird cases, skip
			continue
		}
		// Skip if this is the user character (user handles their own turn)
		// If user is in KnownTo, stop processing - it's the user's turn
		if recipient == cfg.UserRole || recipient == cfg.WriteNextMsgAs {
			return "", false
		}
		return recipient, true
	}
	return "", false
}

// triggerPrivateMessageResponses checks if a message was sent privately to specific characters
// and triggers those non-user characters to respond
func triggerPrivateMessageResponses(msg *models.RoleMsg) {
	recipient, ok := getValidKnowToRecipient(msg)
	if !ok || recipient == "" {
		return
	}
	// Trigger the recipient character to respond
	triggerMsg := recipient + ":\n"
	// Send empty message so LLM continues naturally from the conversation
	crr := &models.ChatRoundReq{
		UserMsg: triggerMsg,
		Role:    recipient,
		Resume:  true,
	}
	fmt.Fprintf(textView, "\n[-:-:b](%d) ", len(chatBody.Messages))
	fmt.Fprint(textView, roleToIcon(recipient))
	fmt.Fprint(textView, "[-:-:-]\n")
	chatRoundChan <- crr
}
