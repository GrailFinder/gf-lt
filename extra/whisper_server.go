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
	"sync"
	"time"

	"gf-lt/config"
)

type whisperServer struct {
	logger      *slog.Logger
	recorder    *Recorder
	serverURL   string
	client      *http.Client
	utteranceCh chan string
	txWg        sync.WaitGroup
	doneCh      chan struct{}
}

func newWhisperServer(logger *slog.Logger, cfg *config.Config) *whisperServer {
	sr := cfg.STT_SR
	if sr == 0 {
		sr = 16000
	}
	w := &whisperServer{
		logger:    logger,
		serverURL: cfg.STT_URL,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
	w.recorder = NewRecorder(logger, sr)
	w.recorder.SetOnUtterance(w.onUtterance)
	silenceMs := cfg.STT_SILENCE_MS
	if silenceMs > 0 {
		w.recorder.SetSilencePeriod(time.Duration(silenceMs) * time.Millisecond)
	}
	return w
}

func (s *whisperServer) onUtterance(wav []byte) {
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

func (s *whisperServer) StartRecording() error {
	s.utteranceCh = make(chan string, 20)
	s.doneCh = make(chan struct{})
	return s.recorder.Start()
}

func (s *whisperServer) StopRecording() (string, error) {
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

func (s *whisperServer) IsRecording() bool {
	return s.recorder.IsRecording()
}

func (s *whisperServer) Utterances() <-chan string {
	return s.utteranceCh
}

func (s *whisperServer) transcribe(wav []byte) (string, error) {
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
