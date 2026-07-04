//go:build extra
// +build extra

package extra

import (
	"gf-lt/config"
	"gf-lt/models"
	"log/slog"
	"os"
	"strings"

	google_translate_tts "github.com/GrailFinder/google-translate-tts"
)

var (
	TTSTextChan  = make(chan string, 10000)
	TTSFlushChan = make(chan bool, 1)
	TTSDoneChan  = make(chan bool, 1)
	// endsWithPunctuation = regexp.MustCompile(`[;.!?]$`)
)

type Orator interface {
	Speak(text string) error
	Stop()
	// pause and resume?
	GetLogger() *slog.Logger
}

func NewOrator(log *slog.Logger, cfg *config.Config) Orator {
	provider := cfg.TTS_PROVIDER
	if provider == "" {
		provider = "google" // does not require local setup
	}
	switch strings.ToLower(provider) {
	case "openai", "kokoro": // OpenAI-compatible TTS
		orator := &OpenAICompatOrator{
			logger: log,
			URL:    cfg.TTS_URL,
			Format: models.AFMP3,
			Speed:  cfg.TTS_SPEED,
			Voice:  cfg.TTS_VOICE,
			Model:  cfg.TTS_MODEL,
		}
		if orator.Model == "" {
			orator.Model = "tts-1"
		}
		orator.tryQuantize()
		go orator.readroutine()
		go orator.stoproutine()
		return orator
	default:
		language := cfg.TTS_LANGUAGE
		if language == "" {
			language = "en"
		}
		speech := &google_translate_tts.Speech{
			Folder:   os.TempDir() + "/gf-lt-tts", // Temporary directory for caching
			Language: language,
			Proxy:    "", // Proxy not supported
			Speed:    cfg.TTS_SPEED,
		}
		orator := &GoogleTranslateOrator{
			logger: log,
			speech: speech,
			Speed:  cfg.TTS_SPEED,
		}
		go orator.readroutine()
		go orator.stoproutine()
		return orator
	}
}
