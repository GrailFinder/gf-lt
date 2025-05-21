package main

import (
	"gf-lt/models"
	"fmt"
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
			mNum := len(tc.cb.Messages)
			removeThinking(tc.cb)
			if len(tc.cb.Messages) != mNum-int(tc.toolMsgs) {
				t.Error("failed to delete tools msg", tc.cb.Messages, cfg.ToolRole)
			}
			for _, msg := range tc.cb.Messages {
				if strings.Contains(msg.Content, "<think>") {
					t.Errorf("msg contains think tag; msg: %s\n", msg.Content)
				}
			}
		})
	}
}
