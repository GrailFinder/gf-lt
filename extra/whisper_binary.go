//go:build extra
// +build extra

package extra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"gf-lt/config"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type WhisperBinary struct {
	logger      *slog.Logger
	whisperPath string
	modelPath   string
	lang        string
	// Per-recording fields (protected by mu)
	mu        sync.Mutex
	recording bool
	tempFile  string
	ctx       context.Context
	cancel    context.CancelFunc
	cmd       *exec.Cmd
	cmdMu     sync.Mutex
}

func (w *WhisperBinary) StartRecording() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.recording {
		return errors.New("recording is already in progress")
	}
	// Fresh context for this recording
	ctx, cancel := context.WithCancel(context.Background())
	w.ctx = ctx
	w.cancel = cancel
	// Create temporary file
	tempFile, err := os.CreateTemp("", "recording_*.wav")
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempFile.Close()
	w.tempFile = tempFile.Name()
	// ffmpeg command: capture from default microphone, write WAV
	args := []string{
		"-f", "alsa", // or "pulse" if preferred
		"-i", "default",
		"-acodec", "pcm_s16le",
		"-ar", "16000",
		"-ac", "1",
		"-y", // overwrite output file
		w.tempFile,
	}
	cmd := exec.CommandContext(w.ctx, "ffmpeg", args...)
	// Capture stderr for debugging (optional, but useful for diagnosing)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		os.Remove(w.tempFile)
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				w.logger.Debug("ffmpeg stderr", "output", string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
	}()
	w.cmdMu.Lock()
	w.cmd = cmd
	w.cmdMu.Unlock()
	if err := cmd.Start(); err != nil {
		cancel()
		os.Remove(w.tempFile)
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}
	w.recording = true
	w.logger.Debug("Recording started", "file", w.tempFile)
	return nil
}

func (w *WhisperBinary) StopRecording() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.recording {
		return "", errors.New("not currently recording")
	}
	w.recording = false
	// Gracefully stop ffmpeg
	w.cmdMu.Lock()
	if w.cmd != nil && w.cmd.Process != nil {
		w.logger.Debug("Sending SIGTERM to ffmpeg")
		w.cmd.Process.Signal(syscall.SIGTERM)
		// Wait for process to exit (up to 2 seconds)
		done := make(chan error, 1)
		go func() {
			done <- w.cmd.Wait()
		}()
		select {
		case <-done:
			w.logger.Debug("ffmpeg exited after SIGTERM")
		case <-time.After(2 * time.Second):
			w.logger.Warn("ffmpeg did not exit, sending SIGKILL")
			w.cmd.Process.Kill()
			<-done
		}
	}
	w.cmdMu.Unlock()
	// Cancel context (already done, but for cleanliness)
	if w.cancel != nil {
		w.cancel()
	}
	// Validate temp file
	if w.tempFile == "" {
		return "", errors.New("no recording file")
	}
	defer os.Remove(w.tempFile)
	info, err := os.Stat(w.tempFile)
	if err != nil {
		return "", fmt.Errorf("failed to stat temp file: %w", err)
	}
	if info.Size() < 44 { // WAV header is 44 bytes
		// Log ffmpeg stderr? Already captured in debug logs.
		return "", fmt.Errorf("recording file too small (%d bytes), possibly no audio captured", info.Size())
	}
	// Run whisper.cpp binary
	cmd := exec.Command(w.whisperPath, "-m", w.modelPath, "-l", w.lang, w.tempFile)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		w.logger.Error("whisper binary failed",
			"error", err,
			"stderr", errBuf.String(),
			"file_size", info.Size())
		return "", fmt.Errorf("whisper binary failed: %w (stderr: %s)", err, errBuf.String())
	}
	result := strings.TrimRight(outBuf.String(), "\n")
	result = specialRE.ReplaceAllString(result, "")
	return strings.TrimSpace(strings.ReplaceAll(result, "\n ", "\n")), nil
}

// IsRecording returns true if a recording is in progress.
func (w *WhisperBinary) IsRecording() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.recording
}

func NewWhisperBinary(logger *slog.Logger, cfg *config.Config) *WhisperBinary {
	ctx, cancel := context.WithCancel(context.Background())
	// Set ALSA error handler first
	return &WhisperBinary{
		logger:      logger,
		whisperPath: cfg.WhisperBinaryPath,
		modelPath:   cfg.WhisperModelPath,
		lang:        cfg.STT_LANG,
		ctx:         ctx,
		cancel:      cancel,
	}
}
