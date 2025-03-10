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
	icon := fmt.Sprintf("(%d)", i)
	// check if already has role annotation (/completion makes them)
	if !strings.HasPrefix(m.Content, m.Role+":") {
		icon = fmt.Sprintf("(%d) <%s>: ", i, m.Role)
	}
	textMsg := fmt.Sprintf("[-:-:b]%s[-:-:-]\n%s\n", icon, m.Content)
	return strings.ReplaceAll(textMsg, "\n\n", "\n")
}

func (m RoleMsg) ToPrompt() string {
	return strings.ReplaceAll(fmt.Sprintf("%s:\n%s", m.Role, m.Content), "\n\n", "\n")
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

type DSChatReq struct {
	Messages         []RoleMsg `json:"messages"`
	Model            string    `json:"model"`
	Stream           bool      `json:"stream"`
	FrequencyPenalty int       `json:"frequency_penalty"`
	MaxTokens        int       `json:"max_tokens"`
	PresencePenalty  int       `json:"presence_penalty"`
	Temperature      float32   `json:"temperature"`
	TopP             float32   `json:"top_p"`
	// ResponseFormat   struct {
	// 	Type string `json:"type"`
	// } `json:"response_format"`
	// Stop          any    `json:"stop"`
	// StreamOptions any     `json:"stream_options"`
	// Tools         any     `json:"tools"`
	// ToolChoice    string  `json:"tool_choice"`
	// Logprobs      bool    `json:"logprobs"`
	// TopLogprobs   any     `json:"top_logprobs"`
}

func NewDSCharReq(cb ChatBody) DSChatReq {
	return DSChatReq{
		Messages:         cb.Messages,
		Model:            cb.Model,
		Stream:           cb.Stream,
		MaxTokens:        2048,
		PresencePenalty:  0,
		FrequencyPenalty: 0,
		Temperature:      1.0,
		TopP:             1.0,
	}
}

type DSCompletionReq struct {
	Model            string `json:"model"`
	Prompt           string `json:"prompt"`
	Echo             bool   `json:"echo"`
	FrequencyPenalty int    `json:"frequency_penalty"`
	// Logprobs         int     `json:"logprobs"`
	MaxTokens       int     `json:"max_tokens"`
	PresencePenalty int     `json:"presence_penalty"`
	Stop            any     `json:"stop"`
	Stream          bool    `json:"stream"`
	StreamOptions   any     `json:"stream_options"`
	Suffix          any     `json:"suffix"`
	Temperature     float32 `json:"temperature"`
	TopP            float32 `json:"top_p"`
}

func NewDSCompletionReq(prompt, model string, temp float32, cfg *config.Config) DSCompletionReq {
	return DSCompletionReq{
		Model:            model,
		Prompt:           prompt,
		Temperature:      temp,
		Stream:           true,
		Echo:             false,
		MaxTokens:        2048,
		PresencePenalty:  0,
		FrequencyPenalty: 0,
		TopP:             1.0,
		Stop: []string{
			cfg.UserRole + ":\n", "<|im_end|>",
			cfg.ToolRole + ":\n",
			cfg.AssistantRole + ":\n",
		},
	}
}

type DSCompletionResp struct {
	ID      string `json:"id"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Index        int    `json:"index"`
		Logprobs     struct {
			TextOffset    []int    `json:"text_offset"`
			TokenLogprobs []int    `json:"token_logprobs"`
			Tokens        []string `json:"tokens"`
			TopLogprobs   []struct {
			} `json:"top_logprobs"`
		} `json:"logprobs"`
		Text string `json:"text"`
	} `json:"choices"`
	Created           int    `json:"created"`
	Model             string `json:"model"`
	SystemFingerprint string `json:"system_fingerprint"`
	Object            string `json:"object"`
	Usage             struct {
		CompletionTokens        int `json:"completion_tokens"`
		PromptTokens            int `json:"prompt_tokens"`
		PromptCacheHitTokens    int `json:"prompt_cache_hit_tokens"`
		PromptCacheMissTokens   int `json:"prompt_cache_miss_tokens"`
		TotalTokens             int `json:"total_tokens"`
		CompletionTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

type DSChatResp struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
			Role    any    `json:"role"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
		Index        int    `json:"index"`
		Logprobs     any    `json:"logprobs"`
	} `json:"choices"`
	Created           int    `json:"created"`
	ID                string `json:"id"`
	Model             string `json:"model"`
	Object            string `json:"object"`
	SystemFingerprint string `json:"system_fingerprint"`
	Usage             struct {
		CompletionTokens int `json:"completion_tokens"`
		PromptTokens     int `json:"prompt_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type DSChatStreamResp struct {
	ID                string `json:"id"`
	Object            string `json:"object"`
	Created           int    `json:"created"`
	Model             string `json:"model"`
	SystemFingerprint string `json:"system_fingerprint"`
	Choices           []struct {
		Index int `json:"index"`
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
		Logprobs     any    `json:"logprobs"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
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

type LlamaCPPReq struct {
	Stream bool `json:"stream"`
	// Messages      []RoleMsg `json:"messages"`
	Prompt        string   `json:"prompt"`
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

func NewLCPReq(prompt string, cfg *config.Config, props map[string]float32) LlamaCPPReq {
	return LlamaCPPReq{
		Stream: true,
		Prompt: prompt,
		// Temperature:   0.8,
		// DryMultiplier: 0.5,
		Temperature:   props["temperature"],
		DryMultiplier: props["dry_multiplier"],
		MinP:          props["min_p"],
		NPredict:      int32(props["n_predict"]),
		Stop: []string{
			cfg.UserRole + ":\n", "<|im_end|>",
			cfg.ToolRole + ":\n",
			cfg.AssistantRole + ":\n",
		},
	}
}

type LlamaCPPResp struct {
	Content string `json:"content"`
	Stop    bool   `json:"stop"`
}

type DSBalance struct {
	IsAvailable  bool `json:"is_available"`
	BalanceInfos []struct {
		Currency        string `json:"currency"`
		TotalBalance    string `json:"total_balance"`
		GrantedBalance  string `json:"granted_balance"`
		ToppedUpBalance string `json:"topped_up_balance"`
	} `json:"balance_infos"`
}
