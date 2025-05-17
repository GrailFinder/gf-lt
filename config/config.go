package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

type Config struct {
	EnableCluedo    bool   `toml:"EnableCluedo"` // Cluedo game mode toggle
	CluedoRole2     string `toml:"CluedoRole2"`  // Secondary AI role name
	ChatAPI         string `toml:"ChatAPI"`
	CompletionAPI   string `toml:"CompletionAPI"`
	CurrentAPI      string
	CurrentProvider string
	APIMap          map[string]string
	//
	ShowSys       bool   `toml:"ShowSys"`
	LogFile       string `toml:"LogFile"`
	UserRole      string `toml:"UserRole"`
	ToolRole      string `toml:"ToolRole"`
	ToolUse       bool   `toml:"ToolUse"`
	ThinkUse      bool   `toml:"ThinkUse"`
	AssistantRole string `toml:"AssistantRole"`
	SysDir        string `toml:"SysDir"`
	ChunkLimit    uint32 `toml:"ChunkLimit"`
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
	// TTS
	TTS_URL     string `toml:"TTS_URL"`
	TTS_ENABLED bool   `toml:"TTS_ENABLED"`
}

func LoadConfigOrDefault(fn string) *Config {
	if fn == "" {
		fn = "config.toml"
	}
	config := &Config{}
	_, err := toml.DecodeFile(fn, &config)
	if err != nil {
		fmt.Println("failed to read config from file, loading default", "error", err)
		config.ChatAPI = "http://localhost:8080/v1/chat/completions"
		config.CompletionAPI = "http://localhost:8080/completion"
		config.DeepSeekCompletionAPI = "https://api.deepseek.com/beta/completions"
		config.DeepSeekChatAPI = "https://api.deepseek.com/chat/completions"
		config.RAGEnabled = false
		config.EmbedURL = "http://localhost:8080/v1/embiddings"
		config.ShowSys = true
		config.LogFile = "log.txt"
		config.UserRole = "user"
		config.ToolRole = "tool"
		config.AssistantRole = "assistant"
		config.SysDir = "sysprompts"
		config.ChunkLimit = 8192
		//
		config.RAGBatchSize = 100
		config.RAGWordLimit = 80
		config.RAGWorkers = 5
		// tts
		config.TTS_ENABLED = false
		config.TTS_URL = "http://localhost:8880/v1/audio/speech"
	}
	config.CurrentAPI = config.ChatAPI
	config.APIMap = map[string]string{
		config.ChatAPI:               config.CompletionAPI,
		config.CompletionAPI:         config.DeepSeekChatAPI,
		config.DeepSeekChatAPI:       config.DeepSeekCompletionAPI,
		config.DeepSeekCompletionAPI: config.ChatAPI,
	}
	for _, el := range []string{config.ChatAPI, config.CompletionAPI, config.DeepSeekChatAPI, config.DeepSeekCompletionAPI} {
		if el != "" {
			config.ApiLinks = append(config.ApiLinks, el)
		}
	}
	// if any value is empty fill with default
	return config
}
