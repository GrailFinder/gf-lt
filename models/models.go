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
			Content   string          `json:"content"`
			ToolCalls []ToolDeltaResp `json:"tool_calls"`
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
}

type TextContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ImageContentPart struct {
	Type     string `json:"type"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
}

// RoleMsg represents a message with content that can be either a simple string or structured content parts
type RoleMsg struct {
	Role            string        `json:"role"`
	Content         string        `json:"-"`
	ContentParts    []interface{} `json:"-"`
	ToolCallID      string        `json:"tool_call_id,omitempty"` // For tool response messages
	KnownTo         []string      `json:"known_to,omitempty"`
	hasContentParts bool          // Flag to indicate which content type to marshal
}

// MarshalJSON implements custom JSON marshaling for RoleMsg
func (m RoleMsg) MarshalJSON() ([]byte, error) {
	if m.hasContentParts {
		// Use structured content format
		aux := struct {
			Role       string        `json:"role"`
			Content    []interface{} `json:"content"`
			ToolCallID string        `json:"tool_call_id,omitempty"`
			KnownTo    []string      `json:"known_to,omitempty"`
		}{
			Role:       m.Role,
			Content:    m.ContentParts,
			ToolCallID: m.ToolCallID,
			KnownTo:    m.KnownTo,
		}
		return json.Marshal(aux)
	} else {
		// Use simple content format
		aux := struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id,omitempty"`
			KnownTo    []string `json:"known_to,omitempty"`
		}{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			KnownTo:    m.KnownTo,
		}
		return json.Marshal(aux)
	}
}

// UnmarshalJSON implements custom JSON unmarshaling for RoleMsg
func (m *RoleMsg) UnmarshalJSON(data []byte) error {
	// First, try to unmarshal as structured content format
	var structured struct {
		Role       string        `json:"role"`
		Content    []interface{} `json:"content"`
		ToolCallID string        `json:"tool_call_id,omitempty"`
		KnownTo    []string      `json:"known_to,omitempty"`
	}
	if err := json.Unmarshal(data, &structured); err == nil && len(structured.Content) > 0 {
		m.Role = structured.Role
		m.ContentParts = structured.Content
		m.ToolCallID = structured.ToolCallID
		m.KnownTo = structured.KnownTo
		m.hasContentParts = true
		return nil
	}

	// Otherwise, unmarshal as simple content format
	var simple struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		ToolCallID string `json:"tool_call_id,omitempty"`
		KnownTo    []string `json:"known_to,omitempty"`
	}
	if err := json.Unmarshal(data, &simple); err != nil {
		return err
	}
	m.Role = simple.Role
	m.Content = simple.Content
	m.ToolCallID = simple.ToolCallID
	m.KnownTo = simple.KnownTo
	m.hasContentParts = false
	return nil
}

func (m RoleMsg) ToText(i int) string {
	icon := fmt.Sprintf("(%d)", i)

	// Convert content to string representation
	contentStr := ""
	if !m.hasContentParts {
		contentStr = m.Content
	} else {
		// For structured content, just take the text parts
		var textParts []string
		for _, part := range m.ContentParts {
			if partMap, ok := part.(map[string]interface{}); ok {
				if partType, exists := partMap["type"]; exists && partType == "text" {
					if textVal, textExists := partMap["text"]; textExists {
						if textStr, isStr := textVal.(string); isStr {
							textParts = append(textParts, textStr)
						}
					}
				}
			}
		}
		contentStr = strings.Join(textParts, " ") + " "
	}

	// check if already has role annotation (/completion makes them)
	if !strings.HasPrefix(contentStr, m.Role+":") {
		icon = fmt.Sprintf("(%d) <%s>: ", i, m.Role)
	}
	textMsg := fmt.Sprintf("[-:-:b]%s[-:-:-]\n%s\n", icon, contentStr)
	return strings.ReplaceAll(textMsg, "\n\n", "\n")
}

func (m RoleMsg) ToPrompt() string {
	contentStr := ""
	if !m.hasContentParts {
		contentStr = m.Content
	} else {
		// For structured content, just take the text parts
		var textParts []string
		for _, part := range m.ContentParts {
			if partMap, ok := part.(map[string]interface{}); ok {
				if partType, exists := partMap["type"]; exists && partType == "text" {
					if textVal, textExists := partMap["text"]; textExists {
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
		hasContentParts: false,
	}
}

// NewMultimodalMsg creates a RoleMsg with structured content parts (text and images)
func NewMultimodalMsg(role string, contentParts []interface{}) RoleMsg {
	return RoleMsg{
		Role:            role,
		ContentParts:    contentParts,
		hasContentParts: true,
	}
}

// HasContent returns true if the message has either string content or structured content parts
func (m RoleMsg) HasContent() bool {
	if m.Content != "" {
		return true
	}
	if m.hasContentParts && len(m.ContentParts) > 0 {
		return true
	}
	return false
}

// IsContentParts returns true if the message uses structured content parts
func (m RoleMsg) IsContentParts() bool {
	return m.hasContentParts
}

// GetContentParts returns the content parts of the message
func (m RoleMsg) GetContentParts() []interface{} {
	return m.ContentParts
}

// Copy creates a copy of the RoleMsg with all fields
func (m RoleMsg) Copy() RoleMsg {
	return RoleMsg{
		Role:            m.Role,
		Content:         m.Content,
		ContentParts:    m.ContentParts,
		ToolCallID:      m.ToolCallID,
		hasContentParts: m.hasContentParts,
	}
}

// AddTextPart adds a text content part to the message
func (m *RoleMsg) AddTextPart(text string) {
	if !m.hasContentParts {
		// Convert to content parts format
		if m.Content != "" {
			m.ContentParts = []interface{}{TextContentPart{Type: "text", Text: m.Content}}
		} else {
			m.ContentParts = []interface{}{}
		}
		m.hasContentParts = true
	}

	textPart := TextContentPart{Type: "text", Text: text}
	m.ContentParts = append(m.ContentParts, textPart)
}

// AddImagePart adds an image content part to the message
func (m *RoleMsg) AddImagePart(imageURL string) {
	if !m.hasContentParts {
		// Convert to content parts format
		if m.Content != "" {
			m.ContentParts = []interface{}{TextContentPart{Type: "text", Text: m.Content}}
		} else {
			m.ContentParts = []interface{}{}
		}
		m.hasContentParts = true
	}

	imagePart := ImageContentPart{
		Type: "image_url",
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
	for i, m := range cb.Messages {
		cb.Messages[i].Content = strings.ReplaceAll(m.Content, oldname, newname)
		cb.Messages[i].Role = strings.ReplaceAll(m.Role, oldname, newname)
	}
}

func (cb *ChatBody) ListRoles() []string {
	namesMap := make(map[string]struct{})
	for _, m := range cb.Messages {
		namesMap[m.Role] = struct{}{}
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
	namesMap := make(map[string]struct{})
	for _, m := range cb.Messages {
		namesMap[m.Role] = struct{}{}
	}
	ss := make([]string, 0, 1+len(namesMap))
	ss = append(ss, "<|im_end|>")
	for k := range namesMap {
		ss = append(ss, k+":\n")
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
	Prompt        interface{} `json:"prompt"` // Can be string or object with prompt_string and multimodal_data
	Temperature   float32     `json:"temperature"`
	DryMultiplier float32     `json:"dry_multiplier"`
	Stop          []string    `json:"stop"`
	MinP          float32     `json:"min_p"`
	NPredict      int32       `json:"n_predict"`
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
	var finalPrompt interface{}
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
