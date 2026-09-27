package main

import (
	"gf-lt/models"
	"testing"
)

func TestSanitizeToolMessagesForAPI(t *testing.T) {
	msgs := []models.RoleMsg{
		{Role: "system", Content: "sys"},
		{Role: "assistant", Content: "let me look", ToolCalls: []models.ToolCall{
			{ID: "call_1", Type: "function", FuncCall: models.ToolCallFunction{Name: "ls"}},
		}},
		{Role: "tool", Content: "a\nb", ToolCallID: "call_1"},
		// shell / roll output: no id, must be demoted
		{Role: "tool", Content: "$ ls\n\nagent\nassets"},
		// id that no assistant announced: also demoted
		{Role: "tool", Content: "stale", ToolCallID: "call_gone"},
		{Role: "user", Content: "hi"},
	}
	out := sanitizeToolMessagesForAPI(msgs)
	if len(out) != len(msgs) {
		t.Fatalf("length changed: %d != %d", len(out), len(msgs))
	}
	if out[2].Role != "tool" || out[2].ToolCallID != "call_1" {
		t.Errorf("valid tool msg was altered: %+v", out[2])
	}
	if out[3].Role != "user" || out[3].ToolCallID != "" {
		t.Errorf("orphan tool msg not demoted: %+v", out[3])
	}
	if out[4].Role != "user" || out[4].ToolCallID != "" {
		t.Errorf("stale-id tool msg not demoted: %+v", out[4])
	}
}

func TestSanitizeGeneratesMissingToolCallIDs(t *testing.T) {
	msgs := []models.RoleMsg{
		{Role: "assistant", Content: "", ToolCalls: []models.ToolCall{
			{Type: "function", FuncCall: models.ToolCallFunction{Name: "ls"}},
		}},
		{Role: "tool", Content: "x"},
	}
	// the tool result has no id, so it cannot be paired -> demoted
	out := sanitizeToolMessagesForAPI(msgs)
	if out[0].ToolCalls[0].ID == "" {
		t.Fatal("expected generated tool call id")
	}
	if out[1].Role != "user" {
		t.Errorf("expected demotion, got %q", out[1].Role)
	}
}
