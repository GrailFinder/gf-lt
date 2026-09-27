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
	Errors() <-chan error
}

type StreamCloser interface {
	Close() error
}

func NewSTT(logger *slog.Logger, cfg *config.Config) STT {
	sttType := cfg.STT_TYPE
	switch sttType {
	case "OPENAI_COMPAT", "openai_compat", "crispasr", "crips_asr":
		// Any server exposing POST /v1/audio/transcriptions, e.g. CrispASR in
		// --server mode. "crips_asr" is a long-standing typo, kept working.
		logger.Debug("stt init, chosen OpenAI-compatible backend")
		return newOpenAICompatSTT(logger, cfg)
	default:
		logger.Debug("stt init, defaulting to OpenAI-compatible backend", "type", sttType)
		return newOpenAICompatSTT(logger, cfg)
	}
}
