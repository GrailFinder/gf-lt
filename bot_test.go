package main

import (
	"gf-lt/config"
	"gf-lt/models"
	"reflect"
	"testing"
)

func TestConsolidateConsecutiveAssistantMessages(t *testing.T) {
	// Mock config for testing
	testCfg := &config.Config{
		AssistantRole:                 "assistant",
		WriteNextMsgAsCompletionAgent: "",
	}
	cfg = testCfg

	tests := []struct {
		name     string
		input    []models.RoleMsg
		expected []models.RoleMsg
	}{
		{
			name: "no consecutive assistant messages",
			input: []models.RoleMsg{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
				{Role: "user", Content: "How are you?"},
			},
			expected: []models.RoleMsg{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
				{Role: "user", Content: "How are you?"},
			},
		},
		{
			name: "consecutive assistant messages should be consolidated",
			input: []models.RoleMsg{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "First part"},
				{Role: "assistant", Content: "Second part"},
				{Role: "user", Content: "Thanks"},
			},
			expected: []models.RoleMsg{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "First part\nSecond part"},
				{Role: "user", Content: "Thanks"},
			},
		},
		{
			name: "multiple sets of consecutive assistant messages",
			input: []models.RoleMsg{
				{Role: "user", Content: "First question"},
				{Role: "assistant", Content: "First answer part 1"},
				{Role: "assistant", Content: "First answer part 2"},
				{Role: "user", Content: "Second question"},
				{Role: "assistant", Content: "Second answer part 1"},
				{Role: "assistant", Content: "Second answer part 2"},
				{Role: "assistant", Content: "Second answer part 3"},
			},
			expected: []models.RoleMsg{
				{Role: "user", Content: "First question"},
				{Role: "assistant", Content: "First answer part 1\nFirst answer part 2"},
				{Role: "user", Content: "Second question"},
				{Role: "assistant", Content: "Second answer part 1\nSecond answer part 2\nSecond answer part 3"},
			},
		},
		{
			name: "single assistant message (no consolidation needed)",
			input: []models.RoleMsg{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
			},
			expected: []models.RoleMsg{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there"},
			},
		},
		{
			name: "only assistant messages",
			input: []models.RoleMsg{
				{Role: "assistant", Content: "First"},
				{Role: "assistant", Content: "Second"},
				{Role: "assistant", Content: "Third"},
			},
			expected: []models.RoleMsg{
				{Role: "assistant", Content: "First\nSecond\nThird"},
			},
		},
		{
			name: "user messages at the end are preserved",
			input: []models.RoleMsg{
				{Role: "assistant", Content: "First"},
				{Role: "assistant", Content: "Second"},
				{Role: "user", Content: "Final user message"},
			},
			expected: []models.RoleMsg{
				{Role: "assistant", Content: "First\nSecond"},
				{Role: "user", Content: "Final user message"},
			},
		},
		{
			name: "tool call ids preserved in consolidation",
			input: []models.RoleMsg{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "First part", ToolCallID: "call_123"},
				{Role: "assistant", Content: "Second part", ToolCallID: "call_123"}, // Same ID
				{Role: "user", Content: "Thanks"},
			},
			expected: []models.RoleMsg{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "First part\nSecond part", ToolCallID: "call_123"},
				{Role: "user", Content: "Thanks"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := consolidateConsecutiveAssistantMessages(tt.input)
			
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d messages, got %d", len(tt.expected), len(result))
				t.Logf("Result: %+v", result)
				t.Logf("Expected: %+v", tt.expected)
				return
			}
			
			for i, expectedMsg := range tt.expected {
				if i >= len(result) {
					t.Errorf("Result has fewer messages than expected at index %d", i)
					continue
				}
				
				actualMsg := result[i]
				if actualMsg.Role != expectedMsg.Role {
					t.Errorf("Message %d: expected role '%s', got '%s'", i, expectedMsg.Role, actualMsg.Role)
				}
				
				if actualMsg.Content != expectedMsg.Content {
					t.Errorf("Message %d: expected content '%s', got '%s'", i, expectedMsg.Content, actualMsg.Content)
				}
				
				if actualMsg.ToolCallID != expectedMsg.ToolCallID {
					t.Errorf("Message %d: expected ToolCallID '%s', got '%s'", i, expectedMsg.ToolCallID, actualMsg.ToolCallID)
				}
			}
			
			// Additional check: ensure no messages were lost
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Result does not match expected:\nResult:   %+v\nExpected: %+v", result, tt.expected)
			}
		})
	}
}