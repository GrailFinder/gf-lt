package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gf-lt/config"
	"gf-lt/extra"
	"gf-lt/models"
	"gf-lt/rag"
	"gf-lt/storage"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/neurosnap/sentences/english"
	"github.com/rivo/tview"
)

var (
	httpClient  = &http.Client{}
	cluedoState *extra.CluedoRoundInfo // Current game state
	playerOrder []string               // Turn order tracking
	cfg         *config.Config
	logger      *slog.Logger
	logLevel    = new(slog.LevelVar)
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
	lastToolCallID      string // Store the ID of the most recent tool call
	//nolint:unused // TTS_ENABLED conditionally uses this
	orator          extra.Orator
	asr             extra.STT
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
)

// cleanNullMessages removes messages with null or empty content to prevent API issues
func cleanNullMessages(messages []models.RoleMsg) []models.RoleMsg {
	cleaned := make([]models.RoleMsg, 0, len(messages))
	for _, msg := range messages {
		// Include message if it has content or if it's a tool response (which might have tool_call_id)
		if msg.HasContent() || msg.ToolCallID != "" {
			cleaned = append(cleaned, msg)
		} else {
			// Log filtered messages for debugging
			logger.Warn("filtering out message during cleaning", "role", msg.Role, "content", msg.Content, "tool_call_id", msg.ToolCallID, "has_content", msg.HasContent())
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

func fetchLCPModelName() *models.LLMModels {
	//nolint
	resp, err := httpClient.Get(cfg.FetchModelNameAPI)
	if err != nil {
		logger.Warn("failed to get model", "link", cfg.FetchModelNameAPI, "error", err)
		return nil
	}
	defer resp.Body.Close()
	llmModel := models.LLMModels{}
	if err := json.NewDecoder(resp.Body).Decode(&llmModel); err != nil {
		logger.Warn("failed to decode resp", "link", cfg.FetchModelNameAPI, "error", err)
		return nil
	}
	if resp.StatusCode != 200 {
		chatBody.Model = "disconnected"
		return nil
	}
	chatBody.Model = path.Base(llmModel.Data[0].ID)
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

func sendMsgToLLM(body io.Reader) {
	choseChunkParser()

	var req *http.Request
	var err error

	// Capture and log the request body for debugging
	if _, ok := body.(*io.LimitedReader); ok {
		// If it's a LimitedReader, we need to handle it differently
		logger.Debug("request body type is LimitedReader", "parser", chunkParser, "link", cfg.CurrentAPI)
		req, err = http.NewRequest("POST", cfg.CurrentAPI, body)
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
	} else {
		// For other reader types, capture and log the body content
		bodyBytes, err := io.ReadAll(body)
		if err != nil {
			logger.Error("failed to read request body for logging", "error", err)
			// Create request with original body if reading fails
			req, err = http.NewRequest("POST", cfg.CurrentAPI, bytes.NewReader(bodyBytes))
			if err != nil {
				logger.Error("newreq error", "error", err)
				if err := notifyUser("error", "apicall failed:"+err.Error()); err != nil {
					logger.Error("failed to notify", "error", err)
				}
				streamDone <- true
				return
			}
		} else {
			// Log the request body for debugging
			logger.Debug("sending request to API", "api", cfg.CurrentAPI, "body", string(bodyBytes))
			// Create request with the captured body
			req, err = http.NewRequest("POST", cfg.CurrentAPI, bytes.NewReader(bodyBytes))
			if err != nil {
				logger.Error("newreq error", "error", err)
				if err := notifyUser("error", "apicall failed:"+err.Error()); err != nil {
					logger.Error("failed to notify", "error", err)
				}
				streamDone <- true
				return
			}
		}

		req.Header.Add("Accept", "application/json")
		req.Header.Add("Content-Type", "application/json")
		req.Header.Add("Authorization", "Bearer "+chunkParser.GetToken())
		req.Header.Set("Accept-Encoding", "gzip")
	}
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
			lastToolCallID = chunk.ToolID
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
	tokenizer, err := english.NewSentenceTokenizer(nil)
	if err != nil {
		return "", err
	}
	// this where llm should find the questions in text and ask them
	questionsS := tokenizer.Tokenize(qText)
	questions := make([]string, len(questionsS))
	for i, q := range questionsS {
		questions[i] = q.Text
	}
	respVecs := []models.VectorRow{}
	for i, q := range questions {
		emb, err := ragger.LineToVector(q)
		if err != nil {
			logger.Error("failed to get embs", "error", err, "index", i, "question", q)
			continue
		}

		// Create EmbeddingResp struct for the search
		embeddingResp := &models.EmbeddingResp{
			Embedding: emb,
			Index:     0, // Not used in search but required for the struct
		}

		vecs, err := ragger.SearchEmb(embeddingResp)
		if err != nil {
			logger.Error("failed to query embs", "error", err, "index", i, "question", q)
			continue
		}
		respVecs = append(respVecs, vecs...)
	}
	// get raw text
	resps := []string{}
	logger.Debug("rag query resp", "vecs len", len(respVecs))
	for _, rv := range respVecs {
		resps = append(resps, rv.RawText)
	}
	if len(resps) == 0 {
		return "No related results from RAG vector storage.", nil
	}
	return strings.Join(resps, "\n"), nil
}

func roleToIcon(role string) string {
	return "<" + role + ">: "
}

// FIXME: it should not be here; move to extra
func checkGame(role string, tv *tview.TextView) {
	// Handle Cluedo game flow
	// should go before form msg, since formmsg takes chatBody and makes ioreader out of it
	// role is almost always user, unless it's regen or resume
	// cannot get in this block, since cluedoState is nil;
	if cfg.EnableCluedo {
		// Initialize Cluedo game if needed
		if cluedoState == nil {
			playerOrder = []string{cfg.UserRole, cfg.AssistantRole, cfg.CluedoRole2}
			cluedoState = extra.CluedoPrepCards(playerOrder)
		}
		// notifyUser("got in cluedo", "yay")
		currentPlayer := playerOrder[0]
		playerOrder = append(playerOrder[1:], currentPlayer) // Rotate turns
		if role == cfg.UserRole {
			fmt.Fprintf(tv, "Your (%s) cards: %s\n", currentPlayer, cluedoState.GetPlayerCards(currentPlayer))
		} else {
			chatBody.Messages = append(chatBody.Messages, models.RoleMsg{
				Role:    cfg.ToolRole,
				Content: cluedoState.GetPlayerCards(currentPlayer),
			})
		}
	}
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
	if !resume {
		checkGame(role, tv)
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
		fmt.Fprintf(tv, "[-:-:b](%d) ", len(chatBody.Messages))
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
			tv.ScrollToEnd()
			// Send chunk to audio stream handler
			if cfg.TTS_ENABLED {
				// audioStream.TextChan <- chunk
				extra.TTSTextChan <- chunk
			}
		case toolChunk := <-openAIToolChan:
			fmt.Fprint(tv, toolChunk)
			toolResp.WriteString(toolChunk)
			tv.ScrollToEnd()
		case <-streamDone:
			botRespMode = false
			if cfg.TTS_ENABLED {
				// audioStream.TextChan <- chunk
				extra.TTSFlushChan <- true
				logger.Debug("sending flushchan signal")
			}
			break out
		}
	}
	botRespMode = false
	// numbers in chatbody and displayed must be the same
	if resume {
		chatBody.Messages[len(chatBody.Messages)-1].Content += respText.String()
		// lastM.Content = lastM.Content + respText.String()
	} else {
		chatBody.Messages = append(chatBody.Messages, models.RoleMsg{
			Role: botPersona, Content: respText.String(),
		})
	}

	logger.Debug("chatRound: before cleanChatBody", "messages_before_clean", len(chatBody.Messages))
	for i, msg := range chatBody.Messages {
		logger.Debug("chatRound: before cleaning", "index", i, "role", msg.Role, "content_len", len(msg.Content), "has_content", msg.HasContent(), "tool_call_id", msg.ToolCallID)
	}

	// Clean null/empty messages to prevent API issues with endpoints like llama.cpp jinja template
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
	if chatBody != nil && chatBody.Messages != nil {
		originalLen := len(chatBody.Messages)
		logger.Debug("cleanChatBody: before cleaning", "message_count", originalLen)
		for i, msg := range chatBody.Messages {
			logger.Debug("cleanChatBody: before clean", "index", i, "role", msg.Role, "content_len", len(msg.Content), "has_content", msg.HasContent(), "tool_call_id", msg.ToolCallID)
		}

		chatBody.Messages = cleanNullMessages(chatBody.Messages)

		logger.Debug("cleanChatBody: after cleaning", "original_len", originalLen, "new_len", len(chatBody.Messages))
		for i, msg := range chatBody.Messages {
			logger.Debug("cleanChatBody: after clean", "index", i, "role", msg.Role, "content_len", len(msg.Content), "has_content", msg.HasContent(), "tool_call_id", msg.ToolCallID)
		}
	}
}

func findCall(msg, toolCall string, tv *tview.TextView) {
	fc := &models.FuncCall{}
	if toolCall != "" {
		// HTML-decode the tool call string to handle encoded characters like &lt; -> <=
		decodedToolCall := html.UnescapeString(toolCall)
		openAIToolMap := make(map[string]string)
		// respect tool call
		if err := json.Unmarshal([]byte(decodedToolCall), &openAIToolMap); err != nil {
			logger.Error("failed to unmarshal openai tool call", "call", decodedToolCall, "error", err)
			// Send error response to LLM so it can retry or handle the error
			toolResponseMsg := models.RoleMsg{
				Role:       cfg.ToolRole,
				Content:    fmt.Sprintf("Error processing tool call: %v. Please check the JSON format and try again.", err),
				ToolCallID: lastToolCallID, // Use the stored tool call ID
			}
			chatBody.Messages = append(chatBody.Messages, toolResponseMsg)
			// Clear the stored tool call ID after using it
			lastToolCallID = ""
			// Trigger the assistant to continue processing with the error message
			chatRound("", cfg.AssistantRole, tv, false, false)
			return
		}
		lastToolCall.Args = openAIToolMap
		fc = lastToolCall
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
		if err := json.Unmarshal([]byte(decodedJsStr), &fc); err != nil {
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
	}
	// call a func
	f, ok := fnMap[fc.Name]
	if !ok {
		m := fc.Name + " is not implemented"
		// Create tool response message with the proper tool_call_id
		toolResponseMsg := models.RoleMsg{
			Role:       cfg.ToolRole,
			Content:    m,
			ToolCallID: lastToolCallID, // Use the stored tool call ID
		}
		chatBody.Messages = append(chatBody.Messages, toolResponseMsg)
		logger.Debug("findCall: added tool not implemented response", "role", toolResponseMsg.Role, "content_len", len(toolResponseMsg.Content), "tool_call_id", toolResponseMsg.ToolCallID, "message_count_after_add", len(chatBody.Messages))
		// Clear the stored tool call ID after using it
		lastToolCallID = ""

		// Trigger the assistant to continue processing with the new tool response
		// by calling chatRound with empty content to continue the assistant's response
		chatRound("", cfg.AssistantRole, tv, false, false)
		return
	}
	resp := f(fc.Args)
	toolMsg := string(resp) // Remove the "tool response: " prefix and %+v formatting
	logger.Info("llm used tool call", "tool_resp", toolMsg, "tool_attrs", fc)
	fmt.Fprintf(tv, "%s[-:-:b](%d) <%s>: [-:-:-]\n%s\n",
		"\n", len(chatBody.Messages), cfg.ToolRole, toolMsg)
	// Create tool response message with the proper tool_call_id
	toolResponseMsg := models.RoleMsg{
		Role:       cfg.ToolRole,
		Content:    toolMsg,
		ToolCallID: lastToolCallID, // Use the stored tool call ID
	}
	chatBody.Messages = append(chatBody.Messages, toolResponseMsg)
	logger.Debug("findCall: added actual tool response", "role", toolResponseMsg.Role, "content_len", len(toolResponseMsg.Content), "tool_call_id", toolResponseMsg.ToolCallID, "message_count_after_add", len(chatBody.Messages))
	// Clear the stored tool call ID after using it
	lastToolCallID = ""
	// Trigger the assistant to continue processing with the new tool response
	// by calling chatRound with empty content to continue the assistant's response
	chatRound("", cfg.AssistantRole, tv, false, false)
}

func chatToTextSlice(showSys bool) []string {
	resp := make([]string, len(chatBody.Messages))
	for i, msg := range chatBody.Messages {
		// INFO: skips system msg and tool msg
		if !showSys && (msg.Role == cfg.ToolRole || msg.Role == "system") {
			continue
		}
		resp[i] = msg.ToText(i)
	}
	return resp
}

func chatToText(showSys bool) string {
	s := chatToTextSlice(showSys)
	return strings.Join(s, "")
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
	// Initialize Cluedo if enabled and matching role
	if cfg.EnableCluedo && cc.Role == "CluedoPlayer" {
		playerOrder = []string{cfg.UserRole, cfg.AssistantRole, cfg.CluedoRole2}
		cluedoState = extra.CluedoPrepCards(playerOrder)
	}
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
	// Initialize Cluedo if enabled and matching role
	if cfg.EnableCluedo && cfg.AssistantRole == "CluedoPlayer" {
		playerOrder = []string{cfg.UserRole, cfg.AssistantRole, cfg.CluedoRole2}
		cluedoState = extra.CluedoPrepCards(playerOrder)
	}
	if cfg.OpenRouterToken != "" {
		go func() {
			ORModels, err := fetchORModels(true)
			if err != nil {
				logger.Error("failed to fetch or models", "error", err)
			} else {
				ORFreeModels = ORModels
			}
		}()
	}
	choseChunkParser()
	httpClient = createClient(time.Second * 15)
	if cfg.TTS_ENABLED {
		orator = extra.NewOrator(logger, cfg)
	}
	if cfg.STT_ENABLED {
		asr = extra.NewSTT(logger, cfg)
	}
}
