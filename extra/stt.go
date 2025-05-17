package extra

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"

	"github.com/MarkKremer/microphone/v2"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/wav"
)

type STT interface {
	StartRecording() error
	StopRecording() (string, error)
	IsRecording() bool
}

type WhisperSTT struct {
	logger     *slog.Logger
	ServerURL  string
	SampleRate beep.SampleRate
	Buffer     *bytes.Buffer
	streamer   beep.StreamCloser
	recording  bool
}

type writeseeker struct {
	buf []byte
	pos int
}

func (m *writeseeker) Write(p []byte) (n int, err error) {
	minCap := m.pos + len(p)
	if minCap > cap(m.buf) { // Make sure buf has enough capacity:
		buf2 := make([]byte, len(m.buf), minCap+len(p)) // add some extra
		copy(buf2, m.buf)
		m.buf = buf2
	}
	if minCap > len(m.buf) {
		m.buf = m.buf[:minCap]
	}
	copy(m.buf[m.pos:], p)
	m.pos += len(p)
	return len(p), nil
}

func (m *writeseeker) Seek(offset int64, whence int) (int64, error) {
	newPos, offs := 0, int(offset)
	switch whence {
	case io.SeekStart:
		newPos = offs
	case io.SeekCurrent:
		newPos = m.pos + offs
	case io.SeekEnd:
		newPos = len(m.buf) + offs
	}
	if newPos < 0 {
		return 0, errors.New("negative result pos")
	}
	m.pos = newPos
	return int64(newPos), nil
}

// Reader returns an io.Reader. Use it, for example, with io.Copy, to copy the content of the WriterSeeker buffer to an io.Writer
func (ws *writeseeker) Reader() io.Reader {
	return bytes.NewReader(ws.buf)
}

func NewWhisperSTT(logger *slog.Logger, serverURL string, sampleRate beep.SampleRate) *WhisperSTT {
	return &WhisperSTT{
		logger:     logger,
		ServerURL:  serverURL,
		SampleRate: sampleRate,
		Buffer:     new(bytes.Buffer),
	}
}

func (stt *WhisperSTT) StartRecording() error {
	stream, err := microphoneStream(stt.SampleRate)
	if err != nil {
		return fmt.Errorf("failed to init microphone: %w", err)
	}

	stt.streamer = stream
	stt.recording = true

	go stt.capture()
	return nil
}

func (stt *WhisperSTT) capture() {
	sink := beep.NewBuffer(beep.Format{
		SampleRate:  stt.SampleRate,
		NumChannels: 1,
		Precision:   2,
	})

	// Append the streamer to the buffer and encode as WAV
	sink.Append(stt.streamer)

	// Encode the captured audio to WAV format using beep's WAV encoder
	// var wavBuf bytes.Buffer
	var wavBuf writeseeker
	if err := wav.Encode(&wavBuf, sink.Streamer(0, sink.Len()), beep.Format{
		SampleRate:  stt.SampleRate,
		NumChannels: 1,
		Precision:   2,
	}); err != nil {
		stt.logger.Error("failed to encode WAV", "error", err)
	}
	r := wavBuf.Reader()
	// stt.Buffer = &wavBuf
	if _, err := io.Copy(stt.Buffer, r); err != nil {
		stt.logger.Error("failed to encode WAV", "error", err)
	}
}

func (stt *WhisperSTT) StopRecording() (string, error) {
	if !stt.recording {
		return "", nil
	}

	stt.streamer.Close()
	stt.recording = false

	// Send to Whisper.cpp server
	req, err := http.NewRequest("POST", stt.ServerURL, stt.Buffer)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "audio/wav")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("transcription request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Text, nil
}

func (stt *WhisperSTT) IsRecording() bool {
	return stt.recording
}

func microphoneStream(sr beep.SampleRate) (beep.StreamCloser, error) {
	if err := microphone.Init(); err != nil {
		return nil, fmt.Errorf("microphone init failed: %w", err)
	}

	stream, _, err := microphone.OpenDefaultStream(sr, 1) // 1 channel mono
	if err != nil {
		microphone.Terminate()
		return nil, fmt.Errorf("failed to open microphone: %w", err)
	}

	// Handle OS signals to clean up
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill)
	go func() {
		<-sig
		stream.Stop()
		stream.Close()
		microphone.Terminate()
		os.Exit(1)
	}()

	stream.Start()
	return stream, nil
}
