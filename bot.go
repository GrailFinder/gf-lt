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
	httpClient          = &http.Client{}
	cluedoState         *extra.CluedoRoundInfo // Current game state
	playerOrder         []string               // Turn order tracking
	cfg                 *config.Config
	logger              *slog.Logger
	logLevel            = new(slog.LevelVar)
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

func fetchModelName() *models.LLMModels {
	// TODO: to config
	api := "http://localhost:8080/v1/models"
	//nolint
	resp, err := httpClient.Get(api)
	if err != nil {
		logger.Warn("failed to get model", "link", api, "error", err)
		return nil
	}
	defer resp.Body.Close()
	llmModel := models.LLMModels{}
	if err := json.NewDecoder(resp.Body).Decode(&llmModel); err != nil {
		logger.Warn("failed to decode resp", "link", api, "error", err)
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
	bodyBytes, _ := io.ReadAll(body)
	ok := json.Valid(bodyBytes)
	if !ok {
		panic("invalid json")
	}
	// nolint
	req, err := http.NewRequest("POST", cfg.CurrentAPI, bytes.NewReader(bodyBytes))
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
	// req.Header.Set("Content-Length", strconv.Itoa(len(bodyBytes)))
	req.Header.Set("Accept-Encoding", "gzip")
	// nolint
	// resp, err := httpClient.Post(cfg.CurrentAPI, "application/json", body)
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
			stop       bool
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
			streamDone <- true
			break
		}
		// Handle error messages in response content
		if string(line) != "" && strings.Contains(strings.ToLower(string(line)), "error") {
			logger.Error("API error response detected", "line", line, "url", cfg.CurrentAPI)
			streamDone <- true
			break
		}
		if stop {
			if chunk.Chunk != "" {
				logger.Warn("text inside of finish llmchunk", "chunk", chunk, "counter", counter)
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
	// TODO: this where llm should find the questions in text and ask them
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
		vecs, err := store.SearchClosest(emb)
		if err != nil {
			logger.Error("failed to query embs", "error", err, "index", i, "question", q)
			continue
		}
		respVecs = append(respVecs, vecs...)
	}
	// get raw text
	resps := []string{}
	logger.Debug("sqlvec resp", "vecs len", len(respVecs))
	for _, rv := range respVecs {
		resps = append(resps, rv.RawText)
	}
	if len(resps) == 0 {
		return "No related results from vector storage.", nil
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
		fmt.Fprint(tv, roleToIcon(cfg.AssistantRole))
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
				logger.Info("sending flushchan signal")
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
			Role: cfg.AssistantRole, Content: respText.String(),
		})
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

func findCall(msg, toolCall string, tv *tview.TextView) {
	fc := &models.FuncCall{}
	if toolCall != "" {
		openAIToolMap := make(map[string]string)
		// respect tool call
		if err := json.Unmarshal([]byte(toolCall), &openAIToolMap); err != nil {
			logger.Error("failed to unmarshal openai tool call", "call", toolCall, "error", err)
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
		if err := json.Unmarshal([]byte(jsStr), &fc); err != nil {
			logger.Error("failed to unmarshal tool call", "error", err, "json_string", jsStr)
			return
		}
	}
	// call a func
	f, ok := fnMap[fc.Name]
	if !ok {
		m := fc.Name + " is not implemented"
		chatRound(m, cfg.ToolRole, tv, false, false)
		return
	}
	resp := f(fc.Args)
	toolMsg := fmt.Sprintf("tool response: %+v", string(resp))
	fmt.Fprintf(tv, "%s[-:-:b](%d) <%s>: [-:-:-]\n%s\n",
		"\n", len(chatBody.Messages), cfg.ToolRole, toolMsg)
	chatRound(toolMsg, cfg.ToolRole, tv, false, false)
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
		// TODO: too much action for err != nil; loadAgentsLastChat needs to be split up
		logger.Warn("failed to load last agent chat;", "agent", cc.Role, "err", err)
		history = []models.RoleMsg{
			{Role: "system", Content: cc.SysPrompt},
			{Role: cfg.AssistantRole, Content: cc.FirstMsg},
		}
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
		chat.Name = fmt.Sprintf("%d_%s", chat.ID, cfg.AssistantRole)
		chatMap[chat.Name] = chat
		activeChatName = chat.Name
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
	cfg = config.LoadConfigOrDefault("config.toml")
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
	toolCard.Role = cfg.AssistantRole
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
		asr = extra.NewWhisperSTT(logger, cfg.STT_URL, 16000)
	}
}
