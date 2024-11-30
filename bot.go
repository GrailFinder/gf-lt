package main

import (
	"bufio"
	"bytes"
	"elefant/config"
	"elefant/models"
	"elefant/storage"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

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
	defaultStarter      = []models.MessagesStory{}
	defaultStarterBytes = []byte{}
	interruptResp       = false
)

// ====

func formMsg(chatBody *models.ChatBody, newMsg, role string) io.Reader {
	if newMsg != "" { // otherwise let the bot continue
		newMsg := models.MessagesStory{Role: role, Content: newMsg}
		chatBody.Messages = append(chatBody.Messages, newMsg)
	}
	data, err := json.Marshal(chatBody)
	if err != nil {
		logger.Error("failed to form a msg", "error", err)
		return nil
	}
	return bytes.NewReader(data)
}

// func sendMsgToLLM(body io.Reader) (*models.LLMRespChunk, error) {
func sendMsgToLLM(body io.Reader) {
	// nolint
	resp, err := httpClient.Post(cfg.APIURL, "application/json", body)
	if err != nil {
		logger.Error("llamacpp api", "error", err)
		return
	}
	defer resp.Body.Close()
	// llmResp := []models.LLMRespChunk{}
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
		llmchunk := models.LLMRespChunk{}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			logger.Error("error reading response body", "error", err)
			continue
		}
		if len(line) <= 1 {
			continue // skip \n
		}
		// starts with -> data:
		line = line[6:]
		if err := json.Unmarshal(line, &llmchunk); err != nil {
			logger.Error("failed to decode", "error", err, "line", string(line))
			streamDone <- true
			return
		}
		// llmResp = append(llmResp, llmchunk)
		// logger.Info("streamview", "chunk", llmchunk)
		// if llmchunk.Choices[len(llmchunk.Choices)-1].FinishReason != "chat.completion.chunk" {
		if llmchunk.Choices[len(llmchunk.Choices)-1].FinishReason == "stop" {
			streamDone <- true
			// last chunk
			break
		}
		// bot sends way too many \n
		answerText := strings.ReplaceAll(llmchunk.Choices[0].Delta.Content, "\n\n", "\n")
		chunkChan <- answerText
	}
}

func chatRound(userMsg, role string, tv *tview.TextView) {
	botRespMode = true
	reader := formMsg(chatBody, userMsg, role)
	if reader == nil {
		logger.Error("empty reader from msgs", "role", role)
		return
	}
	go sendMsgToLLM(reader)
	if userMsg != "" { // no need to write assistant icon since we continue old message
		fmt.Fprintf(tv, "(%d) ", len(chatBody.Messages))
		fmt.Fprint(tv, cfg.AssistantIcon)
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
			break out
		}
	}
	botRespMode = false
	chatBody.Messages = append(chatBody.Messages, models.MessagesStory{
		Role: cfg.AssistantRole, Content: respText.String(),
	})
	// bot msg is done;
	// now check it for func call
	// logChat(activeChatName, chatBody.Messages)
	err := updateStorageChat(activeChatName, chatBody.Messages)
	if err != nil {
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
		chatRound(m, cfg.ToolRole, tv)
		return
	}
	resp := f(fc.Args...)
	toolMsg := fmt.Sprintf("tool response: %+v", string(resp))
	chatRound(toolMsg, cfg.ToolRole, tv)
}

func chatToTextSlice(showSys bool) []string {
	resp := make([]string, len(chatBody.Messages))
	for i, msg := range chatBody.Messages {
		if !showSys && (msg.Role != cfg.AssistantRole && msg.Role != cfg.UserRole) {
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

// func textToMsg(rawMsg string) models.MessagesStory {
// 	msg := models.MessagesStory{}
// 	// system and tool?
// 	if strings.HasPrefix(rawMsg, cfg.AssistantIcon) {
// 		msg.Role = cfg.AssistantRole
// 		msg.Content = strings.TrimPrefix(rawMsg, cfg.AssistantIcon)
// 		return msg
// 	}
// 	if strings.HasPrefix(rawMsg, cfg.UserIcon) {
// 		msg.Role = cfg.UserRole
// 		msg.Content = strings.TrimPrefix(rawMsg, cfg.UserIcon)
// 		return msg
// 	}
// 	return msg
// }

// func textSliceToChat(chat []string) []models.MessagesStory {
// 	resp := make([]models.MessagesStory, len(chat))
// 	for i, rawMsg := range chat {
// 		msg := textToMsg(rawMsg)
// 		resp[i] = msg
// 	}
// 	return resp
// }

func init() {
	cfg = config.LoadConfigOrDefault("config.example.toml")
	defaultStarter = []models.MessagesStory{
		{Role: "system", Content: systemMsg},
		{Role: cfg.AssistantRole, Content: defaultFirstMsg},
	}
	file, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Error("failed to open log file", "error", err, "filename", cfg.LogFile)
		return
	}
	defaultStarterBytes, err = json.Marshal(defaultStarter)
	if err != nil {
		logger.Error("failed to marshal defaultStarter", "error", err)
		return
	}
	logger = slog.New(slog.NewTextHandler(file, nil))
	store = storage.NewProviderSQL("test.db", logger)
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
}
