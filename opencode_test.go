package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"gf-lt/models"
)

func TestIsOpenCodeGoAPI(t *testing.T) {
	savedCfg := *cfg
	defer func() { *cfg = savedCfg }()
	cfg.OpenCodeGoChatAPI = "https://opencode.ai/inference/go/openai/v1/chat/completions"

	cases := []struct {
		api  string
		want bool
	}{
		{"https://opencode.ai/inference/go/openai/v1/chat/completions", true},
		{"https://opencode.ai/zen/go/v1/chat/completions", true},
		{"https://opencode.ai/inference/go/anthropic/v1/messages", true},
		{"https://openrouter.ai/api/v1/chat/completions", false},
		{"https://api.deepseek.com/chat/completions", false},
		{"http://localhost:8080/v1/chat/completions", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isOpenCodeGoAPI(tc.api); got != tc.want {
			t.Errorf("isOpenCodeGoAPI(%q) = %v, want %v", tc.api, got, tc.want)
		}
	}

	// Custom endpoints configured via OpenCodeGoChatAPI must be recognised too.
	cfg.OpenCodeGoChatAPI = "https://proxy.example/v1/chat/completions"
	if !isOpenCodeGoAPI(cfg.OpenCodeGoChatAPI) {
		t.Error("custom OpenCodeGoChatAPI was not recognised")
	}
}

func TestOpenCodeGoSessionID(t *testing.T) {
	savedCfg := *cfg
	savedChat := activeChatName
	defer func() {
		*cfg = savedCfg
		activeChatName = savedChat
	}()

	cfg.OpenCodeGoSession = ""
	activeChatName = "chat-a"
	first := openCodeGoSessionID()
	if first == "" {
		t.Fatal("session id is empty")
	}
	if again := openCodeGoSessionID(); again != first {
		t.Errorf("session id not stable: %q != %q", again, first)
	}

	activeChatName = "chat-b"
	if other := openCodeGoSessionID(); other == first {
		t.Errorf("session id did not change with chat: %q", other)
	}

	cfg.OpenCodeGoSession = "fixed-session"
	if got := openCodeGoSessionID(); got != "fixed-session" {
		t.Errorf("configured session not used: %q", got)
	}
}

func TestOpenCodeGoChatGetHeaders(t *testing.T) {
	savedCfg := *cfg
	defer func() { *cfg = savedCfg }()
	cfg.OpenCodeGoSession = "sess-1"

	headers := OpenCodeGoChat{}.GetHeaders()
	if headers["x-opencode-session"] != "sess-1" {
		t.Errorf("x-opencode-session = %q, want sess-1", headers["x-opencode-session"])
	}
	if headers["User-Agent"] == "" {
		t.Error("User-Agent is empty")
	}
}

func TestOpenCodeGoChatParseChunk(t *testing.T) {
	chunk := []byte(`{"choices":[{"index":0,"delta":{"content":"hi","reasoning_content":"why"},"finish_reason":""}]}`)
	got, err := OpenCodeGoChat{}.ParseChunk(chunk)
	if err != nil {
		t.Fatalf("ParseChunk error: %v", err)
	}
	if got.Chunk != "hi" {
		t.Errorf("chunk = %q, want hi", got.Chunk)
	}
	if got.Reasoning != "why" {
		t.Errorf("reasoning = %q, want why", got.Reasoning)
	}
	if got.Finished {
		t.Error("chunk marked finished too early")
	}

	finish := []byte(`{"choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}]}`)
	got, err = OpenCodeGoChat{}.ParseChunk(finish)
	if err != nil {
		t.Fatalf("ParseChunk(finish) error: %v", err)
	}
	if !got.Finished {
		t.Error("finish_reason=stop did not mark the chunk finished")
	}
}

func TestOpenCodeGoChatParseToolCall(t *testing.T) {
	chunk := []byte(`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"file_read","arguments":"{\"path\":\"a\"}"}}]},"finish_reason":""}]}`)
	got, err := OpenCodeGoChat{}.ParseChunk(chunk)
	if err != nil {
		t.Fatalf("ParseChunk error: %v", err)
	}
	if !got.ToolResp {
		t.Error("tool call chunk not flagged as ToolResp")
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Function.Name != "file_read" {
		t.Errorf("unexpected tool calls: %+v", got.ToolCalls)
	}
}

func TestOpenCodeGoChatFormMsg(t *testing.T) {
	savedCfg := *cfg
	savedBody := chatBody
	defer func() {
		*cfg = savedCfg
		chatBody = savedBody
	}()

	cfg.UserRole = "user"
	cfg.AssistantRole = "assistant"
	cfg.ToolRole = "tool"
	cfg.ToolUse = false
	cfg.CharSpecificContextEnabled = false
	cfg.DisableRoll = true
	cfg.CurrentAPI = "https://opencode.ai/inference/go/openai/v1/chat/completions"
	chatBody = &models.ChatBody{Model: "glm-5.3-flash", Stream: true}

	r, err := OpenCodeGoChat{}.FormMsg("hello", cfg.UserRole, false)
	if err != nil {
		t.Fatalf("FormMsg error: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var req struct {
		Model    string           `json:"model"`
		Stream   bool             `json:"stream"`
		Messages []models.RoleMsg `json:"messages"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		t.Fatalf("request is not valid JSON: %v\n%s", err, data)
	}
	if req.Model != "glm-5.3-flash" {
		t.Errorf("model = %q, want glm-5.3-flash", req.Model)
	}
	if !req.Stream {
		t.Error("stream = false, want true")
	}
	if len(req.Messages) == 0 {
		t.Fatal("no messages in request")
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != "user" || !strings.Contains(last.GetText(), "hello") {
		t.Errorf("unexpected last message: %+v", last)
	}
}
