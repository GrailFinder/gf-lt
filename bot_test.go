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

func TestUnmarshalFuncCall(t *testing.T) {
	tests := []struct {
		name    string
		jsonStr string
		want    *models.FuncCall
		wantErr bool
	}{
		{
			name:    "simple websearch with numeric limit",
			jsonStr: `{"name": "websearch", "args": {"query": "current weather in London", "limit": 3}}`,
			want: &models.FuncCall{
				Name: "websearch",
				Args: map[string]string{"query": "current weather in London", "limit": "3"},
			},
			wantErr: false,
		},
		{
			name:    "string limit",
			jsonStr: `{"name": "websearch", "args": {"query": "test", "limit": "5"}}`,
			want: &models.FuncCall{
				Name: "websearch",
				Args: map[string]string{"query": "test", "limit": "5"},
			},
			wantErr: false,
		},
		{
			name:    "boolean arg",
			jsonStr: `{"name": "test", "args": {"flag": true}}`,
			want: &models.FuncCall{
				Name: "test",
				Args: map[string]string{"flag": "true"},
			},
			wantErr: false,
		},
		{
			name:    "null arg",
			jsonStr: `{"name": "test", "args": {"opt": null}}`,
			want: &models.FuncCall{
				Name: "test",
				Args: map[string]string{"opt": ""},
			},
			wantErr: false,
		},
		{
			name:    "float arg",
			jsonStr: `{"name": "test", "args": {"ratio": 0.5}}`,
			want: &models.FuncCall{
				Name: "test",
				Args: map[string]string{"ratio": "0.5"},
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			jsonStr: `{invalid}`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := unmarshalFuncCall(tt.jsonStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("unmarshalFuncCall() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.Name != tt.want.Name {
				t.Errorf("unmarshalFuncCall() name = %v, want %v", got.Name, tt.want.Name)
			}
			if len(got.Args) != len(tt.want.Args) {
				t.Errorf("unmarshalFuncCall() args length = %v, want %v", len(got.Args), len(tt.want.Args))
			}
			for k, v := range tt.want.Args {
				if got.Args[k] != v {
					t.Errorf("unmarshalFuncCall() args[%v] = %v, want %v", k, got.Args[k], v)
				}
			}
		})
	}
}

func TestConvertJSONToMapStringString(t *testing.T) {
	tests := []struct {
		name    string
		jsonStr string
		want    map[string]string
		wantErr bool
	}{
		{
			name:    "simple map",
			jsonStr: `{"query": "weather", "limit": 5}`,
			want:    map[string]string{"query": "weather", "limit": "5"},
			wantErr: false,
		},
		{
			name:    "boolean and null",
			jsonStr: `{"flag": true, "opt": null}`,
			want:    map[string]string{"flag": "true", "opt": ""},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			jsonStr: `{invalid`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertJSONToMapStringString(tt.jsonStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("convertJSONToMapStringString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("convertJSONToMapStringString() length = %v, want %v", len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("convertJSONToMapStringString()[%v] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}