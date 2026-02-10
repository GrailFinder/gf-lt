package main

import (
	"fmt"
	"gf-lt/config"
	"gf-lt/models"
	"strings"
	"testing"
)

func TestRemoveThinking(t *testing.T) {
	cases := []struct {
		cb       *models.ChatBody
		toolMsgs uint8
	}{
		{cb: &models.ChatBody{
			Stream: true,
			Messages: []models.RoleMsg{
				{Role: "tool", Content: "should be ommited"},
				{Role: "system", Content: "should stay"},
				{Role: "user", Content: "hello, how are you?"},
				{Role: "assistant", Content: "Oh, hi. <think>I should thank user and continue the conversation</think> I am geat, thank you! How are you?"},
			},
		},
			toolMsgs: uint8(1),
		},
	}
	for i, tc := range cases {
					t.Run(fmt.Sprintf("run_%d", i), func(t *testing.T) {
						cfg = &config.Config{ToolRole: "tool"} // Initialize cfg.ToolRole for test
						mNum := len(tc.cb.Messages)
						removeThinking(tc.cb)
						if len(tc.cb.Messages) != mNum-int(tc.toolMsgs) {
							t.Errorf("failed to delete tools msg %v; expected %d, got %d", tc.cb.Messages, mNum-int(tc.toolMsgs), len(tc.cb.Messages))
						}
						for _, msg := range tc.cb.Messages {
							if strings.Contains(msg.Content, "<think>") {
								t.Errorf("msg contains think tag; msg: %s\n", msg.Content)
							}
						}
					})	}
}
