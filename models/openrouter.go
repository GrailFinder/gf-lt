package models

// openrouter
// https://openrouter.ai/docs/api-reference/completion
type OpenRouterCompletionReq struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	Stream      bool     `json:"stream"`
	Temperature float32  `json:"temperature"`
	Stop        []string `json:"stop"` // not present in docs
	MinP        float32  `json:"min_p"`
	NPredict    int32    `json:"max_tokens"`
}

func NewOpenRouterCompletionReq(model, prompt string, props map[string]float32, stopStrings []string) OpenRouterCompletionReq {
	return OpenRouterCompletionReq{
		Stream:      true,
		Prompt:      prompt,
		Temperature: props["temperature"],
		MinP:        props["min_p"],
		NPredict:    int32(props["n_predict"]),
		Stop:        stopStrings,
		Model:       model,
	}
}

type OpenRouterChatReq struct {
	Messages    []RoleMsg `json:"messages"`
	Model       string    `json:"model"`
	Stream      bool      `json:"stream"`
	Temperature float32   `json:"temperature"`
	MinP        float32   `json:"min_p"`
	NPredict    int32     `json:"max_tokens"`
	Tools       []Tool    `json:"tools"`
}

func NewOpenRouterChatReq(cb ChatBody, props map[string]float32) OpenRouterChatReq {
	return OpenRouterChatReq{
		Messages:    cb.Messages,
		Model:       cb.Model,
		Stream:      cb.Stream,
		Temperature: props["temperature"],
		MinP:        props["min_p"],
		NPredict:    int32(props["n_predict"]),
	}
}

type OpenRouterChatRespNonStream struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Object   string `json:"object"`
	Created  int    `json:"created"`
	Choices  []struct {
		Logprobs           any    `json:"logprobs"`
		FinishReason       string `json:"finish_reason"`
		NativeFinishReason string `json:"native_finish_reason"`
		Index              int    `json:"index"`
		Message            struct {
			Role      string          `json:"role"`
			Content   string          `json:"content"`
			Refusal   any             `json:"refusal"`
			Reasoning any             `json:"reasoning"`
			ToolCalls []ToolDeltaResp `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type OpenRouterChatResp struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Object   string `json:"object"`
	Created  int    `json:"created"`
	Choices  []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string          `json:"role"`
			Content   string          `json:"content"`
			ToolCalls []ToolDeltaResp `json:"tool_calls"`
		} `json:"delta"`
		FinishReason       string `json:"finish_reason"`
		NativeFinishReason string `json:"native_finish_reason"`
		Logprobs           any    `json:"logprobs"`
	} `json:"choices"`
}

type OpenRouterCompletionResp struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Object   string `json:"object"`
	Created  int    `json:"created"`
	Choices  []struct {
		Text               string `json:"text"`
		FinishReason       string `json:"finish_reason"`
		NativeFinishReason string `json:"native_finish_reason"`
		Logprobs           any    `json:"logprobs"`
	} `json:"choices"`
}

type ORModel struct {
	ID            string `json:"id"`
	CanonicalSlug string `json:"canonical_slug"`
	HuggingFaceID string `json:"hugging_face_id"`
	Name          string `json:"name"`
	Created       int    `json:"created"`
	Description   string `json:"description"`
	ContextLength int    `json:"context_length"`
	Architecture  struct {
		Modality         string   `json:"modality"`
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
		Tokenizer        string   `json:"tokenizer"`
		InstructType     any      `json:"instruct_type"`
	} `json:"architecture"`
	Pricing struct {
		Prompt            string `json:"prompt"`
		Completion        string `json:"completion"`
		Request           string `json:"request"`
		Image             string `json:"image"`
		Audio             string `json:"audio"`
		WebSearch         string `json:"web_search"`
		InternalReasoning string `json:"internal_reasoning"`
	} `json:"pricing,omitempty"`
	TopProvider struct {
		ContextLength       int  `json:"context_length"`
		MaxCompletionTokens int  `json:"max_completion_tokens"`
		IsModerated         bool `json:"is_moderated"`
	} `json:"top_provider"`
	PerRequestLimits    any      `json:"per_request_limits"`
	SupportedParameters []string `json:"supported_parameters"`
}

type ORModels struct {
	Data []ORModel `json:"data"`
}

func (orm *ORModels) ListModels(free bool) []string {
	resp := []string{}
	for _, model := range orm.Data {
		if free {
			if model.Pricing.Prompt == "0" && model.Pricing.Request == "0" &&
				model.Pricing.Completion == "0" {
				resp = append(resp, model.ID)
			}
		} else {
			resp = append(resp, model.ID)
		}
	}
	return resp
}
