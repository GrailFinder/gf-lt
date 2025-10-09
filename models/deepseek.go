package models

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

func NewDSChatReq(cb ChatBody) DSChatReq {
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

func NewDSCompletionReq(prompt, model string, temp float32, stopSlice []string) DSCompletionReq {
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
		Stop:             stopSlice,
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

type DSBalance struct {
	IsAvailable  bool `json:"is_available"`
	BalanceInfos []struct {
		Currency        string `json:"currency"`
		TotalBalance    string `json:"total_balance"`
		GrantedBalance  string `json:"granted_balance"`
		ToppedUpBalance string `json:"topped_up_balance"`
	} `json:"balance_infos"`
}
