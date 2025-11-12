package extra

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"gf-lt/config"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
	"syscall"

	"github.com/gordonklaus/portaudio"
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

type WhisperServer struct {
	logger      *slog.Logger
	ServerURL   string
	SampleRate  int
	AudioBuffer *bytes.Buffer
	recording   bool
}

func NewWhisperServer(logger *slog.Logger, cfg *config.Config) *WhisperServer {
	return &WhisperServer{
		logger:      logger,
		ServerURL:   cfg.STT_URL,
		SampleRate:  cfg.STT_SR,
		AudioBuffer: new(bytes.Buffer),
	}
}

func (stt *WhisperServer) StartRecording() error {
	if err := stt.microphoneStream(stt.SampleRate); err != nil {
		return fmt.Errorf("failed to init microphone: %w", err)
	}
	stt.recording = true
	return nil
}

func (stt *WhisperServer) StopRecording() (string, error) {
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

func (stt *WhisperServer) microphoneStream(sampleRate int) error {
	// Temporarily redirect stderr to suppress ALSA warnings during PortAudio init
	origStderr, err := syscall.Dup(syscall.Stderr)
	nullFD, err := syscall.Open("/dev/null", syscall.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open /dev/null: %w", err)
	}
	// redirect stderr
	syscall.Dup2(nullFD, syscall.Stderr)
	// Initialize PortAudio (this is where ALSA warnings occur)
	defer func() {
		// Restore stderr
		syscall.Dup2(origStderr, syscall.Stderr)
		syscall.Close(origStderr)
		syscall.Close(nullFD)
	}()
	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("portaudio init failed: %w", err)
	}
	in := make([]int16, 64)
	stream, err := portaudio.OpenDefaultStream(1, 0, float64(sampleRate), len(in), in)
	if err != nil {
		if paErr := portaudio.Terminate(); paErr != nil {
			return fmt.Errorf("failed to open microphone: %w; terminate error: %w", err, paErr)
		}
		return fmt.Errorf("failed to open microphone: %w", err)
	}
	go func(stream *portaudio.Stream) {
		if err := stream.Start(); err != nil {
			stt.logger.Error("microphoneStream", "error", err)
			return
		}
		for {
			if !stt.IsRecording() {
				return
			}
			if err := stream.Read(); err != nil {
				stt.logger.Error("reading stream", "error", err)
				return
			}
			if err := binary.Write(stt.AudioBuffer, binary.LittleEndian, in); err != nil {
				stt.logger.Error("writing to buffer", "error", err)
				return
			}
		}
	}(stream)
	return nil
}
