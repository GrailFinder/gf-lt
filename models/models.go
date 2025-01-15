package models

import (
	"elefant/config"
	"fmt"
	"strings"
)

type FuncCall struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
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

// for streaming
type LLMRespChunk struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Index        int    `json:"index"`
		Delta        struct {
			Content string `json:"content"`
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

type RoleMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (m RoleMsg) ToText(i int, cfg *config.Config) string {
	icon := ""
	switch m.Role {
	case "assistant":
		icon = fmt.Sprintf("(%d) %s", i, cfg.AssistantIcon)
	case "user":
		icon = fmt.Sprintf("(%d) %s", i, cfg.UserIcon)
	case "system":
		icon = fmt.Sprintf("(%d) <system>: ", i)
	case "tool":
		icon = fmt.Sprintf("(%d) %s", i, cfg.ToolIcon)
	default:
		icon = fmt.Sprintf("(%d) <%s>: ", i, m.Role)
	}
	textMsg := fmt.Sprintf("[-:-:b]%s[-:-:-]\n%s\n", icon, m.Content)
	return strings.ReplaceAll(textMsg, "\n\n", "\n")
}

type ChatBody struct {
	Model    string    `json:"model"`
	Stream   bool      `json:"stream"`
	Messages []RoleMsg `json:"messages"`
}

type ChatToolsBody struct {
	Model    string    `json:"model"`
	Messages []RoleMsg `json:"messages"`
	Tools    []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  struct {
				Type       string `json:"type"`
				Properties struct {
					Location struct {
						Type        string `json:"type"`
						Description string `json:"description"`
					} `json:"location"`
					Unit struct {
						Type string   `json:"type"`
						Enum []string `json:"enum"`
					} `json:"unit"`
				} `json:"properties"`
				Required []string `json:"required"`
			} `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
	ToolChoice string `json:"tool_choice"`
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

type LLMModels struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int    `json:"created"`
		OwnedBy string `json:"owned_by"`
		Meta    struct {
			VocabType int   `json:"vocab_type"`
			NVocab    int   `json:"n_vocab"`
			NCtxTrain int   `json:"n_ctx_train"`
			NEmbd     int   `json:"n_embd"`
			NParams   int64 `json:"n_params"`
			Size      int64 `json:"size"`
		} `json:"meta"`
	} `json:"data"`
}
