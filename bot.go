package main

import (
	"bufio"
	"elefant/config"
	"elefant/models"
	"elefant/rag"
	"elefant/storage"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/neurosnap/sentences/english"
	"github.com/rivo/tview"
)

var httpClient = http.Client{}

var (
	cfg                 *config.Config
	logger              *slog.Logger
	activeChatName      string
	chunkChan           = make(chan string, 10)
	streamDone          = make(chan bool, 1)
	chatBody            *models.ChatBody
	store               storage.FullRepo
	defaultFirstMsg     = "Hello! What can I do for you?"
	defaultStarter      = []models.RoleMsg{}
	defaultStarterBytes = []byte{}
	interruptResp       = false
	ragger              *rag.RAG
	currentModel        = "none"
	chunkParser         ChunkParser
	defaultLCPProps     = map[string]float32{
		"temperature":    0.8,
		"dry_multiplier": 0.6,
	}
)

func fetchModelName() {
	api := "http://localhost:8080/v1/models"
	resp, err := httpClient.Get(api)
	if err != nil {
		logger.Warn("failed to get model", "link", api, "error", err)
		return
	}
	defer resp.Body.Close()
	llmModel := models.LLMModels{}
	if err := json.NewDecoder(resp.Body).Decode(&llmModel); err != nil {
		logger.Warn("failed to decode resp", "link", api, "error", err)
		return
	}
	if resp.StatusCode != 200 {
		currentModel = "none"
		return
	}
	currentModel = path.Base(llmModel.Data[0].ID)
	updateStatusLine()
}

// func fetchProps() {
// 	api := "http://localhost:8080/props"
// 	resp, err := httpClient.Get(api)
// 	if err != nil {
// 		logger.Warn("failed to get model", "link", api, "error", err)
// 		return
// 	}
// 	defer resp.Body.Close()
// 	llmModel := models.LLMModels{}
// 	if err := json.NewDecoder(resp.Body).Decode(&llmModel); err != nil {
// 		logger.Warn("failed to decode resp", "link", api, "error", err)
// 		return
// 	}
// 	if resp.StatusCode != 200 {
// 		currentModel = "none"
// 		return
// 	}
// 	currentModel = path.Base(llmModel.Data[0].ID)
// 	updateStatusLine()
// }

// func sendMsgToLLM(body io.Reader) (*models.LLMRespChunk, error) {
func sendMsgToLLM(body io.Reader) {
	// nolint
	resp, err := httpClient.Post(cfg.APIURL, "application/json", body)
	if err != nil {
		logger.Error("llamacpp api", "error", err)
		streamDone <- true
		return
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	counter := uint32(0)
	for {
		counter++
		if interruptResp {
			interruptResp = false
			logger.Info("interrupted bot response")
			break
		}
		if cfg.ChunkLimit > 0 && counter > cfg.ChunkLimit {
			logger.Warn("response hit chunk limit", "limit", cfg.ChunkLimit)
			streamDone <- true
			break
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			logger.Error("error reading response body", "error", err, "line", string(line))
			if err.Error() != "EOF" {
				streamDone <- true
				break
			}
			continue
		}
		if len(line) <= 1 {
			continue // skip \n
		}
		// starts with -> data:
		line = line[6:]
		content, stop, err := chunkParser.ParseChunk(line)
		if err != nil {
			logger.Error("error parsing response body", "error", err, "line", string(line), "url", cfg.APIURL)
			streamDone <- true
			break
		}
		if stop {
			if content != "" {
				logger.Warn("text inside of finish llmchunk", "chunk", content, "counter", counter)
			}
			streamDone <- true
			break
		}
		if counter == 0 {
			content = strings.TrimPrefix(content, " ")
		}
		// bot sends way too many \n
		answerText := strings.ReplaceAll(content, "\n\n", "\n")
		chunkChan <- answerText
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
	logger.Info("sqlvec resp", "vecs len", len(respVecs))
	for _, rv := range respVecs {
		resps = append(resps, rv.RawText)
	}
	if len(resps) == 0 {
		return "No related results from vector storage.", nil
	}
	return strings.Join(resps, "\n"), nil
}

func chatRound(userMsg, role string, tv *tview.TextView, regen bool) {
	botRespMode = true
	// reader := formMsg(chatBody, userMsg, role)
	reader, err := chunkParser.FormMsg(userMsg, role)
	if reader == nil || err != nil {
		logger.Error("empty reader from msgs", "role", role, "error", err)
		return
	}
	go sendMsgToLLM(reader)
	// if userMsg != "" && !regen { // no need to write assistant icon since we continue old message
	if userMsg != "" || regen {
		fmt.Fprintf(tv, "(%d) ", len(chatBody.Messages))
		fmt.Fprint(tv, cfg.AssistantIcon)
		fmt.Fprint(tv, "\n")
	}
	respText := strings.Builder{}
out:
	for {
		select {
		case chunk := <-chunkChan:
			// fmt.Printf(chunk)
			fmt.Fprint(tv, chunk)
			respText.WriteString(chunk)
			tv.ScrollToEnd()
		case <-streamDone:
			botRespMode = false
			break out
		}
	}
	botRespMode = false
	chatBody.Messages = append(chatBody.Messages, models.RoleMsg{
		Role: cfg.AssistantRole, Content: respText.String(),
	})
	colorText()
	updateStatusLine()
	// bot msg is done;
	// now check it for func call
	// logChat(activeChatName, chatBody.Messages)
	if err := updateStorageChat(activeChatName, chatBody.Messages); err != nil {
		logger.Warn("failed to update storage", "error", err, "name", activeChatName)
	}
	findCall(respText.String(), tv)
}

func findCall(msg string, tv *tview.TextView) {
	fc := models.FuncCall{}
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
	// call a func
	f, ok := fnMap[fc.Name]
	if !ok {
		m := fc.Name + "%s is not implemented"
		chatRound(m, cfg.ToolRole, tv, false)
		return
	}
	resp := f(fc.Args...)
	toolMsg := fmt.Sprintf("tool response: %+v", string(resp))
	chatRound(toolMsg, cfg.ToolRole, tv, false)
}

func chatToTextSlice(showSys bool) []string {
	resp := make([]string, len(chatBody.Messages))
	for i, msg := range chatBody.Messages {
		if !showSys && (msg.Role != cfg.AssistantRole && msg.Role != cfg.UserRole) {
			continue
		}
		resp[i] = msg.ToText(i, cfg)
	}
	return resp
}

func chatToText(showSys bool) string {
	s := chatToTextSlice(showSys)
	return strings.Join(s, "")
}

// func removeThinking() {
// 	s := chatToTextSlice(false) // will delete tools messages though
// 	chat := strings.Join(s, "")
// 	chat = thinkRE.ReplaceAllString(chat, "")
// 	reS := fmt.Sprintf("[%s:\n,%s:\n]", cfg.AssistantRole, cfg.UserRole)
// 	// no way to know what agent wrote which msg
// 	s = regexp.MustCompile(reS).Split(chat, -1)
// }

func textToMsgs(text string) []models.RoleMsg {
	lines := strings.Split(text, "\n")
	roleRE := regexp.MustCompile(`^\(\d+\) <.*>:`)
	resp := []models.RoleMsg{}
	oldrole := ""
	for _, line := range lines {
		if roleRE.MatchString(line) {
			// extract role
			role := ""
			// if role changes
			if role != oldrole {
				oldrole = role
				// newmsg
				msg := models.RoleMsg{
					Role: role,
				}
				resp = append(resp, msg)
			}
			resp[len(resp)-1].Content += "\n" + line
		}
	}
	if len(resp) != 0 {
		resp[0].Content = strings.TrimPrefix(resp[0].Content, "\n")
	}
	return resp
}

func applyCharCard(cc *models.CharCard) {
	cfg.AssistantRole = cc.Role
	// TODO: need map role->icon
	cfg.AssistantIcon = "<" + cc.Role + ">: "
	// try to load last active chat
	history, err := loadAgentsLastChat(cfg.AssistantRole)
	if err != nil {
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

func runModelNameTicker(n time.Duration) {
	ticker := time.NewTicker(n)
	for {
		fetchModelName()
		<-ticker.C
	}
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
		logger.Error("failed to open log file", "error", err, "filename", cfg.LogFile)
		return
	}
	defaultStarterBytes, err = json.Marshal(defaultStarter)
	if err != nil {
		logger.Error("failed to marshal defaultStarter", "error", err)
		return
	}
	// load cards
	basicCard.Role = cfg.AssistantRole
	toolCard.Role = cfg.AssistantRole
	//
	logger = slog.New(slog.NewTextHandler(logfile, nil))
	store = storage.NewProviderSQL("test.db", logger)
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
	lastChat := loadOldChatOrGetNew()
	logger.Info("loaded history")
	chatBody = &models.ChatBody{
		Model:    "modl_name",
		Stream:   true,
		Messages: lastChat,
	}
	initChunkParser()
	// go runModelNameTicker(time.Second * 120)
	// tempLoad()
}
