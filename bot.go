package main

import (
	"bufio"
	"bytes"
	"elefant/models"
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
	chunkChan     = make(chan string, 10)
	streamDone    = make(chan bool, 1)
	chatBody      *models.ChatBody
	systemMsg     = `You're a helpful assistant.
# Tools
You can do functions call if needed.
Your current tools:
<tools>
{
"name":"get_id",
"args": "username"
}
</tools>
To make a function call return a json object within __tool_call__ tags;
Example:
__tool_call__
{
"name":"get_id",
"args": "Adam"
}
__tool_call___
When making function call avoid typing anything else. 'tool' user will respond with the results of the call.
After that you are free to respond to the user.
`
)

// predifine funcs
func getUserDetails(id ...string) map[string]any {
	// db query
	// return DB[id[0]]
	return map[string]any{
		"username":   "fm11",
		"id":         24983,
		"reputation": 911,
		"balance":    214.73,
	}
}

type fnSig func(...string) map[string]any

var fnMap = map[string]fnSig{
	"get_id": getUserDetails,
}

// ====

func getUserInput(userPrompt string) string {
	// fmt.Printf("<🤖>: %s\n<user>:", botMsg)
	fmt.Printf(userPrompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		panic(err) // think about it
	}
	// fmt.Printf("read line: %s-\n", line)
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
	llmResp := []models.LLMRespChunk{}
	// chunkChan <- assistantIcon
	reader := bufio.NewReader(resp.Body)
	counter := 0
	for {
		llmchunk := models.LLMRespChunk{}
		if counter > 2000 {
			streamDone <- true
			break
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			streamDone <- true
			panic(err)
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
		logger.Info("streamview", "chunk", llmchunk)
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
	fmt.Fprintf(tv, assistantIcon)
	respText := strings.Builder{}
out:
	for {
		select {
		case chunk := <-chunkChan:
			// fmt.Printf(chunk)
			fmt.Fprintf(tv, chunk)
			respText.WriteString(chunk)
		case <-streamDone:
			break out
		}
	}
	botRespMode = false
	chatBody.Messages = append(chatBody.Messages, models.MessagesStory{
		Role: assistantRole, Content: respText.String(),
	})
	// TODO:
	// bot msg is done;
	// now check it for func call
	logChat("testlog", chatBody.Messages)
	findCall(respText.String(), tv)
}

func logChat(fname string, msgs []models.MessagesStory) {
	data, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		logger.Error("failed to marshal", "error", err)
	}
	if err := os.WriteFile(fname, data, 0666); err != nil {
		logger.Error("failed to write log", "error", err)
	}
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
func init() {
	file, err := os.OpenFile("log.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	logger = slog.New(slog.NewTextHandler(file, &slog.HandlerOptions{}))
	logger.Info("test msg")
	firstMsg := "Hello! What can I do for you?"
	// fm, err := fillTempl("chatml", chatml)
	// if err != nil {
	// 	panic(err)
	// }
	// https://github.com/coreydaley/ggerganov-llama.cpp/blob/master/examples/server/README.md
	chatBody = &models.ChatBody{
		Model:  "modl_name",
		Stream: true,
		Messages: []models.MessagesStory{
			{Role: "system", Content: systemMsg},
			{Role: assistantRole, Content: firstMsg},
		},
	}
	// fmt.Printf("<🤖>: Hello! How can I help?")
	// for {
	// 	chatLoop()
	// }
}
