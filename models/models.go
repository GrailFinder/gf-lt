package models

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type FuncCall struct {
	ID   string            `json:"id,omitempty"`
	Name string            `json:"name"`
	Args map[string]string `json:"args"`
}

type ToolCall struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	Args string `json:"arguments"`
}

type LLMResp struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Index        int    `json:"index"`
		Message      struct {
			Content string `json:"content"`
			Role    string `json:"role"`
		} `json:"message"`
	} `json:"choices"`
	Created int    `json:"created"`
	Model   string `json:"model"`
	Object  string `json:"object"`
	Usage   struct {
		CompletionTokens int `json:"completion_tokens"`
		PromptTokens     int `json:"prompt_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	ID string `json:"id"`
}

type ToolDeltaFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDeltaResp struct {
	ID       string        `json:"id,omitempty"`
	Index    int           `json:"index"`
	Function ToolDeltaFunc `json:"function"`
}

// for streaming
type LLMRespChunk struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Index        int    `json:"index"`
		Delta        struct {
			Content          string          `json:"content"`
			ReasoningContent string          `json:"reasoning_content"`
			ToolCalls        []ToolDeltaResp `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Created int    `json:"created"`
	ID      string `json:"id"`
	Model   string `json:"model"`
	Object  string `json:"object"`
	Usage   struct {
		CompletionTokens int `json:"completion_tokens"`
		PromptTokens     int `json:"prompt_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type TextChunk struct {
	Chunk     string
	ToolChunk string
	Finished  bool
	ToolResp  bool
	FuncName  string
	ToolID    string
	Reasoning string // For models that send reasoning separately (OpenRouter, etc.)
}

type TextContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ImageContentPart struct {
	Type     string `json:"type"`
	Path     string `json:"path,omitempty"` // Store original file path
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
}

// RoleMsg represents a message with content that can be either a simple string or structured content parts
type RoleMsg struct {
	Role            string         `json:"role"`
	Content         string         `json:"-"`
	ContentParts    []any          `json:"-"`
	ToolCallID      string         `json:"tool_call_id,omitempty"`     // For tool response messages
	ToolCalls       []ToolCall     `json:"tool_calls,omitempty"`       // For assistant messages with tool calls
	IsShellCommand  bool           `json:"is_shell_command,omitempty"` // True for shell command outputs (always shown)
	KnownTo         []string       `json:"known_to,omitempty"`
	Stats           *ResponseStats `json:"stats"`
	HasContentParts bool           // Flag to indicate which content type to marshal
}

// MarshalJSON implements custom JSON marshaling for RoleMsg
//
//nolint:gocritic
func (m RoleMsg) MarshalJSON() ([]byte, error) {
	if m.HasContentParts {
		// Use structured content format
		aux := struct {
			Role           string         `json:"role"`
			Content        []any          `json:"content"`
			ToolCallID     string         `json:"tool_call_id,omitempty"`
			ToolCalls      []ToolCall     `json:"tool_calls,omitempty"`
			IsShellCommand bool           `json:"is_shell_command,omitempty"`
			KnownTo        []string       `json:"known_to,omitempty"`
			Stats          *ResponseStats `json:"stats,omitempty"`
		}{
			Role:           m.Role,
			Content:        m.ContentParts,
			ToolCallID:     m.ToolCallID,
			ToolCalls:      m.ToolCalls,
			IsShellCommand: m.IsShellCommand,
			KnownTo:        m.KnownTo,
			Stats:          m.Stats,
		}
		return json.Marshal(aux)
	} else {
		// Use simple content format
		aux := struct {
			Role           string         `json:"role"`
			Content        string         `json:"content"`
			ToolCallID     string         `json:"tool_call_id,omitempty"`
			ToolCalls      []ToolCall     `json:"tool_calls,omitempty"`
			IsShellCommand bool           `json:"is_shell_command,omitempty"`
			KnownTo        []string       `json:"known_to,omitempty"`
			Stats          *ResponseStats `json:"stats,omitempty"`
		}{
			Role:           m.Role,
			Content:        m.Content,
			ToolCallID:     m.ToolCallID,
			ToolCalls:      m.ToolCalls,
			IsShellCommand: m.IsShellCommand,
			KnownTo:        m.KnownTo,
			Stats:          m.Stats,
		}
		return json.Marshal(aux)
	}
}

// UnmarshalJSON implements custom JSON unmarshaling for RoleMsg
func (m *RoleMsg) UnmarshalJSON(data []byte) error {
	// First, try to unmarshal as structured content format
	var structured struct {
		Role           string         `json:"role"`
		Content        []any          `json:"content"`
		ToolCallID     string         `json:"tool_call_id,omitempty"`
		ToolCalls      []ToolCall     `json:"tool_calls,omitempty"`
		IsShellCommand bool           `json:"is_shell_command,omitempty"`
		KnownTo        []string       `json:"known_to,omitempty"`
		Stats          *ResponseStats `json:"stats,omitempty"`
	}
	if err := json.Unmarshal(data, &structured); err == nil && len(structured.Content) > 0 {
		m.Role = structured.Role
		m.ContentParts = structured.Content
		m.ToolCallID = structured.ToolCallID
		m.ToolCalls = structured.ToolCalls
		m.IsShellCommand = structured.IsShellCommand
		m.KnownTo = structured.KnownTo
		m.Stats = structured.Stats
		m.HasContentParts = true
		return nil
	}

	// Otherwise, unmarshal as simple content format
	var simple struct {
		Role           string         `json:"role"`
		Content        string         `json:"content"`
		ToolCallID     string         `json:"tool_call_id,omitempty"`
		ToolCalls      []ToolCall     `json:"tool_calls,omitempty"`
		IsShellCommand bool           `json:"is_shell_command,omitempty"`
		KnownTo        []string       `json:"known_to,omitempty"`
		Stats          *ResponseStats `json:"stats,omitempty"`
	}
	if err := json.Unmarshal(data, &simple); err != nil {
		return err
	}
	m.Role = simple.Role
	m.Content = simple.Content
	m.ToolCallID = simple.ToolCallID
	m.ToolCalls = simple.ToolCalls
	m.IsShellCommand = simple.IsShellCommand
	m.KnownTo = simple.KnownTo
	m.Stats = simple.Stats
	m.HasContentParts = false
	return nil
}

func (m *RoleMsg) ToPrompt() string {
	var contentStr string
	if !m.HasContentParts {
		contentStr = m.Content
	} else {
		// For structured content, just take the text parts
		var textParts []string
		for _, part := range m.ContentParts {
			switch p := part.(type) {
			case TextContentPart:
				if p.Type == "text" {
					textParts = append(textParts, p.Text)
				}
			case ImageContentPart:
				// skip images for text display
			case map[string]any:
				if partType, exists := p["type"]; exists && partType == "text" {
					if textVal, textExists := p["text"]; textExists {
						if textStr, isStr := textVal.(string); isStr {
							textParts = append(textParts, textStr)
						}
					}
				}
			}
		}
		contentStr = strings.Join(textParts, " ") + " "
	}
	return strings.ReplaceAll(fmt.Sprintf("%s:\n%s", m.Role, contentStr), "\n\n", "\n")
}

// NewRoleMsg creates a simple RoleMsg with string content
func NewRoleMsg(role, content string) RoleMsg {
	return RoleMsg{
		Role:            role,
		Content:         content,
		HasContentParts: false,
	}
}

// NewMultimodalMsg creates a RoleMsg with structured content parts (text and images)
func NewMultimodalMsg(role string, contentParts []any) RoleMsg {
	return RoleMsg{
		Role:            role,
		ContentParts:    contentParts,
		HasContentParts: true,
	}
}

// HasContent returns true if the message has either string content or structured content parts
func (m *RoleMsg) HasContent() bool {
	if m.Content != "" {
		return true
	}
	if m.HasContentParts && len(m.ContentParts) > 0 {
		return true
	}
	return false
}

// IsContentParts returns true if the message uses structured content parts
func (m *RoleMsg) IsContentParts() bool {
	return m.HasContentParts
}

// GetContentParts returns the content parts of the message
func (m *RoleMsg) GetContentParts() []any {
	return m.ContentParts
}

// Copy creates a copy of the RoleMsg with all fields
func (m *RoleMsg) Copy() RoleMsg {
	return RoleMsg{
		Role:            m.Role,
		Content:         m.Content,
		ContentParts:    m.ContentParts,
		ToolCallID:      m.ToolCallID,
		KnownTo:         m.KnownTo,
		Stats:           m.Stats,
		HasContentParts: m.HasContentParts,
	}
}

// GetText returns the text content of the message, handling both
// simple Content and multimodal ContentParts formats.
func (m *RoleMsg) GetText() string {
	if !m.HasContentParts {
		return m.Content
	}
	var textParts []string
	for _, part := range m.ContentParts {
		switch p := part.(type) {
		case TextContentPart:
			if p.Type == "text" {
				textParts = append(textParts, p.Text)
			}
		case map[string]any:
			if partType, exists := p["type"]; exists {
				if partType == "text" {
					if textVal, textExists := p["text"]; textExists {
						if textStr, isStr := textVal.(string); isStr {
							textParts = append(textParts, textStr)
						}
					}
				}
			}
		}
	}
	return strings.Join(textParts, " ")
}

// SetText updates the text content of the message. If the message has
// ContentParts (multimodal), it updates the text parts while preserving
// images. If not, it sets the simple Content field.
func (m *RoleMsg) SetText(text string) {
	if !m.HasContentParts {
		m.Content = text
		return
	}
	var newParts []any
	for _, part := range m.ContentParts {
		switch p := part.(type) {
		case TextContentPart:
			if p.Type == "text" {
				p.Text = text
				newParts = append(newParts, p)
			} else {
				newParts = append(newParts, p)
			}
		case map[string]any:
			if partType, exists := p["type"]; exists && partType == "text" {
				p["text"] = text
				newParts = append(newParts, p)
			} else {
				newParts = append(newParts, p)
			}
		default:
			newParts = append(newParts, part)
		}
	}
	m.ContentParts = newParts
}

// AddTextPart adds a text content part to the message
func (m *RoleMsg) AddTextPart(text string) {
	if !m.HasContentParts {
		// Convert to content parts format
		if m.Content != "" {
			m.ContentParts = []any{TextContentPart{Type: "text", Text: m.Content}}
		} else {
			m.ContentParts = []any{}
		}
		m.HasContentParts = true
	}
	textPart := TextContentPart{Type: "text", Text: text}
	m.ContentParts = append(m.ContentParts, textPart)
}

// AddImagePart adds an image content part to the message
func (m *RoleMsg) AddImagePart(imageURL, imagePath string) {
	if !m.HasContentParts {
		// Convert to content parts format
		if m.Content != "" {
			m.ContentParts = []any{TextContentPart{Type: "text", Text: m.Content}}
		} else {
			m.ContentParts = []any{}
		}
		m.HasContentParts = true
	}
	imagePart := ImageContentPart{
		Type: "image_url",
		Path: imagePath, // Store the original file path
		ImageURL: struct {
			URL string `json:"url"`
		}{URL: imageURL},
	}
	m.ContentParts = append(m.ContentParts, imagePart)
}

// CreateImageURLFromPath creates a data URL from an image file path
func CreateImageURLFromPath(imagePath string) (string, error) {
	// Read the image file
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", err
	}

	// Determine the image format based on file extension
	var mimeType string
	switch {
	case strings.HasSuffix(strings.ToLower(imagePath), ".png"):
		mimeType = "image/png"
	case strings.HasSuffix(strings.ToLower(imagePath), ".jpg"):
		fallthrough
	case strings.HasSuffix(strings.ToLower(imagePath), ".jpeg"):
		mimeType = "image/jpeg"
	case strings.HasSuffix(strings.ToLower(imagePath), ".gif"):
		mimeType = "image/gif"
	case strings.HasSuffix(strings.ToLower(imagePath), ".webp"):
		mimeType = "image/webp"
	default:
		mimeType = "image/jpeg" // default
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(data)

	// Create data URL
	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded), nil
}

type ChatBody struct {
	Model    string    `json:"model"`
	Stream   bool      `json:"stream"`
	Messages []RoleMsg `json:"messages"`
}

func (cb *ChatBody) Rename(oldname, newname string) {
	for i := range cb.Messages {
		cb.Messages[i].Content = strings.ReplaceAll(cb.Messages[i].Content, oldname, newname)
		cb.Messages[i].Role = strings.ReplaceAll(cb.Messages[i].Role, oldname, newname)
	}
}

func (cb *ChatBody) ListRoles() []string {
	namesMap := make(map[string]struct{})
	for i := range cb.Messages {
		namesMap[cb.Messages[i].Role] = struct{}{}
	}
	resp := make([]string, len(namesMap))
	i := 0
	for k := range namesMap {
		resp[i] = k
		i++
	}
	return resp
}

func (cb *ChatBody) MakeStopSlice() []string {
	return cb.MakeStopSliceExcluding("", cb.ListRoles())
}

func (cb *ChatBody) MakeStopSliceExcluding(
	excludeRole string, roleList []string,
) []string {
	ss := []string{}
	for _, role := range roleList {
		// Skip the excluded role (typically the current speaker)
		if role == excludeRole {
			continue
		}
		// Add multiple variations to catch different formatting
		ss = append(ss,
			role+":\n",   // Most common: role with newline
			role+":",     // Role with colon but no newline
			role+": ",    // Role with colon and single space
			role+":  ",   // Role with colon and double space (common tokenization)
			role+":  \n", // Role with colon and double space (common tokenization)
			role+":   ",  // Role with colon and triple space
		)
	}
	return ss
}

type EmbeddingResp struct {
	Embedding []float32 `json:"embedding"`
	Index     uint32    `json:"index"`
}

// type EmbeddingsResp struct {
// 	Model  string `json:"model"`
// 	Object string `json:"object"`
// 	Usage  struct {
// 		PromptTokens int `json:"prompt_tokens"`
// 		TotalTokens  int `json:"total_tokens"`
// 	} `json:"usage"`
// 	Data []struct {
// 		Embedding []float32 `json:"embedding"`
// 		Index     int       `json:"index"`
// 		Object    string    `json:"object"`
// 	} `json:"data"`
// }

// === tools models

type ToolArgProps struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ToolFuncParams struct {
	Type       string                  `json:"type"`
	Properties map[string]ToolArgProps `json:"properties"`
	Required   []string                `json:"required"`
}

type ToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  ToolFuncParams `json:"parameters"`
}

type Tool struct {
	Type     string   `json:"type"`
	Function ToolFunc `json:"function"`
}

type OpenAIReq struct {
	*ChatBody
	Tools []Tool `json:"tools"`
}

// ===

// type LLMModels struct {
// 	Object string `json:"object"`
// 	Data   []struct {
// 		ID      string `json:"id"`
// 		Object  string `json:"object"`
// 		Created int    `json:"created"`
// 		OwnedBy string `json:"owned_by"`
// 		Meta    struct {
// 			VocabType int   `json:"vocab_type"`
// 			NVocab    int   `json:"n_vocab"`
// 			NCtxTrain int   `json:"n_ctx_train"`
// 			NEmbd     int   `json:"n_embd"`
// 			NParams   int64 `json:"n_params"`
// 			Size      int64 `json:"size"`
// 		} `json:"meta"`
// 	} `json:"data"`
// }

type LlamaCPPReq struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
	// For multimodal requests, prompt should be an object with prompt_string and multimodal_data
	// For regular requests, prompt is a string
	Prompt        any      `json:"prompt"` // Can be string or object with prompt_string and multimodal_data
	Temperature   float32  `json:"temperature"`
	DryMultiplier float32  `json:"dry_multiplier"`
	Stop          []string `json:"stop"`
	MinP          float32  `json:"min_p"`
	NPredict      int32    `json:"n_predict"`
	// MaxTokens        int     `json:"max_tokens"`
	// DryBase          float64 `json:"dry_base"`
	// DryAllowedLength int     `json:"dry_allowed_length"`
	// DryPenaltyLastN  int     `json:"dry_penalty_last_n"`
	// CachePrompt      bool    `json:"cache_prompt"`
	// DynatempRange    int     `json:"dynatemp_range"`
	// DynatempExponent int     `json:"dynatemp_exponent"`
	// TopK             int     `json:"top_k"`
	// TopP             float32 `json:"top_p"`
	// TypicalP         int     `json:"typical_p"`
	// XtcProbability   int     `json:"xtc_probability"`
	// XtcThreshold     float32 `json:"xtc_threshold"`
	// RepeatLastN      int     `json:"repeat_last_n"`
	// RepeatPenalty    int     `json:"repeat_penalty"`
	// PresencePenalty  int     `json:"presence_penalty"`
	// FrequencyPenalty int     `json:"frequency_penalty"`
	// Samplers         string  `json:"samplers"`
}

type PromptObject struct {
	PromptString   string   `json:"prompt_string"`
	MultimodalData []string `json:"multimodal_data,omitempty"`
	// Alternative field name used by some llama.cpp implementations
	ImageData []string `json:"image_data,omitempty"` // For compatibility
}

func NewLCPReq(prompt, model string, multimodalData []string, props map[string]float32, stopStrings []string) LlamaCPPReq {
	var finalPrompt any
	if len(multimodalData) > 0 {
		// When multimodal data is present, use the object format as per Python example:
		// { "prompt": { "prompt_string": "...", "multimodal_data": [...] } }
		finalPrompt = PromptObject{
			PromptString:   prompt,
			MultimodalData: multimodalData,
			ImageData:      multimodalData, // Also populate for compatibility with different llama.cpp versions
		}
	} else {
		// When no multimodal data, use plain string
		finalPrompt = prompt
	}
	return LlamaCPPReq{
		Model:         model,
		Stream:        true,
		Prompt:        finalPrompt,
		Temperature:   props["temperature"],
		DryMultiplier: props["dry_multiplier"],
		Stop:          stopStrings,
		MinP:          props["min_p"],
		NPredict:      int32(props["n_predict"]),
	}
}

type LlamaCPPResp struct {
	Content string `json:"content"`
	Stop    bool   `json:"stop"`
}

type LCPModels struct {
	Data []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
		Created int    `json:"created"`
		InCache bool   `json:"in_cache"`
		Path    string `json:"path"`
		Status  struct {
			Value string   `json:"value"`
			Args  []string `json:"args"`
		} `json:"status"`
	} `json:"data"`
	Object string `json:"object"`
}

func (lcp *LCPModels) ListModels() []string {
	resp := make([]string, 0, len(lcp.Data))
	for _, model := range lcp.Data {
		resp = append(resp, model.ID)
	}
	return resp
}

type ResponseStats struct {
	Tokens       int
	Duration     float64
	TokensPerSec float64
}

type ChatRoundReq struct {
	UserMsg string
	Role    string
	Regen   bool
	Resume  bool
}

type APIType int

const (
	APITypeChat APIType = iota
	APITypeCompletion
)
