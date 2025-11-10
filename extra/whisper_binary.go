package extra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"gf-lt/config"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
)

type WhisperBinary struct {
	logger      *slog.Logger
	whisperPath string
	modelPath   string
	lang        string
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	recording   bool
	audioBuffer []int16
}

func NewWhisperBinary(logger *slog.Logger, cfg *config.Config) *WhisperBinary {
	ctx, cancel := context.WithCancel(context.Background())
	return &WhisperBinary{
		logger:      logger,
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
	if w.recording {
		return errors.New("recording is already in progress")
	}

	// Suppress ALSA warnings by setting environment variables
	origCard := os.Getenv("ALSA_PCM_CARD")
	origDevice := os.Getenv("ALSA_PCM_DEVICE")
	origSubdevice := os.Getenv("ALSA_PCM_SUBDEVICE")

	// Set specific ALSA device to prevent "Unknown PCM card.pcm.rear" warnings
	os.Setenv("ALSA_PCM_CARD", "0")
	os.Setenv("ALSA_PCM_DEVICE", "0")
	os.Setenv("ALSA_PCM_SUBDEVICE", "0")

	if err := portaudio.Initialize(); err != nil {
		// Restore original environment variables on error
		if origCard != "" {
			os.Setenv("ALSA_PCM_CARD", origCard)
		} else {
			os.Unsetenv("ALSA_PCM_CARD")
		}
		if origDevice != "" {
			os.Setenv("ALSA_PCM_DEVICE", origDevice)
		} else {
			os.Unsetenv("ALSA_PCM_DEVICE")
		}
		if origSubdevice != "" {
			os.Setenv("ALSA_PCM_SUBDEVICE", origSubdevice)
		} else {
			os.Unsetenv("ALSA_PCM_SUBDEVICE")
		}
		return fmt.Errorf("portaudio init failed: %w", err)
	}

	// Restore original environment variables after initialization
	if origCard != "" {
		os.Setenv("ALSA_PCM_CARD", origCard)
	} else {
		os.Unsetenv("ALSA_PCM_CARD")
	}
	if origDevice != "" {
		os.Setenv("ALSA_PCM_DEVICE", origDevice)
	} else {
		os.Unsetenv("ALSA_PCM_DEVICE")
	}
	if origSubdevice != "" {
		os.Setenv("ALSA_PCM_SUBDEVICE", origSubdevice)
	} else {
		os.Unsetenv("ALSA_PCM_SUBDEVICE")
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

	go w.recordAudio(stream, in)
	w.recording = true
	w.logger.Debug("Recording started")
	return nil
}

func (w *WhisperBinary) recordAudio(stream *portaudio.Stream, in []int16) {
	defer func() {
		w.logger.Debug("recordAudio defer function called")
		_ = stream.Stop()         // Stop the stream
		_ = portaudio.Terminate() // ignoring error as we're shutting down
		w.logger.Debug("recordAudio terminated")
	}()
	w.logger.Debug("Starting audio stream")
	if err := stream.Start(); err != nil {
		w.logger.Error("Failed to start audio stream", "error", err)
		return
	}
	w.logger.Debug("Audio stream started, entering recording loop")
	for {
		select {
		case <-w.ctx.Done():
			w.logger.Debug("Context done, exiting recording loop")
			return
		default:
			// Check recording status with minimal lock time
			w.mu.Lock()
			recording := w.recording
			w.mu.Unlock()

			if !recording {
				w.logger.Debug("Recording flag is false, exiting recording loop")
				return
			}
			if err := stream.Read(); err != nil {
				w.logger.Error("Error reading from stream", "error", err)
				return
			}
			// Append samples to buffer - only acquire lock when necessary
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
	w.logger.Debug("StopRecording called")
	w.mu.Lock()
	if !w.recording {
		w.mu.Unlock()
		return "", errors.New("not currently recording")
	}
	w.logger.Debug("Setting recording to false and cancelling context")
	w.recording = false
	w.cancel() // This will stop the recording goroutine
	w.mu.Unlock()

	// Small delay to allow the recording goroutine to react to context cancellation
	time.Sleep(100 * time.Millisecond)

	// Save the recorded audio to a temporary file
	tempFile, err := w.saveAudioToTempFile()
	if err != nil {
		w.logger.Error("Error saving audio to temp file", "error", err)
		return "", fmt.Errorf("failed to save audio to temp file: %w", err)
	}
	w.logger.Debug("Saved audio to temp file", "file", tempFile)

	// Run the whisper binary with a separate context to avoid cancellation during transcription
	cmd := exec.Command(w.whisperPath, "-m", w.modelPath, "-l", w.lang, tempFile, "2>/dev/null")
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	// Redirect stderr to suppress ALSA warnings and other stderr output
	cmd.Stderr = io.Discard // Suppress stderr output from whisper binary

	w.logger.Debug("Running whisper binary command")
	if err := cmd.Run(); err != nil {
		// Clean up audio buffer
		w.mu.Lock()
		w.audioBuffer = nil
		w.mu.Unlock()
		// Since we're suppressing stderr, we'll just log that the command failed
		w.logger.Error("Error running whisper binary", "error", err)
		return "", fmt.Errorf("whisper binary failed: %w", err)
	}
	result := outBuf.String()
	w.logger.Debug("Whisper binary completed", "result", result)

	// Clean up audio buffer
	w.mu.Lock()
	w.audioBuffer = nil
	w.mu.Unlock()

	// Clean up the temporary file after transcription
	w.logger.Debug("StopRecording completed")
	os.Remove(tempFile)

	return result, nil
}

// saveAudioToTempFile saves the recorded audio data to a temporary WAV file
func (w *WhisperBinary) saveAudioToTempFile() (string, error) {
	w.logger.Debug("saveAudioToTempFile called")
	// Create temporary WAV file
	tempFile, err := os.CreateTemp("", "recording_*.wav")
	if err != nil {
		w.logger.Error("Failed to create temp file", "error", err)
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	w.logger.Debug("Created temp file", "file", tempFile.Name())
	defer tempFile.Close()

	// Write WAV header and data
	w.logger.Debug("About to write WAV file", "file", tempFile.Name())
	err = w.writeWAVFile(tempFile.Name())
	if err != nil {
		w.logger.Error("Error writing WAV file", "error", err)
		return "", fmt.Errorf("failed to write WAV file: %w", err)
	}
	w.logger.Debug("WAV file written successfully", "file", tempFile.Name())

	return tempFile.Name(), nil
}

// writeWAVFile creates a WAV file from the recorded audio data
func (w *WhisperBinary) writeWAVFile(filename string) error {
	w.logger.Debug("writeWAVFile called", "filename", filename)
	// Open file for writing
	file, err := os.Create(filename)
	if err != nil {
		w.logger.Error("Error creating file", "error", err)
		return err
	}
	defer file.Close()

	w.logger.Debug("About to acquire mutex in writeWAVFile")
	w.mu.Lock()
	w.logger.Debug("Locked mutex, copying audio buffer")
	audioData := make([]int16, len(w.audioBuffer))
	copy(audioData, w.audioBuffer)
	w.mu.Unlock()
	w.logger.Debug("Unlocked mutex", "audio_data_length", len(audioData))

	if len(audioData) == 0 {
		w.logger.Warn("No audio data to write")
		return errors.New("no audio data to write")
	}

	// Calculate data size (number of samples * size of int16)
	dataSize := len(audioData) * 2 // 2 bytes per int16 sample
	w.logger.Debug("Calculated data size", "size", dataSize)

	// Write WAV header with the correct data size
	header := w.createWAVHeader(16000, 1, 16, dataSize)
	_, err = file.Write(header)
	if err != nil {
		w.logger.Error("Error writing WAV header", "error", err)
		return err
	}
	w.logger.Debug("WAV header written successfully")

	// Write audio data
	w.logger.Debug("About to write audio data samples")
	for i, sample := range audioData {
		// Write little-endian 16-bit sample
		_, err := file.Write([]byte{byte(sample), byte(sample >> 8)})
		if err != nil {
			w.logger.Error("Error writing sample", "index", i, "error", err)
			return err
		}
		// Log progress every 10000 samples to avoid too much output
		if i%10000 == 0 {
			w.logger.Debug("Written samples", "count", i)
		}
	}
	w.logger.Debug("All audio data written successfully")

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
	return w.recording
}
