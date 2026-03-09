//go:build extra
// +build extra

package extra

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os/exec"
	"strings"
	"sync"
)

type WhisperServer struct {
	logger      *slog.Logger
	ServerURL   string
	SampleRate  int
	AudioBuffer *bytes.Buffer
	recording   bool          // protected by mu
	mu          sync.Mutex    // protects recording & AudioBuffer
	cmd         *exec.Cmd     // protected by cmdMu
	stopCh      chan struct{} // protected by cmdMu
	cmdMu       sync.Mutex    // protects cmd and stopCh
}

func (stt *WhisperServer) StartRecording() error {
	stt.mu.Lock()
	defer stt.mu.Unlock()
	if stt.recording {
		return nil
	}
	// Build ffmpeg command for microphone capture
	args := []string{
		"-f", "alsa",
		"-i", "default",
		"-acodec", "pcm_s16le",
		"-ar", fmt.Sprint(stt.SampleRate),
		"-ac", "1",
		"-f", "s16le",
		"-",
	}
	cmd := exec.Command("ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stt.cmdMu.Lock()
	stt.cmd = cmd
	stt.stopCh = make(chan struct{})
	stt.cmdMu.Unlock()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}
	stt.recording = true
	stt.AudioBuffer.Reset()
	// Read PCM data in goroutine
	go func() {
		buf := make([]byte, 4096)
		for {
			select {
			case <-stt.stopCh:
				return
			default:
				n, err := stdout.Read(buf)
				if n > 0 {
					stt.mu.Lock()
					stt.AudioBuffer.Write(buf[:n])
					stt.mu.Unlock()
				}
				if err != nil {
					if err != io.EOF {
						stt.logger.Error("recording read error", "error", err)
					}
					return
				}
			}
		}
	}()
	return nil
}

func (stt *WhisperServer) StopRecording() (string, error) {
	stt.mu.Lock()
	defer stt.mu.Unlock()
	if !stt.recording {
		return "", errors.New("not recording")
	}
	stt.recording = false
	// Stop ffmpeg
	stt.cmdMu.Lock()
	if stt.cmd != nil && stt.cmd.Process != nil {
		stt.cmd.Process.Kill()
		stt.cmd.Wait()
	}
	close(stt.stopCh)
	stt.cmdMu.Unlock()
	// Rest of StopRecording unchanged (WAV header + HTTP upload)
	// ...
	stt.recording = false
	// wait loop to finish?
	if stt.AudioBuffer == nil {
		err := errors.New("unexpected nil AudioBuffer")
		stt.logger.Error(err.Error())
		return "", err
	}
	// Create WAV header first
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	// Add audio file part
	part, err := writer.CreateFormFile("file", "recording.wav")
	if err != nil {
		stt.logger.Error("fn: StopRecording", "error", err)
		return "", err
	}
	// Stream directly to multipart writer: header + raw data
	dataSize := stt.AudioBuffer.Len()
	stt.writeWavHeader(part, dataSize)
	if _, err := io.Copy(part, stt.AudioBuffer); err != nil {
		stt.logger.Error("fn: StopRecording", "error", err)
		return "", err
	}
	// Reset buffer for next recording
	stt.AudioBuffer.Reset()
	// Add response format field
	err = writer.WriteField("response_format", "text")
	if err != nil {
		stt.logger.Error("fn: StopRecording", "error", err)
		return "", err
	}
	if writer.Close() != nil {
		stt.logger.Error("fn: StopRecording", "error", err)
		return "", err
	}
	// Send request
	resp, err := http.Post(stt.ServerURL, writer.FormDataContentType(), body) //nolint:noctx
	if err != nil {
		stt.logger.Error("fn: StopRecording", "error", err)
		return "", err
	}
	defer resp.Body.Close()
	// Read and print response
	responseTextBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		stt.logger.Error("fn: StopRecording", "error", err)
		return "", err
	}
	resptext := strings.TrimRight(string(responseTextBytes), "\n")
	// in case there are special tokens like [_BEG_]
	resptext = specialRE.ReplaceAllString(resptext, "")
	return strings.TrimSpace(strings.ReplaceAll(resptext, "\n ", "\n")), nil
}
