//go:build extra
// +build extra

package extra

import (
	"bytes"
	"encoding/binary"
	"gf-lt/config"
	"io"
	"log/slog"
	"regexp"
)

var specialRE = regexp.MustCompile(`\[.*?\]`)

type STT interface {
	StartRecording() error
	StopRecording() (string, error)
	IsRecording() bool
}

type StreamCloser interface {
	Close() error
}

func NewSTT(logger *slog.Logger, cfg *config.Config) STT {
	switch cfg.STT_TYPE {
	case "WHISPER_BINARY":
		logger.Debug("stt init, chosen whisper binary")
		return NewWhisperBinary(logger, cfg)
	case "WHISPER_SERVER":
		logger.Debug("stt init, chosen whisper server")
		return NewWhisperServer(logger, cfg)
	}
	return NewWhisperServer(logger, cfg)
}

func NewWhisperServer(logger *slog.Logger, cfg *config.Config) *WhisperServer {
	return &WhisperServer{
		logger:      logger,
		ServerURL:   cfg.STT_URL,
		SampleRate:  cfg.STT_SR,
		AudioBuffer: new(bytes.Buffer),
	}
}

func (stt *WhisperServer) writeWavHeader(w io.Writer, dataSize int) {
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(stt.SampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(stt.SampleRate)*1*(16/8))
	binary.LittleEndian.PutUint16(header[32:34], 1*(16/8))
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))
	if _, err := w.Write(header); err != nil {
		stt.logger.Error("writeWavHeader", "error", err)
	}
}

func (stt *WhisperServer) IsRecording() bool {
	return stt.recording
}
