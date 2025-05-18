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
	"time"

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
	logger     *slog.Logger
	ServerURL  string
	SampleRate int
	RawBuffer  *bytes.Buffer
	WavBuffer  *bytes.Buffer
	streamer   StreamCloser
	recording  bool
}

func NewWhisperSTT(logger *slog.Logger, serverURL string, sampleRate int) *WhisperSTT {
	return &WhisperSTT{
		logger:     logger,
		ServerURL:  serverURL,
		SampleRate: sampleRate,
		RawBuffer:  new(bytes.Buffer),
		WavBuffer:  new(bytes.Buffer),
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
	time.Sleep(time.Millisecond * 200) // this is not the way
	// wait loop to finish?
	if stt.RawBuffer == nil {
		err := errors.New("unexpected nil RawBuffer")
		stt.logger.Error(err.Error())
		return "", err
	}
	// Create WAV header first
	stt.writeWavHeader(stt.WavBuffer, len(stt.RawBuffer.Bytes())) // Write initial header with 0 size
	stt.WavBuffer.Write(stt.RawBuffer.Bytes())
	body := &bytes.Buffer{} // third buffer?
	writer := multipart.NewWriter(body)
	// Add audio file part
	part, err := writer.CreateFormFile("file", "recording.wav")
	if err != nil {
		stt.logger.Error("fn: StopRecording", "error", err)
		return "", err
	}
	_, err = io.Copy(part, stt.WavBuffer)
	if err != nil {
		stt.logger.Error("fn: StopRecording", "error", err)
		return "", err
	}
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
	responseText, err := io.ReadAll(resp.Body)
	if err != nil {
		stt.logger.Error("fn: StopRecording", "error", err)
		return "", err
	}
	stt.logger.Info("got transcript", "text", string(responseText))
	return string(responseText), nil
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
			if err := binary.Write(stt.RawBuffer, binary.LittleEndian, in); err != nil {
				stt.logger.Error("writing to buffer", "error", err)
				return
			}
		}
	}(stream)
	return nil
}
