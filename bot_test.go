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

func TestParseKnownToTag(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		enabled     bool
		tag         string
		wantCleaned string
		wantKnownTo []string
	}{
		{
			name:        "feature disabled returns original",
			content:     "Hello __known_to_chars__Alice__",
			enabled:     false,
			tag:         "__known_to_chars__",
			wantCleaned: "Hello __known_to_chars__Alice__",
			wantKnownTo: nil,
		},
		{
			name:        "no tag returns original",
			content:     "Hello Alice",
			enabled:     true,
			tag:         "__known_to_chars__",
			wantCleaned: "Hello Alice",
			wantKnownTo: nil,
		},
		{
			name:        "single tag with one char",
			content:     "Hello __known_to_chars__Alice__",
			enabled:     true,
			tag:         "__known_to_chars__",
			wantCleaned: "Hello",
			wantKnownTo: []string{"Alice"},
		},
		{
			name:        "single tag with two chars",
			content:     "Secret __known_to_chars__Alice,Bob__ message",
			enabled:     true,
			tag:         "__known_to_chars__",
			wantCleaned: "Secret  message",
			wantKnownTo: []string{"Alice", "Bob"},
		},
		{
			name:        "tag at beginning",
			content:     "__known_to_chars__Alice__ Hello",
			enabled:     true,
			tag:         "__known_to_chars__",
			wantCleaned: "Hello",
			wantKnownTo: []string{"Alice"},
		},
		{
			name:        "tag at end",
			content:     "Hello __known_to_chars__Alice__",
			enabled:     true,
			tag:         "__known_to_chars__",
			wantCleaned: "Hello",
			wantKnownTo: []string{"Alice"},
		},
		{
			name:        "multiple tags",
			content:     "First __known_to_chars__Alice__ then __known_to_chars__Bob__",
			enabled:     true,
			tag:         "__known_to_chars__",
			wantCleaned: "First  then",
			wantKnownTo: []string{"Alice", "Bob"},
		},
		{
			name:        "custom tag",
			content:     "Secret __secret__Alice,Bob__ message",
			enabled:     true,
			tag:         "__secret__",
			wantCleaned: "Secret  message",
			wantKnownTo: []string{"Alice", "Bob"},
		},
		{
			name:        "empty list",
			content:     "Secret __known_to_chars____",
			enabled:     true,
			tag:         "__known_to_chars__",
			wantCleaned: "Secret",
			wantKnownTo: nil,
		},
		{
			name:        "whitespace around commas",
			content:     "__known_to_chars__ Alice , Bob , Carl __",
			enabled:     true,
			tag:         "__known_to_chars__",
			wantCleaned: "",
			wantKnownTo: []string{"Alice", "Bob", "Carl"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up config
			testCfg := &config.Config{
				CharSpecificContextEnabled: tt.enabled,
				CharSpecificContextTag:     tt.tag,
			}
			cfg = testCfg
			knownTo := parseKnownToTag(tt.content)
			if len(knownTo) != len(tt.wantKnownTo) {
				t.Errorf("parseKnownToTag() knownTo length = %v, want %v", len(knownTo), len(tt.wantKnownTo))
				t.Logf("got: %v", knownTo)
				t.Logf("want: %v", tt.wantKnownTo)
			} else {
				for i, got := range knownTo {
					if got != tt.wantKnownTo[i] {
						t.Errorf("parseKnownToTag() knownTo[%d] = %q, want %q", i, got, tt.wantKnownTo[i])
					}
				}
			}
		})
	}
}

func TestProcessMessageTag(t *testing.T) {
	tests := []struct {
		name    string
		msg     models.RoleMsg
		enabled bool
		tag     string
		wantMsg models.RoleMsg
	}{
		{
			name: "feature disabled returns unchanged",
			msg: models.RoleMsg{
				Role:    "Alice",
				Content: "Secret __known_to_chars__Bob__",
			},
			enabled: false,
			tag:     "__known_to_chars__",
			wantMsg: models.RoleMsg{
				Role:    "Alice",
				Content: "Secret __known_to_chars__Bob__",
				KnownTo: nil,
			},
		},
		{
			name: "no tag, no knownTo",
			msg: models.RoleMsg{
				Role:    "Alice",
				Content: "Hello everyone",
			},
			enabled: true,
			tag:     "__known_to_chars__",
			wantMsg: models.RoleMsg{
				Role:    "Alice",
				Content: "Hello everyone",
				KnownTo: nil,
			},
		},
		{
			name: "tag with Bob, adds Alice automatically",
			msg: models.RoleMsg{
				Role:    "Alice",
				Content: "Secret __known_to_chars__Bob__",
			},
			enabled: true,
			tag:     "__known_to_chars__",
			wantMsg: models.RoleMsg{
				Role:    "Alice",
				Content: "Secret",
				KnownTo: []string{"Bob", "Alice"},
			},
		},
		{
			name: "tag already includes sender",
			msg: models.RoleMsg{
				Role:    "Alice",
				Content: "__known_to_chars__Alice,Bob__",
			},
			enabled: true,
			tag:     "__known_to_chars__",
			wantMsg: models.RoleMsg{
				Role:    "Alice",
				Content: "",
				KnownTo: []string{"Alice", "Bob"},
			},
		},
		{
			name: "knownTo already set (from DB), tag still processed",
			msg: models.RoleMsg{
				Role:    "Alice",
				Content: "Secret __known_to_chars__Bob__",
				KnownTo: []string{"Alice"}, // from previous processing
			},
			enabled: true,
			tag:     "__known_to_chars__",
			wantMsg: models.RoleMsg{
				Role:    "Alice",
				Content: "Secret",
				KnownTo: []string{"Bob", "Alice"},
			},
		},
		{
			name: "example from real use",
			msg: models.RoleMsg{
				Role:    "Alice",
				Content: "I'll start with a simple one! The word is 'banana'. (ooc: __known_to_chars__Bob__)",
				KnownTo: []string{"Alice"}, // from previous processing
			},
			enabled: true,
			tag:     "__known_to_chars__",
			wantMsg: models.RoleMsg{
				Role:    "Alice",
				Content: "I'll start with a simple one! The word is 'banana'. (ooc: __known_to_chars__Bob__)",
				KnownTo: []string{"Bob", "Alice"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCfg := &config.Config{
				CharSpecificContextEnabled: tt.enabled,
				CharSpecificContextTag:     tt.tag,
			}
			cfg = testCfg
			got := processMessageTag(tt.msg)
			if len(got.KnownTo) != len(tt.wantMsg.KnownTo) {
				t.Errorf("processMessageTag() KnownTo length = %v, want %v", len(got.KnownTo), len(tt.wantMsg.KnownTo))
				t.Logf("got: %v", got.KnownTo)
				t.Logf("want: %v", tt.wantMsg.KnownTo)
			} else {
				// order may differ; check membership
				for _, want := range tt.wantMsg.KnownTo {
					found := false
					for _, gotVal := range got.KnownTo {
						if gotVal == want {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("processMessageTag() missing KnownTo entry %q, got %v", want, got.KnownTo)
					}
				}
			}
		})
	}
}

func TestFilterMessagesForCharacter(t *testing.T) {
	messages := []models.RoleMsg{
		{Role: "system", Content: "System message", KnownTo: nil}, // visible to all
		{Role: "Alice", Content: "Hello everyone", KnownTo: nil},  // visible to all
		{Role: "Alice", Content: "Secret for Bob", KnownTo: []string{"Alice", "Bob"}},
		{Role: "Bob", Content: "Reply to Alice", KnownTo: []string{"Alice", "Bob"}},
		{Role: "Alice", Content: "Private to Carl", KnownTo: []string{"Alice", "Carl"}},
		{Role: "Carl", Content: "Hi all", KnownTo: nil}, // visible to all
	}

	tests := []struct {
		name        string
		enabled     bool
		character   string
		wantIndices []int // indices from original messages that should be included
	}{
		{
			name:        "feature disabled returns all",
			enabled:     false,
			character:   "Alice",
			wantIndices: []int{0, 1, 2, 3, 4, 5},
		},
		{
			name:        "character empty returns all",
			enabled:     true,
			character:   "",
			wantIndices: []int{0, 1, 2, 3, 4, 5},
		},
		{
			name:        "Alice sees all including Carl-private",
			enabled:     true,
			character:   "Alice",
			wantIndices: []int{0, 1, 2, 3, 4, 5},
		},
		{
			name:        "Bob sees Alice-Bob secrets and all public",
			enabled:     true,
			character:   "Bob",
			wantIndices: []int{0, 1, 2, 3, 5},
		},
		{
			name:        "Carl sees Alice-Carl secret and public",
			enabled:     true,
			character:   "Carl",
			wantIndices: []int{0, 1, 4, 5},
		},
		{
			name:        "David sees only public messages",
			enabled:     true,
			character:   "David",
			wantIndices: []int{0, 1, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCfg := &config.Config{
				CharSpecificContextEnabled: tt.enabled,
				CharSpecificContextTag:     "__known_to_chars__",
			}
			cfg = testCfg

			got := filterMessagesForCharacter(messages, tt.character)

			if len(got) != len(tt.wantIndices) {
				t.Errorf("filterMessagesForCharacter() returned %d messages, want %d", len(got), len(tt.wantIndices))
				t.Logf("got: %v", got)
				return
			}

			for i, idx := range tt.wantIndices {
				if got[i].Content != messages[idx].Content {
					t.Errorf("filterMessagesForCharacter() message %d content = %q, want %q", i, got[i].Content, messages[idx].Content)
				}
			}
		})
	}
}

func TestRoleMsgCopyPreservesKnownTo(t *testing.T) {
	// Test that the Copy() method preserves the KnownTo field
	originalMsg := models.RoleMsg{
		Role:    "Alice",
		Content: "Test message",
		KnownTo: []string{"Bob", "Charlie"},
	}

	copiedMsg := originalMsg.Copy()

	if copiedMsg.Role != originalMsg.Role {
		t.Errorf("Copy() failed to preserve Role: got %q, want %q", copiedMsg.Role, originalMsg.Role)
	}
	if copiedMsg.Content != originalMsg.Content {
		t.Errorf("Copy() failed to preserve Content: got %q, want %q", copiedMsg.Content, originalMsg.Content)
	}
	if !reflect.DeepEqual(copiedMsg.KnownTo, originalMsg.KnownTo) {
		t.Errorf("Copy() failed to preserve KnownTo: got %v, want %v", copiedMsg.KnownTo, originalMsg.KnownTo)
	}
	if copiedMsg.ToolCallID != originalMsg.ToolCallID {
		t.Errorf("Copy() failed to preserve ToolCallID: got %q, want %q", copiedMsg.ToolCallID, originalMsg.ToolCallID)
	}
	if copiedMsg.IsContentParts() != originalMsg.IsContentParts() {
		t.Errorf("Copy() failed to preserve hasContentParts flag")
	}
}

func TestKnownToFieldPreservationScenario(t *testing.T) {
	// Test the specific scenario from the log where KnownTo field was getting lost
	originalMsg := models.RoleMsg{
		Role:    "Alice",
		Content: `Alice: "Okay, Bob. The word is... **'Ephemeral'**. (ooc: __known_to_chars__Bob__)"`,
		KnownTo: []string{"Bob"}, // This was detected in the log
	}

	t.Logf("Original message - Role: %s, Content: %s, KnownTo: %v",
		originalMsg.Role, originalMsg.Content, originalMsg.KnownTo)

	// Simulate what happens when the message gets copied during processing
	copiedMsg := originalMsg.Copy()

	t.Logf("Copied message - Role: %s, Content: %s, KnownTo: %v",
		copiedMsg.Role, copiedMsg.Content, copiedMsg.KnownTo)

	// Check if KnownTo field survived the copy
	if len(copiedMsg.KnownTo) == 0 {
		t.Error("ERROR: KnownTo field was lost during copy!")
	} else {
		t.Log("SUCCESS: KnownTo field was preserved during copy!")
	}

	// Verify the content is the same
	if copiedMsg.Content != originalMsg.Content {
		t.Errorf("Content was changed during copy: got %s, want %s", copiedMsg.Content, originalMsg.Content)
	}

	// Verify the KnownTo slice is properly copied
	if !reflect.DeepEqual(copiedMsg.KnownTo, originalMsg.KnownTo) {
		t.Errorf("KnownTo was not properly copied: got %v, want %v", copiedMsg.KnownTo, originalMsg.KnownTo)
	}
}
