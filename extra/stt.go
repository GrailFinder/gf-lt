//go:build extra
// +build extra

package extra

import (
	"gf-lt/config"
	"log/slog"
	"regexp"
)

var specialRE = regexp.MustCompile(`\[.*?\]`)

type STT interface {
	StartRecording() error
	StopRecording() (string, error)
	IsRecording() bool
	Utterances() <-chan string
}

type StreamCloser interface {
	Close() error
}

func NewSTT(logger *slog.Logger, cfg *config.Config) STT {
	sttType := cfg.STT_TYPE
	switch sttType {
	case "WHISPER_BINARY", "whisper_binary":
		logger.Debug("stt init, chosen whisper binary")
		return NewWhisperBinary(logger, cfg)
	case "WHISPER_SERVER", "whisper_server":
		logger.Debug("stt init, chosen whisper server")
		return newWhisperServer(logger, cfg)
	case "OPENAI_COMPAT", "openai_compat", "crips_asr":
		logger.Debug("stt init, chosen OpenAI-compatible backend")
		return newOpenAICompatSTT(logger, cfg)
	default:
		logger.Debug("stt init, defaulting to OpenAI-compatible backend", "type", sttType)
		return newOpenAICompatSTT(logger, cfg)
	}
}
