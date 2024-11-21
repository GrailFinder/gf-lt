package main

import (
	"bufio"
	"bytes"
	"elefant/models"
	"elefant/storage"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rivo/tview"
)

var httpClient = http.Client{
	Timeout: time.Second * 20,
}

var (
	logger        *slog.Logger
	APIURL        = "http://localhost:8080/v1/chat/completions"
	DB            = map[string]map[string]any{}
	userRole      = "user"
	assistantRole = "assistant"
	toolRole      = "tool"
	assistantIcon = "<🤖>: "
	userIcon      = "<user>: "
	historyDir    = "./history/"
	// TODO: pass as an cli arg
	showSystemMsgs  bool
	chunkLimit      = 1000
	activeChatName  string
	chunkChan       = make(chan string, 10)
	streamDone      = make(chan bool, 1)
	chatBody        *models.ChatBody
	store           storage.FullRepo
	defaultFirstMsg = "Hello! What can I do for you?"
	defaultStarter  = []models.MessagesStory{
		{Role: "system", Content: systemMsg},
		{Role: assistantRole, Content: defaultFirstMsg},
	}
	interruptResp = false
)

// ====

func getUserInput(userPrompt string) string {
	fmt.Printf(userPrompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		panic(err) // think about it
	}
	return line
}

func formMsg(chatBody *models.ChatBody, newMsg, role string) io.Reader {
	if newMsg != "" { // otherwise let the bot continue
		newMsg := models.MessagesStory{Role: role, Content: newMsg}
		chatBody.Messages = append(chatBody.Messages, newMsg)
	}
	data, err := json.Marshal(chatBody)
	if err != nil {
		panic(err)
	}
	return bytes.NewReader(data)
}

// func sendMsgToLLM(body io.Reader) (*models.LLMRespChunk, error) {
func sendMsgToLLM(body io.Reader) (any, error) {
	resp, err := httpClient.Post(APIURL, "application/json", body)
	if err != nil {
		logger.Error("llamacpp api", "error", err)
		return nil, err
	}
	defer resp.Body.Close()
	llmResp := []models.LLMRespChunk{}
	// chunkChan <- assistantIcon
	reader := bufio.NewReader(resp.Body)
	counter := 0
	for {
		if interruptResp {
			interruptResp = false
			logger.Info("interrupted bot response")
			break
		}
		llmchunk := models.LLMRespChunk{}
		if counter > chunkLimit {
			logger.Warn("response hit chunk limit", "limit", chunkLimit)
			streamDone <- true
			break
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			streamDone <- true
			logger.Error("error reading response body", "error", err)
		}
		// logger.Info("linecheck", "line", string(line), "len", len(line), "counter", counter)
		if len(line) <= 1 {
			continue // skip \n
		}
		// starts with -> data:
		line = line[6:]
		if err := json.Unmarshal(line, &llmchunk); err != nil {
			logger.Error("failed to decode", "error", err, "line", string(line))
			streamDone <- true
			return nil, err
		}
		llmResp = append(llmResp, llmchunk)
		// logger.Info("streamview", "chunk", llmchunk)
		// if llmchunk.Choices[len(llmchunk.Choices)-1].FinishReason != "chat.completion.chunk" {
		if llmchunk.Choices[len(llmchunk.Choices)-1].FinishReason == "stop" {
			streamDone <- true
			// last chunk
			break
		}
		counter++
		// bot sends way too many \n
		answerText := strings.ReplaceAll(llmchunk.Choices[0].Delta.Content, "\n\n", "\n")
		chunkChan <- answerText
	}
	return llmResp, nil
}

func chatRound(userMsg, role string, tv *tview.TextView) {
	botRespMode = true
	reader := formMsg(chatBody, userMsg, role)
	go sendMsgToLLM(reader)
	fmt.Fprintf(tv, fmt.Sprintf("(%d) ", len(chatBody.Messages)))
	fmt.Fprintf(tv, assistantIcon)
	respText := strings.Builder{}
out:
	for {
		select {
		case chunk := <-chunkChan:
			// fmt.Printf(chunk)
			fmt.Fprintf(tv, chunk)
			respText.WriteString(chunk)
			tv.ScrollToEnd()
		case <-streamDone:
			break out
		}
	}
	botRespMode = false
	chatBody.Messages = append(chatBody.Messages, models.MessagesStory{
		Role: assistantRole, Content: respText.String(),
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
	prefix := "__tool_call__\n"
	suffix := "\n__tool_call__"
	fc := models.FuncCall{}
	if !strings.HasPrefix(msg, prefix) ||
		!strings.HasSuffix(msg, suffix) {
		return
	}
	jsStr := strings.TrimSuffix(strings.TrimPrefix(msg, prefix), suffix)
	if err := json.Unmarshal([]byte(jsStr), &fc); err != nil {
		logger.Error("failed to unmarshal tool call", "error", err)
		return
		// panic(err)
	}
	// call a func
	f, ok := fnMap[fc.Name]
	if !ok {
		m := fmt.Sprintf("%s is not implemented", fc.Name)
		chatRound(m, toolRole, tv)
		return
	}
	resp := f(fc.Args)
	toolMsg := fmt.Sprintf("tool response: %+v", resp)
	// reader := formMsg(chatBody, toolMsg, toolRole)
	// sendMsgToLLM()
	chatRound(toolMsg, toolRole, tv)
	// return func result to the llm
}

func chatToTextSlice(showSys bool) []string {
	resp := make([]string, len(chatBody.Messages))
	for i, msg := range chatBody.Messages {
		if !showSys && (msg.Role != assistantRole && msg.Role != userRole) {
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

func textToMsg(rawMsg string) models.MessagesStory {
	msg := models.MessagesStory{}
	// system and tool?
	if strings.HasPrefix(rawMsg, assistantIcon) {
		msg.Role = assistantRole
		msg.Content = strings.TrimPrefix(rawMsg, assistantIcon)
		return msg
	}
	if strings.HasPrefix(rawMsg, userIcon) {
		msg.Role = userRole
		msg.Content = strings.TrimPrefix(rawMsg, userIcon)
		return msg
	}
	return msg
}

func textSliceToChat(chat []string) []models.MessagesStory {
	resp := make([]models.MessagesStory, len(chat))
	for i, rawMsg := range chat {
		msg := textToMsg(rawMsg)
		resp[i] = msg
	}
	return resp
}

func init() {
	file, err := os.OpenFile("log.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	// create dir if does not exist
	if err := os.MkdirAll(historyDir, os.ModePerm); err != nil {
		panic(err)
	}
	logger = slog.New(slog.NewTextHandler(file, nil))
	store = storage.NewProviderSQL("test.db", logger)
	// https://github.com/coreydaley/ggerganov-llama.cpp/blob/master/examples/server/README.md
	// load all chats in memory
	loadHistoryChats()
	lastChat := loadOldChatOrGetNew()
	logger.Info("loaded history", "chat", lastChat)
	chatBody = &models.ChatBody{
		Model:    "modl_name",
		Stream:   true,
		Messages: lastChat,
	}
}
