//go:build extra
// +build extra

package extra

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"gf-lt/config"
)

type whisperServer struct {
	logger    *slog.Logger
	recorder  *Recorder
	serverURL string
	client    *http.Client
}

func newWhisperServer(logger *slog.Logger, cfg *config.Config) *whisperServer {
	sr := cfg.STT_SR
	if sr == 0 {
		sr = 16000
	}
	return &whisperServer{
		logger:    logger,
		recorder:  NewRecorder(logger, sr),
		serverURL: cfg.STT_URL,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *whisperServer) StartRecording() error {
	return s.recorder.Start()
}

func (s *whisperServer) StopRecording() (string, error) {
	wav, err := s.recorder.Stop()
	if err != nil {
		return "", err
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "recording.wav")
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(wav); err != nil {
		return "", fmt.Errorf("write wav: %w", err)
	}
	writer.WriteField("response_format", "text")
	writer.Close()
	resp, err := s.client.Post(s.serverURL, writer.FormDataContentType(), body)
	if err != nil {
		return "", fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()
	text, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	result := strings.TrimRight(string(text), "\n")
	result = specialRE.ReplaceAllString(result, "")
	return strings.TrimSpace(strings.ReplaceAll(result, "\n ", "\n")), nil
}

func (s *whisperServer) IsRecording() bool {
	return s.recorder.IsRecording()
}
