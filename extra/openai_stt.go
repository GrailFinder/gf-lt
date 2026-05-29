//go:build extra
// +build extra

package extra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"gf-lt/config"
)

type openaiSTT struct {
	logger   *slog.Logger
	recorder *Recorder
	baseURL  string
	model    string
	client   *http.Client
}

func newOpenAICompatSTT(logger *slog.Logger, cfg *config.Config) *openaiSTT {
	sr := cfg.STT_SR
	if sr == 0 {
		sr = 16000
	}
	model := cfg.ASR_MODEL
	if model == "" {
		model = "whisper-1"
	}
	return &openaiSTT{
		logger:   logger,
		recorder: NewRecorder(logger, sr),
		baseURL:  strings.TrimRight(cfg.STT_URL, "/"),
		model:    model,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *openaiSTT) StartRecording() error {
	return s.recorder.Start()
}

func (s *openaiSTT) StopRecording() (string, error) {
	wav, err := s.recorder.Stop()
	if err != nil {
		return "", err
	}
	url := s.baseURL + "/v1/audio/transcriptions"
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "recording.wav")
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(wav); err != nil {
		return "", fmt.Errorf("write wav: %w", err)
	}
	writer.WriteField("model", s.model)
	writer.WriteField("response_format", "json")
	writer.Close()
	resp, err := s.client.Post(url, writer.FormDataContentType(), body)
	if err != nil {
		return "", fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(respBody))
	}
	var transcription struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &transcription); err != nil {
		text := strings.TrimRight(string(respBody), "\n")
		text = specialRE.ReplaceAllString(text, "")
		return strings.TrimSpace(strings.ReplaceAll(text, "\n ", "\n")), nil
	}
	text := strings.TrimRight(transcription.Text, "\n")
	text = specialRE.ReplaceAllString(text, "")
	return strings.TrimSpace(strings.ReplaceAll(text, "\n ", "\n")), nil
}

func (s *openaiSTT) IsRecording() bool {
	return s.recorder.IsRecording()
}
