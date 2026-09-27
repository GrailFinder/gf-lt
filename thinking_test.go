package main

import (
	"testing"

	"gf-lt/models"
)

// stripThinkingFromMsg must not mutate its input: the same message objects live
// in chatBody and are persisted to storage, so stripping in place used to erase
// thinking from chat history on the first API call.
func TestStripThinkingDoesNotMutate(t *testing.T) {
	old := cfg.StripThinkingFromAPI
	cfg.StripThinkingFromAPI = true
	defer func() { cfg.StripThinkingFromAPI = old }()

	body := []models.RoleMsg{
		{Role: "assistant", Content: "<think>reasoning here</think>\n\nthe answer"},
	}
	original := body[0].Content

	// filterMessagesForCurrentCharacter returns the same slice when char specific
	// context is off, so this mirrors what llm.go does.
	filtered, _ := filterMessagesForCurrentCharacter(body)
	stripped := stripThinkingFromMsg(&filtered[0])

	if got := stripped.GetText(); got != "the answer" {
		t.Errorf("stripped text = %q, want %q", got, "the answer")
	}
	if body[0].Content != original {
		t.Errorf("original mutated: got %q, want %q", body[0].Content, original)
	}
}

func TestStripThinkingKeepsUserAndToolMessages(t *testing.T) {
	old := cfg.StripThinkingFromAPI
	cfg.StripThinkingFromAPI = true
	defer func() { cfg.StripThinkingFromAPI = old }()

	for _, role := range []string{cfg.UserRole, cfg.ToolRole, "system"} {
		msg := models.RoleMsg{Role: role, Content: "<think>example</think>\nhi"}
		got := stripThinkingFromMsg(&msg)
		if got.GetText() != msg.Content {
			t.Errorf("role %q was stripped: %q", role, got.GetText())
		}
	}
}

func TestStripThinkingRespectsConfig(t *testing.T) {
	old := cfg.StripThinkingFromAPI
	cfg.StripThinkingFromAPI = false
	defer func() { cfg.StripThinkingFromAPI = old }()

	msg := models.RoleMsg{Role: "assistant", Content: "<think>kept</think>\nanswer"}
	if got := stripThinkingFromMsg(&msg).GetText(); got != msg.Content {
		t.Errorf("thinking stripped while disabled: %q", got)
	}
}

// multimodal messages hold their text in ContentParts; stripping must not
// disturb the images or alias the original parts.
func TestStripThinkingMultimodal(t *testing.T) {
	old := cfg.StripThinkingFromAPI
	cfg.StripThinkingFromAPI = true
	defer func() { cfg.StripThinkingFromAPI = old }()

	msg := models.NewMultimodalMsg("assistant", []any{
		models.TextContentPart{Type: "text", Text: "<think>t</think>\nhello"},
		models.ImageContentPart{Type: "image_url"},
	})
	stripped := stripThinkingFromMsg(&msg)

	if got := stripped.GetText(); got != "hello" {
		t.Errorf("stripped text = %q, want %q", got, "hello")
	}
	if got := msg.GetText(); got != "<think>t</think>\nhello" {
		t.Errorf("original text mutated: %q", got)
	}
	if len(stripped.ContentParts) != 2 {
		t.Fatalf("image part lost: %d parts", len(stripped.ContentParts))
	}
}
