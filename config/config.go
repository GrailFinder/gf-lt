package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

type Config struct {
	APIURL        string `toml:"APIURL"`
	ShowSys       bool   `toml:"ShowSys"`
	LogFile       string `toml:"LogFile"`
	UserRole      string `toml:"UserRole"`
	ToolRole      string `toml:"ToolRole"`
	AssistantRole string `toml:"AssistantRole"`
	AssistantIcon string `toml:"AssistantIcon"`
	UserIcon      string `toml:"UserIcon"`
	ToolIcon      string `toml:"ToolIcon"`
	SysDir        string `toml:"SysDir"`
	ChunkLimit    uint32 `toml:"ChunkLimit"`
	// embeddings
	RAGEnabled bool   `toml:"RAGEnabled"`
	EmbedURL   string `toml:"EmbedURL"`
	HFToken    string `toml:"HFToken"`
	RAGDir     string `toml:"RAGDir"`
}

func LoadConfigOrDefault(fn string) *Config {
	if fn == "" {
		fn = "config.toml"
	}
	config := &Config{}
	_, err := toml.DecodeFile(fn, &config)
	if err != nil {
		fmt.Println("failed to read config from file, loading default")
		config.APIURL = "http://localhost:8080/v1/chat/completions"
		config.RAGEnabled = false
		config.EmbedURL = "http://localhost:8080/v1/embiddings"
		config.ShowSys = true
		config.LogFile = "log.txt"
		config.UserRole = "user"
		config.ToolRole = "tool"
		config.AssistantRole = "assistant"
		config.AssistantIcon = "<assistant>: "
		config.UserIcon = "<user>: "
		config.UserIcon = "<tool>: "
		config.SysDir = "sysprompts"
		config.ChunkLimit = 8192
	}
	// if any value is empty fill with default
	return config
}
