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
	"sync"
	"time"

	"gf-lt/config"
)

type openaiSTT struct {
	logger      *slog.Logger
	recorder    *Recorder
	baseURL     string
	model       string
	client      *http.Client
	utteranceCh chan string
	txWg        sync.WaitGroup
	doneCh      chan struct{}
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
	o := &openaiSTT{
		logger:  logger,
		baseURL: strings.TrimRight(cfg.STT_URL, "/"),
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	o.recorder = NewRecorder(logger, sr)
	o.recorder.SetOnUtterance(o.onUtterance)
	silenceMs := cfg.STT_SILENCE_MS
	if silenceMs > 0 {
		o.recorder.SetSilencePeriod(time.Duration(silenceMs) * time.Millisecond)
	}
	return o
}

func (s *openaiSTT) onUtterance(wav []byte) {
	s.txWg.Add(1)
	go func() {
		defer s.txWg.Done()
		text, err := s.transcribe(wav)
		if err != nil {
			s.logger.Error("utterance transcription failed", "error", err)
			return
		}
		if text == "" {
			return
		}
		select {
		case s.utteranceCh <- text:
		case <-s.doneCh:
		}
	}()
}

func (s *openaiSTT) StartRecording() error {
	s.utteranceCh = make(chan string, 20)
	s.doneCh = make(chan struct{})
	return s.recorder.Start()
}

func (s *openaiSTT) StopRecording() (string, error) {
	remainingWav, err := s.recorder.Stop()
	close(s.doneCh)
	s.txWg.Wait()
	if err == nil && len(remainingWav) > 44 {
		text, txErr := s.transcribe(remainingWav)
		if txErr == nil && text != "" {
			s.utteranceCh <- text
		}
	}
	close(s.utteranceCh)
	return "", err
}

func (s *openaiSTT) IsRecording() bool {
	return s.recorder.IsRecording()
}

func (s *openaiSTT) Utterances() <-chan string {
	return s.utteranceCh
}

func (s *openaiSTT) transcribe(wav []byte) (string, error) {
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
