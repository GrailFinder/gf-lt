package models

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	// imageBaseDir is the base directory for displaying image paths.
	// If set, image paths will be shown relative to this directory.
	imageBaseDir = ""
)

// SetImageBaseDir sets the base directory for displaying image paths.
// If dir is empty, full paths will be shown.
func SetImageBaseDir(dir string) {
	imageBaseDir = dir
}

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
	ToolCallID      string         `json:"tool_call_id,omitempty"` // For tool response messages
	KnownTo         []string       `json:"known_to,omitempty"`
	Stats           *ResponseStats `json:"-"` // Display-only, not persisted
	hasContentParts bool           // Flag to indicate which content type to marshal
}

// MarshalJSON implements custom JSON marshaling for RoleMsg
func (m *RoleMsg) MarshalJSON() ([]byte, error) {
	if m.hasContentParts {
		// Use structured content format
		aux := struct {
			Role       string   `json:"role"`
			Content    []any    `json:"content"`
			ToolCallID string   `json:"tool_call_id,omitempty"`
			KnownTo    []string `json:"known_to,omitempty"`
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
			Role       string   `json:"role"`
			Content    string   `json:"content"`
			ToolCallID string   `json:"tool_call_id,omitempty"`
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
		Role       string   `json:"role"`
		Content    []any    `json:"content"`
		ToolCallID string   `json:"tool_call_id,omitempty"`
		KnownTo    []string `json:"known_to,omitempty"`
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
		Role       string   `json:"role"`
		Content    string   `json:"content"`
		ToolCallID string   `json:"tool_call_id,omitempty"`
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

func (m *RoleMsg) ToText(i int) string {
	var contentStr string
	var imageIndicators []string
	if !m.hasContentParts {
		contentStr = m.Content
	} else {
		var textParts []string
		for _, part := range m.ContentParts {
			switch p := part.(type) {
			case TextContentPart:
				if p.Type == "text" {
					textParts = append(textParts, p.Text)
				}
			case ImageContentPart:
				displayPath := p.Path
				if displayPath == "" {
					displayPath = "image"
				} else {
					displayPath = extractDisplayPath(displayPath)
				}
				imageIndicators = append(imageIndicators, fmt.Sprintf("[orange::i][image: %s][-:-:-]", displayPath))
			case map[string]any:
				if partType, exists := p["type"]; exists {
					switch partType {
					case "text":
						if textVal, textExists := p["text"]; textExists {
							if textStr, isStr := textVal.(string); isStr {
								textParts = append(textParts, textStr)
							}
						}
					case "image_url":
						var displayPath string
						if pathVal, pathExists := p["path"]; pathExists {
							if pathStr, isStr := pathVal.(string); isStr && pathStr != "" {
								displayPath = extractDisplayPath(pathStr)
							}
						}
						if displayPath == "" {
							displayPath = "image"
						}
						imageIndicators = append(imageIndicators, fmt.Sprintf("[orange::i][image: %s][-:-:-]", displayPath))
					}
				}
			}
		}
		contentStr = strings.Join(textParts, " ") + " "
	}
	contentStr, _ = strings.CutPrefix(contentStr, m.Role+":")
	icon := fmt.Sprintf("(%d) <%s>: ", i, m.Role)
	var finalContent strings.Builder
	if len(imageIndicators) > 0 {
		for _, indicator := range imageIndicators {
			finalContent.WriteString(indicator)
			finalContent.WriteString("\n")
		}
	}
	finalContent.WriteString(contentStr)
	if m.Stats != nil {
		finalContent.WriteString(fmt.Sprintf("\n[gray::i][%d tok, %.1fs, %.1f t/s][-:-:-]",
			m.Stats.Tokens, m.Stats.Duration, m.Stats.TokensPerSec))
	}
	textMsg := fmt.Sprintf("[-:-:b]%s[-:-:-]\n%s\n", icon, finalContent.String())
	return strings.ReplaceAll(textMsg, "\n\n", "\n")
}

func (m *RoleMsg) ToPrompt() string {
	var contentStr string
	if !m.hasContentParts {
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
		hasContentParts: false,
	}
}

// NewMultimodalMsg creates a RoleMsg with structured content parts (text and images)
func NewMultimodalMsg(role string, contentParts []any) RoleMsg {
	return RoleMsg{
		Role:            role,
		ContentParts:    contentParts,
		hasContentParts: true,
	}
}

// HasContent returns true if the message has either string content or structured content parts
func (m *RoleMsg) HasContent() bool {
	if m.Content != "" {
		return true
	}
	if m.hasContentParts && len(m.ContentParts) > 0 {
		return true
	}
	return false
}

// IsContentParts returns true if the message uses structured content parts
func (m *RoleMsg) IsContentParts() bool {
	return m.hasContentParts
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
		hasContentParts: m.hasContentParts,
	}
}

// AddTextPart adds a text content part to the message
func (m *RoleMsg) AddTextPart(text string) {
	if !m.hasContentParts {
		// Convert to content parts format
		if m.Content != "" {
			m.ContentParts = []any{TextContentPart{Type: "text", Text: m.Content}}
		} else {
			m.ContentParts = []any{}
		}
		m.hasContentParts = true
	}

	textPart := TextContentPart{Type: "text", Text: text}
	m.ContentParts = append(m.ContentParts, textPart)
}

// AddImagePart adds an image content part to the message
func (m *RoleMsg) AddImagePart(imageURL, imagePath string) {
	if !m.hasContentParts {
		// Convert to content parts format
		if m.Content != "" {
			m.ContentParts = []any{TextContentPart{Type: "text", Text: m.Content}}
		} else {
			m.ContentParts = []any{}
		}
		m.hasContentParts = true
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

// extractDisplayPath returns a path suitable for display, potentially relative to imageBaseDir
func extractDisplayPath(p string) string {
	if p == "" {
		return ""
	}

	// If base directory is set, try to make path relative to it
	if imageBaseDir != "" {
		if rel, err := filepath.Rel(imageBaseDir, p); err == nil {
			// Check if relative path doesn't start with ".." (meaning it's within base dir)
			// If it starts with "..", we might still want to show it as relative
			// but for now we show full path if it goes outside base dir
			if !strings.HasPrefix(rel, "..") {
				p = rel
			}
		}
	}

	// Truncate long paths to last 60 characters if needed
	if len(p) > 60 {
		return "..." + p[len(p)-60:]
	}
	return p
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
