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
	"sync"

	"github.com/gordonklaus/portaudio"
)



type WhisperBinary struct {
	whisperPath string
	modelPath   string
	lang        string
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	running     bool
	cmd         *exec.Cmd
	audioBuffer []int16
}

func NewWhisperBinary(logger *slog.Logger, cfg *config.Config) *WhisperBinary {
	ctx, cancel := context.WithCancel(context.Background())
	return &WhisperBinary{
		whisperPath: cfg.WhisperBinaryPath,
		modelPath:   cfg.WhisperModelPath,
		lang:        cfg.STT_LANG,
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (w *WhisperBinary) StartRecording() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return errors.New("recording is already in progress")
	}

	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("portaudio init failed: %w", err)
	}

	// Initialize audio buffer
	w.audioBuffer = make([]int16, 0)

	in := make([]int16, 1024) // buffer size
	stream, err := portaudio.OpenDefaultStream(1, 0, 16000.0, len(in), in)
	if err != nil {
		if paErr := portaudio.Terminate(); paErr != nil {
			return fmt.Errorf("failed to open microphone: %w; terminate error: %w", err, paErr)
		}
		return fmt.Errorf("failed to open microphone: %w", err)
	}

	// Create a dummy command just for context management
	w.cmd = exec.CommandContext(w.ctx, "sh", "-c", "echo 'dummy command'")

	go w.recordAudio(stream, in)
	w.running = true

	return nil
}

func (w *WhisperBinary) recordAudio(stream *portaudio.Stream, in []int16) {
	defer func() {
		_ = portaudio.Terminate() // ignoring error as we're shutting down
	}()

	if err := stream.Start(); err != nil {
		return
	}

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
			if !w.running {
				return
			}
			if err := stream.Read(); err != nil {
				return
			}

			// Append samples to buffer
			w.mu.Lock()
			if w.audioBuffer == nil {
				w.audioBuffer = make([]int16, 0)
			}
			// Make a copy of the input buffer to avoid overwriting
			tempBuffer := make([]int16, len(in))
			copy(tempBuffer, in)
			w.audioBuffer = append(w.audioBuffer, tempBuffer...)
			w.mu.Unlock()
		}
	}
}

func (w *WhisperBinary) StopRecording() (string, error) {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return "", errors.New("not currently recording")
	}

	w.running = false
	w.cancel() // This will stop the recording goroutine
	w.mu.Unlock()

	// Save the recorded audio to a temporary file
	tempFile, err := w.saveAudioToTempFile()
	if err != nil {
		return "", fmt.Errorf("failed to save audio to temp file: %w", err)
	}
	defer os.Remove(tempFile) // Clean up the temp file

	// Run the whisper binary
	cmd := exec.CommandContext(w.ctx, w.whisperPath, "-m", w.modelPath, "-l", w.lang, tempFile)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("whisper binary failed: %w, stderr: %s", err, errBuf.String())
	}

	result := outBuf.String()
	
	// Clean up audio buffer
	w.mu.Lock()
	w.audioBuffer = nil
	w.mu.Unlock()
	
	return result, nil
}

// saveAudioToTempFile saves the recorded audio data to a temporary WAV file
func (w *WhisperBinary) saveAudioToTempFile() (string, error) {
	// Create temporary WAV file
	tempFile, err := os.CreateTemp("", "recording_*.wav")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tempFile.Close()

	// Write WAV header and data
	err = w.writeWAVFile(tempFile.Name())
	if err != nil {
		return "", fmt.Errorf("failed to write WAV file: %w", err)
	}

	return tempFile.Name(), nil
}

// writeWAVFile creates a WAV file from the recorded audio data
func (w *WhisperBinary) writeWAVFile(filename string) error {
	// Open file for writing
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	w.mu.Lock()
	audioData := make([]int16, len(w.audioBuffer))
	copy(audioData, w.audioBuffer)
	w.mu.Unlock()

	if len(audioData) == 0 {
		return errors.New("no audio data to write")
	}

	// Calculate data size (number of samples * size of int16)
	dataSize := len(audioData) * 2 // 2 bytes per int16 sample

	// Write WAV header with the correct data size
	header := w.createWAVHeader(16000, 1, 16, dataSize)
	_, err = file.Write(header)
	if err != nil {
		return err
	}

	// Write audio data
	for _, sample := range audioData {
		// Write little-endian 16-bit sample
		_, err := file.Write([]byte{byte(sample), byte(sample >> 8)})
		if err != nil {
			return err
		}
	}

	return nil
}

// createWAVHeader creates a WAV file header
func (w *WhisperBinary) createWAVHeader(sampleRate, channels, bitsPerSample int, dataSize int) []byte {
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	// Total file size will be updated later
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	// fmt chunk size (16 for PCM)
	header[16] = 16
	header[17] = 0
	header[18] = 0
	header[19] = 0
	// Audio format (1 = PCM)
	header[20] = 1
	header[21] = 0
	// Number of channels
	header[22] = byte(channels)
	header[23] = 0
	// Sample rate
	header[24] = byte(sampleRate)
	header[25] = byte(sampleRate >> 8)
	header[26] = byte(sampleRate >> 16)
	header[27] = byte(sampleRate >> 24)
	// Byte rate
	byteRate := sampleRate * channels * bitsPerSample / 8
	header[28] = byte(byteRate)
	header[29] = byte(byteRate >> 8)
	header[30] = byte(byteRate >> 16)
	header[31] = byte(byteRate >> 24)
	// Block align
	blockAlign := channels * bitsPerSample / 8
	header[32] = byte(blockAlign)
	header[33] = 0
	// Bits per sample
	header[34] = byte(bitsPerSample)
	header[35] = 0
	// "data" subchunk
	copy(header[36:40], "data")
	// Data size
	header[40] = byte(dataSize)
	header[41] = byte(dataSize >> 8)
	header[42] = byte(dataSize >> 16)
	header[43] = byte(dataSize >> 24)

	return header
}

func (w *WhisperBinary) IsRecording() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}
