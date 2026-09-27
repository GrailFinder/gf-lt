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

// resolveFormat picks the response_format to request. audio.cpp only encodes
// WAV, so that is the default rather than the historical mp3; an explicit
// TTS_FORMAT still wins for other OpenAI-compatible servers.
func resolveFormat(cfg *config.Config) models.AudioFormat {
	if f := strings.ToLower(strings.TrimSpace(cfg.TTS_FORMAT)); f != "" {
		return models.AudioFormat(f)
	}
	return models.AFWav
}

func NewOrator(log *slog.Logger, cfg *config.Config) Orator {
	provider := cfg.TTS_PROVIDER
	if provider == "" {
		provider = "google" // does not require local setup
	}
	switch strings.ToLower(provider) {
	case "openai", "kokoro", "audiocpp": // OpenAI-compatible TTS
		orator := &OpenAICompatOrator{
			logger:   log,
			URL:      cfg.TTS_URL,
			Format:   resolveFormat(cfg),
			Speed:    cfg.TTS_SPEED,
			Voice:    cfg.TTS_VOICE,
			Model:    cfg.TTS_MODEL,
			VoiceRef: cfg.TTS_VOICE_REF,
			RefText:  cfg.TTS_REFERENCE_TEXT,
		}
		if orator.Model == "" {
			orator.Model = "tts-1"
		}
		// audio.cpp names its models explicitly (e.g. "omnivoice"); the OpenAI
		// default would 404 on unknown model id.
		if orator.VoiceRef != "" && orator.RefText == "" {
			log.Warn("TTS_VOICE_REF is set without TTS_REFERENCE_TEXT; OmniVoice rejects clones with no reference transcript")
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
