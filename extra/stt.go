package extra

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gordonklaus/portaudio"
)

type STT interface {
	StartRecording() error
	StopRecording() (string, error)
	IsRecording() bool
}

type StreamCloser interface {
	Close() error
}

type WhisperSTT struct {
	logger      *slog.Logger
	ServerURL   string
	SampleRate  int
	AudioBuffer *bytes.Buffer
	streamer    StreamCloser
	recording   bool
}

func NewWhisperSTT(logger *slog.Logger, serverURL string, sampleRate int) *WhisperSTT {
	return &WhisperSTT{
		logger:      logger,
		ServerURL:   serverURL,
		SampleRate:  sampleRate,
		AudioBuffer: new(bytes.Buffer),
	}
}

func (stt *WhisperSTT) StartRecording() error {
	if err := stt.microphoneStream(stt.SampleRate); err != nil {
		return fmt.Errorf("failed to init microphone: %w", err)
	}
	stt.recording = true
	return nil
}

func (stt *WhisperSTT) StopRecording() (string, error) {
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
	resp, err := http.Post("http://localhost:8081/inference", writer.FormDataContentType(), body)
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
	return strings.TrimRight(string(responseTextBytes), "\n"), nil
}

func (stt *WhisperSTT) writeWavHeader(w io.Writer, dataSize int) {
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
	w.Write(header)
}

func (stt *WhisperSTT) IsRecording() bool {
	return stt.recording
}

func (stt *WhisperSTT) microphoneStream(sampleRate int) error {
	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("portaudio init failed: %w", err)
	}
	in := make([]int16, 64)
	stream, err := portaudio.OpenDefaultStream(1, 0, float64(sampleRate), len(in), in)
	if err != nil {
		portaudio.Terminate()
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
