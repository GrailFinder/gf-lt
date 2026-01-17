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
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/neurosnap/sentences/english"
	"github.com/rivo/tview"
)

var (
	httpClient = &http.Client{}
	cfg        *config.Config
	logger     *slog.Logger
	logLevel   = new(slog.LevelVar)
)
var (
	activeChatName      string
	chunkChan           = make(chan string, 10)
	openAIToolChan      = make(chan string, 10)
	streamDone          = make(chan bool, 1)
	chatBody            *models.ChatBody
	store               storage.FullRepo
	defaultFirstMsg     = "Hello! What can I do for you?"
	defaultStarter      = []models.RoleMsg{}
	defaultStarterBytes = []byte{}
	interruptResp       = false
	ragger              *rag.RAG
	chunkParser         ChunkParser
	lastToolCall        *models.FuncCall
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
		tag = "__known_to_chars__"
	}
	// Pattern: tag + list + "__"
	pattern := regexp.QuoteMeta(tag) + `(.*?)__`
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
		parts := strings.Split(list, ",")
		for _, p := range parts {
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
func processMessageTag(msg models.RoleMsg) models.RoleMsg {
	if cfg == nil || !cfg.CharSpecificContextEnabled {
		return msg
	}
	// If KnownTo already set, assume tag already processed (content cleaned).
	// However, we still check for new tags (maybe added later).
	knownTo := parseKnownToTag(msg.Content)
	logger.Info("processing tags", "msg", msg.Content, "known_to", knownTo)
	// If tag found, replace KnownTo with new list (merge with existing?)
	// For simplicity, if knownTo is not nil, replace.
	if knownTo != nil {
		msg.KnownTo = knownTo
		// Only ensure sender role is in KnownTo if there was a tag
		// This means the message is intended for specific characters
		if msg.Role != "" {
			senderAdded := false
			for _, k := range msg.KnownTo {
				if k == msg.Role {
					senderAdded = true
					break
				}
			}
			if !senderAdded {
				msg.KnownTo = append(msg.KnownTo, msg.Role)
			}
		}
	}
	return msg
}

// filterMessagesForCharacter returns messages visible to the specified character.
// If CharSpecificContextEnabled is false, returns all messages.
func filterMessagesForCharacter(messages []models.RoleMsg, character string) []models.RoleMsg {
	if cfg == nil || !cfg.CharSpecificContextEnabled || character == "" {
		return messages
	}
	filtered := make([]models.RoleMsg, 0, len(messages))
	for i, msg := range messages {
		logger.Info("filtering messages", "character", character, "index", i, "known_to", msg.KnownTo)
		// If KnownTo is nil or empty, message is visible to all
		// system msg cannot be filtered
		if len(msg.KnownTo) == 0 || msg.Role == "system" {
			filtered = append(filtered, msg)
			continue
		}
		// Check if character is in KnownTo list
		found := false
		for _, k := range msg.KnownTo {
			if k == character {
				found = true
				break
			}
		}
		if found {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

// cleanNullMessages removes messages with null or empty content to prevent API issues
func cleanNullMessages(messages []models.RoleMsg) []models.RoleMsg {
	// // deletes tool calls which we don't want for now
	// cleaned := make([]models.RoleMsg, 0, len(messages))
	// for _, msg := range messages {
	// 	// is there a sense for this check at all?
	// 	if msg.HasContent() || msg.ToolCallID != "" || msg.Role == cfg.AssistantRole || msg.Role == cfg.WriteNextMsgAsCompletionAgent {
	// 		cleaned = append(cleaned, msg)
	// 	} else {
	// 		// Log filtered messages for debugging
	// 		logger.Warn("filtering out message during cleaning", "role", msg.Role, "content", msg.Content, "tool_call_id", msg.ToolCallID, "has_content", msg.HasContent())
	// 	}
	// }
	return consolidateConsecutiveAssistantMessages(messages)
}

func cleanToolCalls(messages []models.RoleMsg) []models.RoleMsg {
	// If AutoCleanToolCallsFromCtx is false, keep tool call messages in context
	if cfg != nil && !cfg.AutoCleanToolCallsFromCtx {
		return consolidateConsecutiveAssistantMessages(messages)
	}
	cleaned := make([]models.RoleMsg, 0, len(messages))
	for i, msg := range messages {
		// recognize the message as the tool call and remove it
		// tool call in last msg should stay
		if msg.ToolCallID == "" || i == len(messages)-1 {
			cleaned = append(cleaned, msg)
		}
	}
	return consolidateConsecutiveAssistantMessages(cleaned)
}

// consolidateConsecutiveAssistantMessages merges consecutive assistant messages into a single message
func consolidateConsecutiveAssistantMessages(messages []models.RoleMsg) []models.RoleMsg {
	if len(messages) == 0 {
		return messages
	}
	consolidated := make([]models.RoleMsg, 0, len(messages))
	currentAssistantMsg := models.RoleMsg{}
	isBuildingAssistantMsg := false
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == cfg.AssistantRole || msg.Role == cfg.WriteNextMsgAsCompletionAgent {
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
		if strings.HasSuffix(cfg.CurrentAPI, "/completion") {
			// Old completion endpoint
			req := models.NewLCPReq(".", chatBody.Model, nil, map[string]float32{
				"temperature":    0.8,
				"dry_multiplier": 0.0,
				"min_p":          0.05,
				"n_predict":      0,
			}, []string{})
			req.Stream = false
			data, err = json.Marshal(req)
		} else if strings.Contains(cfg.CurrentAPI, "/v1/chat/completions") {
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
		} else {
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

func fetchLCPModelName() *models.LCPModels {
	//nolint
	resp, err := httpClient.Get(cfg.FetchModelNameAPI)
	if err != nil {
		chatBody.Model = "disconnected"
		logger.Warn("failed to get model", "link", cfg.FetchModelNameAPI, "error", err)
		if err := notifyUser("error", "request failed "+cfg.FetchModelNameAPI); err != nil {
			logger.Debug("failed to notify user", "error", err, "fn", "fetchLCPModelName")
		}
		return nil
	}
	defer resp.Body.Close()
	llmModel := models.LCPModels{}
	if err := json.NewDecoder(resp.Body).Decode(&llmModel); err != nil {
		logger.Warn("failed to decode resp", "link", cfg.FetchModelNameAPI, "error", err)
		return nil
	}
	if resp.StatusCode != 200 {
		chatBody.Model = "disconnected"
		return nil
	}
	chatBody.Model = path.Base(llmModel.Data[0].ID)
	cfg.CurrentModel = chatBody.Model
	return &llmModel
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
					return
				}
			}
		}
	}()
}

// sendMsgToLLM expects streaming resp
func sendMsgToLLM(body io.Reader) {
	choseChunkParser()
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
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	counter := uint32(0)
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
			logger.Error("error reading response body", "error", err, "line", string(line),
				"user_role", cfg.UserRole, "parser", chunkParser, "link", cfg.CurrentAPI)
			// if err.Error() != "EOF" {
			if err := notifyUser("API error", err.Error()); err != nil {
				logger.Error("failed to notify", "error", err)
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
			if chunk.Chunk != "" {
				logger.Warn("text inside of finish llmchunk", "chunk", chunk, "counter", counter)
				answerText = strings.ReplaceAll(chunk.Chunk, "\n\n", "\n")
				chunkChan <- answerText
			}
			streamDone <- true
			break
		}
		if counter == 0 {
			chunk.Chunk = strings.TrimPrefix(chunk.Chunk, " ")
		}
		// bot sends way too many \n
		answerText = strings.ReplaceAll(chunk.Chunk, "\n\n", "\n")
		chunkChan <- answerText
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

func chatRagUse(qText string) (string, error) {
	logger.Debug("Starting RAG query", "original_query", qText)
	tokenizer, err := english.NewSentenceTokenizer(nil)
	if err != nil {
		logger.Error("failed to create sentence tokenizer", "error", err)
		return "", err
	}
	// this where llm should find the questions in text and ask them
	questionsS := tokenizer.Tokenize(qText)
	questions := make([]string, len(questionsS))
	for i, q := range questionsS {
		questions[i] = q.Text
		logger.Debug("RAG question extracted", "index", i, "question", q.Text)
	}

	if len(questions) == 0 {
		logger.Warn("No questions extracted from query text", "query", qText)
		return "No related results from RAG vector storage.", nil
	}

	respVecs := []models.VectorRow{}
	for i, q := range questions {
		logger.Debug("Processing RAG question", "index", i, "question", q)
		emb, err := ragger.LineToVector(q)
		if err != nil {
			logger.Error("failed to get embeddings for RAG", "error", err, "index", i, "question", q)
			continue
		}
		logger.Debug("Got embeddings for question", "index", i, "question_len", len(q), "embedding_len", len(emb))

		// Create EmbeddingResp struct for the search
		embeddingResp := &models.EmbeddingResp{
			Embedding: emb,
			Index:     0, // Not used in search but required for the struct
		}
		vecs, err := ragger.SearchEmb(embeddingResp)
		if err != nil {
			logger.Error("failed to query embeddings in RAG", "error", err, "index", i, "question", q)
			continue
		}
		logger.Debug("RAG search returned vectors", "index", i, "question", q, "vector_count", len(vecs))
		respVecs = append(respVecs, vecs...)
	}

	// get raw text
	resps := []string{}
	logger.Debug("RAG query final results", "total_vecs_found", len(respVecs))
	for _, rv := range respVecs {
		resps = append(resps, rv.RawText)
		logger.Debug("RAG result", "slug", rv.Slug, "filename", rv.FileName, "raw_text_len", len(rv.RawText))
	}

	if len(resps) == 0 {
		logger.Info("No RAG results found for query", "original_query", qText, "question_count", len(questions))
		return "No related results from RAG vector storage.", nil
	}

	result := strings.Join(resps, "\n")
	logger.Debug("RAG query completed", "result_len", len(result), "response_count", len(resps))
	return result, nil
}

func roleToIcon(role string) string {
	return "<" + role + ">: "
}

func chatRound(userMsg, role string, tv *tview.TextView, regen, resume bool) {
	botRespMode = true
	botPersona := cfg.AssistantRole
	if cfg.WriteNextMsgAsCompletionAgent != "" {
		botPersona = cfg.WriteNextMsgAsCompletionAgent
	}
	defer func() { botRespMode = false }()
	// check that there is a model set to use if is not local
	if cfg.CurrentAPI == cfg.DeepSeekChatAPI || cfg.CurrentAPI == cfg.DeepSeekCompletionAPI {
		if chatBody.Model != "deepseek-chat" && chatBody.Model != "deepseek-reasoner" {
			if err := notifyUser("bad request", "wrong deepseek model name"); err != nil {
				logger.Warn("failed ot notify user", "error", err)
				return
			}
			return
		}
	}
	choseChunkParser()
	reader, err := chunkParser.FormMsg(userMsg, role, resume)
	if reader == nil || err != nil {
		logger.Error("empty reader from msgs", "role", role, "error", err)
		return
	}
	if cfg.SkipLLMResp {
		return
	}
	go sendMsgToLLM(reader)
	logger.Debug("looking at vars in chatRound", "msg", userMsg, "regen", regen, "resume", resume)
	if !resume {
		fmt.Fprintf(tv, "\n[-:-:b](%d) ", len(chatBody.Messages))
		fmt.Fprint(tv, roleToIcon(botPersona))
		fmt.Fprint(tv, "[-:-:-]\n")
		if cfg.ThinkUse && !strings.Contains(cfg.CurrentAPI, "v1") {
			// fmt.Fprint(tv, "<think>")
			chunkChan <- "<think>"
		}
	}
	respText := strings.Builder{}
	toolResp := strings.Builder{}
out:
	for {
		select {
		case chunk := <-chunkChan:
			fmt.Fprint(tv, chunk)
			respText.WriteString(chunk)
			if scrollToEndEnabled {
				tv.ScrollToEnd()
			}
			// Send chunk to audio stream handler
			if cfg.TTS_ENABLED {
				TTSTextChan <- chunk
			}
		case toolChunk := <-openAIToolChan:
			fmt.Fprint(tv, toolChunk)
			toolResp.WriteString(toolChunk)
			if scrollToEndEnabled {
				tv.ScrollToEnd()
			}
		case <-streamDone:
			// drain any remaining chunks from chunkChan before exiting
			for len(chunkChan) > 0 {
				chunk := <-chunkChan
				fmt.Fprint(tv, chunk)
				respText.WriteString(chunk)
				if scrollToEndEnabled {
					tv.ScrollToEnd()
				}
				if cfg.TTS_ENABLED {
					// Send chunk to audio stream handler
					TTSTextChan <- chunk
				}
			}
			if cfg.TTS_ENABLED {
				// msg is done; flush it down
				TTSFlushChan <- true
			}
			break out
		}
	}
	botRespMode = false
	// numbers in chatbody and displayed must be the same
	if resume {
		chatBody.Messages[len(chatBody.Messages)-1].Content += respText.String()
		// lastM.Content = lastM.Content + respText.String()
		// Process the updated message to check for known_to tags in resumed response
		updatedMsg := chatBody.Messages[len(chatBody.Messages)-1]
		processedMsg := processMessageTag(updatedMsg)
		chatBody.Messages[len(chatBody.Messages)-1] = processedMsg
	} else {
		newMsg := models.RoleMsg{
			Role: botPersona, Content: respText.String(),
		}
		// Process the new message to check for known_to tags in LLM response
		newMsg = processMessageTag(newMsg)
		chatBody.Messages = append(chatBody.Messages, newMsg)
	}
	logger.Debug("chatRound: before cleanChatBody", "messages_before_clean", len(chatBody.Messages))
	for i, msg := range chatBody.Messages {
		logger.Debug("chatRound: before cleaning", "index", i, "role", msg.Role, "content_len", len(msg.Content), "has_content", msg.HasContent(), "tool_call_id", msg.ToolCallID)
	}
	// // Clean null/empty messages to prevent API issues with endpoints like llama.cpp jinja template
	cleanChatBody()
	logger.Debug("chatRound: after cleanChatBody", "messages_after_clean", len(chatBody.Messages))
	for i, msg := range chatBody.Messages {
		logger.Debug("chatRound: after cleaning", "index", i, "role", msg.Role, "content_len", len(msg.Content), "has_content", msg.HasContent(), "tool_call_id", msg.ToolCallID)
	}
	colorText()
	updateStatusLine()
	// bot msg is done;
	// now check it for func call
	// logChat(activeChatName, chatBody.Messages)
	if err := updateStorageChat(activeChatName, chatBody.Messages); err != nil {
		logger.Warn("failed to update storage", "error", err, "name", activeChatName)
	}
	findCall(respText.String(), toolResp.String(), tv)
}

// cleanChatBody removes messages with null or empty content to prevent API issues
func cleanChatBody() {
	if chatBody == nil || chatBody.Messages == nil {
		return
	}
	originalLen := len(chatBody.Messages)
	logger.Debug("cleanChatBody: before cleaning", "message_count", originalLen)
	for i, msg := range chatBody.Messages {
		logger.Debug("cleanChatBody: before clean", "index", i, "role", msg.Role, "content_len", len(msg.Content), "has_content", msg.HasContent(), "tool_call_id", msg.ToolCallID)
	}
	// Tool request cleaning is now configurable via AutoCleanToolCallsFromCtx (default false)
	// /completion msg where part meant for user and other part tool call
	chatBody.Messages = cleanToolCalls(chatBody.Messages)
	chatBody.Messages = cleanNullMessages(chatBody.Messages)
	logger.Debug("cleanChatBody: after cleaning", "original_len", originalLen, "new_len", len(chatBody.Messages))
	for i, msg := range chatBody.Messages {
		logger.Debug("cleanChatBody: after clean", "index", i, "role", msg.Role, "content_len", len(msg.Content), "has_content", msg.HasContent(), "tool_call_id", msg.ToolCallID)
	}
}

// convertJSONToMapStringString unmarshals JSON into map[string]interface{} and converts all values to strings.
func convertJSONToMapStringString(jsonStr string) (map[string]string, error) {
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

func findCall(msg, toolCall string, tv *tview.TextView) {
	fc := &models.FuncCall{}
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
			chatRound("", cfg.AssistantRole, tv, false, false)
			return
		}
		lastToolCall.Args = openAIToolMap
		fc = lastToolCall
		// Set lastToolCall.ID from parsed tool call ID if available
		if len(openAIToolMap) > 0 {
			if id, exists := openAIToolMap["id"]; exists {
				lastToolCall.ID = id
			}
		}
	} else {
		jsStr := toolCallRE.FindString(msg)
		if jsStr == "" {
			return
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
			chatRound("", cfg.AssistantRole, tv, false, false)
			return
		}
		// Update lastToolCall with parsed function call
		lastToolCall.ID = fc.ID
		lastToolCall.Name = fc.Name
		lastToolCall.Args = fc.Args
	}
	// we got here => last msg recognized as a tool call (correct or not)
	// make sure it has ToolCallID
	if chatBody.Messages[len(chatBody.Messages)-1].ToolCallID == "" {
		// Tool call IDs should be alphanumeric strings with length 9!
		chatBody.Messages[len(chatBody.Messages)-1].ToolCallID = randString(9)
	}
	// Ensure lastToolCall.ID is set, fallback to assistant message's ToolCallID
	if lastToolCall.ID == "" {
		lastToolCall.ID = chatBody.Messages[len(chatBody.Messages)-1].ToolCallID
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
		chatRound("", cfg.AssistantRole, tv, false, false)
		return
	}
	resp := callToolWithAgent(fc.Name, fc.Args)
	toolMsg := string(resp) // Remove the "tool response: " prefix and %+v formatting
	logger.Info("llm used tool call", "tool_resp", toolMsg, "tool_attrs", fc)
	fmt.Fprintf(tv, "%s[-:-:b](%d) <%s>: [-:-:-]\n%s\n",
		"\n\n", len(chatBody.Messages), cfg.ToolRole, toolMsg)
	// Create tool response message with the proper tool_call_id
	toolResponseMsg := models.RoleMsg{
		Role:       cfg.ToolRole,
		Content:    toolMsg,
		ToolCallID: lastToolCall.ID, // Use the stored tool call ID
	}
	chatBody.Messages = append(chatBody.Messages, toolResponseMsg)
	logger.Debug("findCall: added actual tool response", "role", toolResponseMsg.Role, "content_len", len(toolResponseMsg.Content), "tool_call_id", toolResponseMsg.ToolCallID, "message_count_after_add", len(chatBody.Messages))
	// Clear the stored tool call ID after using it
	lastToolCall.ID = ""
	// Trigger the assistant to continue processing with the new tool response
	// by calling chatRound with empty content to continue the assistant's response
	chatRound("", cfg.AssistantRole, tv, false, false)
}

func chatToTextSlice(messages []models.RoleMsg, showSys bool) []string {
	resp := make([]string, len(messages))
	for i, msg := range messages {
		// INFO: skips system msg and tool msg
		if !showSys && (msg.Role == cfg.ToolRole || msg.Role == "system") {
			continue
		}
		resp[i] = msg.ToText(i)
	}
	return resp
}

func chatToText(messages []models.RoleMsg, showSys bool) string {
	s := chatToTextSlice(messages, showSys)
	return strings.Join(s, "\n")
}

func removeThinking(chatBody *models.ChatBody) {
	msgs := []models.RoleMsg{}
	for _, msg := range chatBody.Messages {
		// Filter out tool messages and thinking markers
		if msg.Role == cfg.ToolRole {
			continue
		}
		// find thinking and remove it
		rm := models.RoleMsg{
			Role:    msg.Role,
			Content: thinkRE.ReplaceAllString(msg.Content, ""),
		}
		msgs = append(msgs, rm)
	}
	chatBody.Messages = msgs
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

func applyCharCard(cc *models.CharCard) {
	cfg.AssistantRole = cc.Role
	// FIXME: remove
	history, err := loadAgentsLastChat(cfg.AssistantRole)
	if err != nil {
		// too much action for err != nil; loadAgentsLastChat needs to be split up
		logger.Warn("failed to load last agent chat;", "agent", cc.Role, "err", err)
		history = []models.RoleMsg{
			{Role: "system", Content: cc.SysPrompt},
			{Role: cfg.AssistantRole, Content: cc.FirstMsg},
		}
		addNewChat("")
	}
	chatBody.Messages = history
}

func charToStart(agentName string) bool {
	cc, ok := sysMap[agentName]
	if !ok {
		return false
	}
	applyCharCard(cc)
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
	startNewChat()
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
	var err error
	cfg, err = config.LoadConfig("config.toml")
	if err != nil {
		fmt.Println("failed to load config.toml")
		os.Exit(1)
		return
	}
	defaultStarter = []models.RoleMsg{
		{Role: "system", Content: basicSysMsg},
		{Role: cfg.AssistantRole, Content: defaultFirstMsg},
	}
	logfile, err := os.OpenFile(cfg.LogFile,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		slog.Error("failed to open log file", "error", err, "filename", cfg.LogFile)
		return
	}
	defaultStarterBytes, err = json.Marshal(defaultStarter)
	if err != nil {
		slog.Error("failed to marshal defaultStarter", "error", err)
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
		os.Exit(1)
	}
	ragger = rag.New(logger, store, cfg)
	// https://github.com/coreydaley/ggerganov-llama.cpp/blob/master/examples/server/README.md
	// load all chats in memory
	if _, err := loadHistoryChats(); err != nil {
		logger.Error("failed to load chat", "error", err)
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
}
