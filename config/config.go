package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ResolveConfigPath searches for config.toml in CWD, then ~/.config/gf-lt/.
// Returns empty string if not found.
func ResolveConfigPath() string {
	if _, err := os.Stat("config.toml"); err == nil {
		abs, err := filepath.Abs("config.toml")
		if err == nil {
			return abs
		}
		return "config.toml"
	}
	home, err := os.UserHomeDir()
	if err == nil {
		path := filepath.Join(home, ".config", "gf-lt", "config.toml")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// resolvePath expands ~, resolves relative paths against configDir, passes through absolutes.
func resolvePath(p, configDir string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	if filepath.IsAbs(p) {
		return p
	}
	if configDir != "" {
		return filepath.Join(configDir, p)
	}
	return p
}

type MCPServerConfig struct {
	URL string `toml:"url"`
}

type ModelManagementConfig struct {
	VRAMFreeServers []string `toml:"VRAMFreeServers"`
}

type Config struct {
	ChatAPI                       string `toml:"ChatAPI"`
	CompletionAPI                 string `toml:"CompletionAPI"`
	CurrentAPI                    string
	CurrentModel                  string `toml:"CurrentModel"`
	APIMap                        map[string]string
	FetchModelNameAPI             string `toml:"FetchModelNameAPI"`
	ShowSys                       bool   `toml:"ShowSys"`
	LogFile                       string `toml:"LogFile"`
	UserRole                      string `toml:"UserRole"`
	ToolRole                      string `toml:"ToolRole"`
	ToolUse                       bool   `toml:"ToolUse"`
	StripThinkingFromAPI          bool   `toml:"StripThinkingFromAPI"`
	ReasoningEffort               string `toml:"ReasoningEffort"`
	AssistantRole                 string `toml:"AssistantRole"`
	SysDir                        string `toml:"SysDir"`
	ChunkLimit                    uint32 `toml:"ChunkLimit"`
	AutoScrollEnabled             bool   `toml:"AutoScrollEnabled"`
	WriteNextMsgAs                string
	WriteNextMsgAsCompletionAgent string
	SkipLLMResp                   bool
	DBPATH                        string                     `toml:"DBPATH"`
	FilePickerDir                 string                     `toml:"FilePickerDir"`
	FilePickerExts                string                     `toml:"FilePickerExts"`
	FSAllowOutOfRoot              bool                       `toml:"FSAllowOutOfRoot"`
	ImagePreview                  bool                       `toml:"ImagePreview"`
	EnableMouse                   bool                       `toml:"EnableMouse"`
	MCPServers                    map[string]MCPServerConfig `toml:"MCPServers"`
	// embeddings
	EmbedURL           string `toml:"EmbedURL"`
	HFToken            string `toml:"HFToken"`
	EmbedModelPath     string `toml:"EmbedModelPath"`
	EmbedTokenizerPath string `toml:"EmbedTokenizerPath"`
	EmbedDims          int    `toml:"EmbedDims"`
	// rag settings
	RAGDir          string `toml:"RAGDir"`
	RAGBatchSize    int    `toml:"RAGBatchSize"`
	RAGWordLimit    uint32 `toml:"RAGWordLimit"`
	RAGOverlapWords uint32 `toml:"RAGOverlapWords"`
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
	TTS_URL      string  `toml:"TTS_URL"`
	TTS_ENABLED  bool    `toml:"TTS_ENABLED"`
	TTS_SPEED    float32 `toml:"TTS_SPEED"`
	TTS_PROVIDER string  `toml:"TTS_PROVIDER"`
	TTS_LANGUAGE string  `toml:"TTS_LANGUAGE"`
	// STT
	STT_TYPE          string `toml:"STT_TYPE"` // WHISPER_SERVER, WHISPER_BINARY, OPENAI_COMPAT, crips_asr
	STT_URL           string `toml:"STT_URL"`
	STT_SR            int    `toml:"STT_SR"`
	STT_ENABLED       bool   `toml:"STT_ENABLED"`
	WhisperBinaryPath string `toml:"WhisperBinaryPath"`
	WhisperModelPath  string `toml:"WhisperModelPath"`
	STT_LANG          string `toml:"STT_LANG"`
	ASR_MODEL         string `toml:"ASR_MODEL"`
	STT_SILENCE_MS    int    `toml:"STT_SILENCE_MS"`
	// character spefic contetx
	CharSpecificContextEnabled bool   `toml:"CharSpecificContextEnabled"`
	CharSpecificContextTag     string `toml:"CharSpecificContextTag"`
	AutoTurn                   bool   `toml:"AutoTurn"`
	// playwright browser
	PlaywrightEnabled bool `toml:"PlaywrightEnabled"`
	MemoryEnabled     bool `toml:"MemoryEnabled"`
	PlaywrightDebug   bool `toml:"PlaywrightDebug"` // !headless
	// CLI mode
	CLIMode       bool
	UseNotifySend bool
	// Mission mode (auto issue solver)
	MissionMode        bool
	MissionIssueID      string
	MissionAgentCard    string
	MissionResumeFile   string
	ConfigDir           string // directory of the loaded config file (set during LoadConfig)
	ExportDir           string // chat export directory (default: "chat_exports")
	MissionPMInterval   int
	MissionMaxFailures  int
	MissionCheckpointFile string
	OutputFormat        string // "text" or "json" — applies to CLI and mission modes
	MissionOutputFormat string // deprecated, use OutputFormat
	MissionQuiet        bool
	MissionToolsEnabled bool   `toml:"MissionToolsEnabled"`
	IssuesDir          string `toml:"IssuesDir"`
	ModelManagement    *ModelManagementConfig `toml:"ModelManagement"`
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

	// Store config directory for resolving relative paths
	absFn, err := filepath.Abs(fn)
	if err == nil {
		config.ConfigDir = filepath.Dir(absFn)
	}

	// Resolve relative paths against config directory
	config.SysDir = resolvePath(config.SysDir, config.ConfigDir)
	config.DBPATH = resolvePath(config.DBPATH, config.ConfigDir)
	config.LogFile = resolvePath(config.LogFile, config.ConfigDir)
	config.RAGDir = resolvePath(config.RAGDir, config.ConfigDir)
	config.EmbedModelPath = resolvePath(config.EmbedModelPath, config.ConfigDir)
	config.EmbedTokenizerPath = resolvePath(config.EmbedTokenizerPath, config.ConfigDir)
	config.ExportDir = resolvePath(config.ExportDir, config.ConfigDir)
	config.WhisperBinaryPath = resolvePath(config.WhisperBinaryPath, config.ConfigDir)
	config.WhisperModelPath = resolvePath(config.WhisperModelPath, config.ConfigDir)

	// Default FilePickerDir to current working directory if not set
	if config.FilePickerDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			config.FilePickerDir = "."
		} else {
			config.FilePickerDir = cwd
		}
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
	// check env if keys not in config
	if config.OpenRouterToken == "" {
		config.OpenRouterToken = os.Getenv("OPENROUTER_API_KEY")
	}
	if config.DeepSeekToken == "" {
		config.DeepSeekToken = os.Getenv("DEEPSEEK_API_KEY")
	}
	// Build ApiLinks slice with only non-empty API links
	// Only include DeepSeek APIs if DeepSeekToken is provided
	if config.DeepSeekToken != "" {
		if config.DeepSeekChatAPI != "" {
			config.ApiLinks = append(config.ApiLinks, config.DeepSeekChatAPI)
		}
		if config.DeepSeekCompletionAPI != "" {
			config.ApiLinks = append(config.ApiLinks, config.DeepSeekCompletionAPI)
		}
	}
	// Only include OpenRouter APIs if OpenRouterToken is provided
	if config.OpenRouterToken != "" {
		if config.OpenRouterChatAPI != "" {
			config.ApiLinks = append(config.ApiLinks, config.OpenRouterChatAPI)
		}
		if config.OpenRouterCompletionAPI != "" {
			config.ApiLinks = append(config.ApiLinks, config.OpenRouterCompletionAPI)
		}
	}
	// Always include basic APIs
	if config.ChatAPI != "" {
		config.ApiLinks = append(config.ApiLinks, config.ChatAPI)
	}
	if config.CompletionAPI != "" {
		config.ApiLinks = append(config.ApiLinks, config.CompletionAPI)
	}
	if config.RAGDir == "" {
		config.RAGDir = resolvePath("ragimport", config.ConfigDir)
	}
	if config.ExportDir == "" {
		config.ExportDir = resolvePath("chat_exports", config.ConfigDir)
	}
	// Mission mode defaults
	if config.MissionPMInterval == 0 {
		config.MissionPMInterval = 75
	}
	if config.MissionMaxFailures == 0 {
		config.MissionMaxFailures = 3
	}
	if config.OutputFormat == "" {
		config.OutputFormat = config.MissionOutputFormat
	}
	if config.OutputFormat == "" {
		config.OutputFormat = "text"
	}
	config.MissionOutputFormat = config.OutputFormat // sync deprecated field
	if config.IssuesDir == "" {
		if envDir := os.Getenv("GF_LT_ISSUES_DIR"); envDir != "" {
			config.IssuesDir = envDir
		} else {
			config.IssuesDir = resolvePath("./issues", config.ConfigDir)
		}
	}
	// if any value is empty fill with default
	return config, nil
}
