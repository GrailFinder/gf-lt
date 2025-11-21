package config

import (
	"github.com/BurntSushi/toml"
)

type Config struct {
	EnableCluedo      bool   `toml:"EnableCluedo"` // Cluedo game mode toggle
	CluedoRole2       string `toml:"CluedoRole2"`  // Secondary AI role name
	ChatAPI           string `toml:"ChatAPI"`
	CompletionAPI     string `toml:"CompletionAPI"`
	CurrentAPI        string
	CurrentProvider   string
	APIMap            map[string]string
	FetchModelNameAPI string `toml:"FetchModelNameAPI"`
	//  ToolsAPI list?
	SearchAPI      string `toml:"SearchAPI"`
	SearchDescribe string `toml:"SearchDescribe"`
	//
	ShowSys                       bool   `toml:"ShowSys"`
	LogFile                       string `toml:"LogFile"`
	UserRole                      string `toml:"UserRole"`
	ToolRole                      string `toml:"ToolRole"`
	ToolUse                       bool   `toml:"ToolUse"`
	ThinkUse                      bool   `toml:"ThinkUse"`
	AssistantRole                 string `toml:"AssistantRole"`
	SysDir                        string `toml:"SysDir"`
	ChunkLimit                    uint32 `toml:"ChunkLimit"`
	WriteNextMsgAs                string
	WriteNextMsgAsCompletionAgent string
	SkipLLMResp                   bool
	// embeddings
	RAGEnabled bool   `toml:"RAGEnabled"`
	EmbedURL   string `toml:"EmbedURL"`
	HFToken    string `toml:"HFToken"`
	RAGDir     string `toml:"RAGDir"`
	// rag settings
	RAGWorkers   uint32 `toml:"RAGWorkers"`
	RAGBatchSize int    `toml:"RAGBatchSize"`
	RAGWordLimit uint32 `toml:"RAGWordLimit"`
	// deepseek
	DeepSeekChatAPI       string `toml:"DeepSeekChatAPI"`
	DeepSeekCompletionAPI string `toml:"DeepSeekCompletionAPI"`
	DeepSeekToken         string `toml:"DeepSeekToken"`
	DeepSeekModel         string `toml:"DeepSeekModel"`
	ApiLinks              []string
	// openrouter
	OpenRouterChatAPI       string `toml:"OpenRouterChatAPI"`
	OpenRouterCompletionAPI string `toml:"OpenRouterCompletionAPI"`
	OpenRouterToken         string `toml:"OpenRouterToken"`
	OpenRouterModel         string `toml:"OpenRouterModel"`
	// TTS
	TTS_URL     string  `toml:"TTS_URL"`
	TTS_ENABLED bool    `toml:"TTS_ENABLED"`
	TTS_SPEED   float32 `toml:"TTS_SPEED"`
	// STT
	STT_TYPE          string `toml:"STT_TYPE"` // WHISPER_SERVER, WHISPER_BINARY
	STT_URL           string `toml:"STT_URL"`
	STT_SR            int    `toml:"STT_SR"`
	STT_ENABLED       bool   `toml:"STT_ENABLED"`
	WhisperBinaryPath string `toml:"WhisperBinaryPath"`
	WhisperModelPath  string `toml:"WhisperModelPath"`
	STT_LANG          string `toml:"STT_LANG"`
	DBPATH            string `toml:"DBPATH"`
	FilePickerDir     string `toml:"FilePickerDir"`
	FilePickerExts    string `toml:"FilePickerExts"`
}

func LoadConfig(fn string) (*Config, error) {
	if fn == "" {
		fn = "config.toml"
	}
	config := &Config{}
	_, err := toml.DecodeFile(fn, &config)
	if err != nil {
		return nil, err
	}
	config.CurrentAPI = config.ChatAPI
	config.APIMap = map[string]string{
		config.ChatAPI:                 config.CompletionAPI,
		config.CompletionAPI:           config.DeepSeekChatAPI,
		config.DeepSeekChatAPI:         config.DeepSeekCompletionAPI,
		config.DeepSeekCompletionAPI:   config.OpenRouterCompletionAPI,
		config.OpenRouterCompletionAPI: config.OpenRouterChatAPI,
		config.OpenRouterChatAPI:       config.ChatAPI,
	}
	for _, el := range []string{config.ChatAPI, config.CompletionAPI, config.DeepSeekChatAPI, config.DeepSeekCompletionAPI} {
		if el != "" {
			config.ApiLinks = append(config.ApiLinks, el)
		}
	}
	// if any value is empty fill with default
	return config, nil
}
