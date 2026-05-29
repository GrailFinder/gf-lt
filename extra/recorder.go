//go:build extra
// +build extra

package extra

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os/exec"
	"sync"
	"time"
)

type Recorder struct {
	logger     *slog.Logger
	sampleRate int
	buffer     *bytes.Buffer
	recording  bool
	mu         sync.Mutex
	cmd        *exec.Cmd
	stopCh     chan struct{}
	cmdMu      sync.Mutex
	wg         sync.WaitGroup
	// VAD
	onUtterance   func(wav []byte)
	silencePeriod time.Duration
	speaking      bool
	silenceSince  time.Time
	noiseFloor    float64
	nfCount       int
}

func NewRecorder(logger *slog.Logger, sampleRate int) *Recorder {
	return &Recorder{
		logger:        logger,
		sampleRate:    sampleRate,
		buffer:        new(bytes.Buffer),
		silencePeriod: 1000 * time.Millisecond,
	}
}

func (r *Recorder) SetOnUtterance(fn func(wav []byte)) {
	r.onUtterance = fn
}

func (r *Recorder) SetSilencePeriod(d time.Duration) {
	r.silencePeriod = d
}

func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recording {
		return nil
	}
	args := []string{
		"-f", "alsa",
		"-i", "default",
		"-acodec", "pcm_s16le",
		"-ar", fmt.Sprint(r.sampleRate),
		"-ac", "1",
		"-f", "s16le",
		"-",
	}
	cmd := exec.Command("ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	r.cmdMu.Lock()
	r.cmd = cmd
	r.stopCh = make(chan struct{})
	r.cmdMu.Unlock()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}
	r.recording = true
	r.buffer.Reset()
	r.speaking = false
	r.silenceSince = time.Time{}
	r.noiseFloor = 0
	r.nfCount = 0
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		buf := make([]byte, 4096)
		for {
			select {
			case <-r.stopCh:
				return
			default:
				n, err := stdout.Read(buf)
				if n > 0 {
					chunk := make([]byte, n)
					copy(chunk, buf[:n])
					r.mu.Lock()
					r.buffer.Write(chunk)
					r.mu.Unlock()
					r.feedVAD(chunk)
				}
				if err != nil {
					if err != io.EOF {
						r.logger.Error("recording read error", "error", err)
					}
					return
				}
			}
		}
	}()
	return nil
}

func (r *Recorder) Stop() ([]byte, error) {
	r.mu.Lock()
	if !r.recording {
		r.mu.Unlock()
		return nil, errors.New("not recording")
	}
	r.recording = false
	r.cmdMu.Lock()
	if r.cmd != nil && r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}
	close(r.stopCh)
	r.cmdMu.Unlock()
	r.mu.Unlock()
	r.wg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.buffer == nil {
		return nil, errors.New("unexpected nil audio buffer")
	}
	dataSize := r.buffer.Len()
	if dataSize == 0 {
		return nil, errors.New("no audio data captured")
	}
	wav := make([]byte, 44+dataSize)
	writeWavHeader(wav[:44], r.sampleRate, dataSize)
	copy(wav[44:], r.buffer.Bytes())
	r.buffer.Reset()
	return wav, nil
}

func (r *Recorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recording
}

func (r *Recorder) feedVAD(pcm []byte) {
	rms := computeRMS(pcm)
	if r.nfCount < 10 {
		if r.nfCount == 0 {
			r.noiseFloor = rms
		} else {
			r.noiseFloor = r.noiseFloor*0.75 + rms*0.25
		}
		r.nfCount++
		return
	}
	threshold := r.noiseFloor * 2.5
	if threshold < 300 {
		threshold = 300
	}
	now := time.Now()
	if rms > threshold {
		if !r.speaking {
			r.speaking = true
		}
		r.silenceSince = time.Time{}
	} else {
		if r.speaking {
			if r.silenceSince.IsZero() {
				r.silenceSince = now
			} else if now.Sub(r.silenceSince) >= r.silencePeriod {
				r.speaking = false
				r.silenceSince = time.Time{}
				r.emitUtterance()
			}
		}
	}
}

func (r *Recorder) emitUtterance() {
	r.mu.Lock()
	dataSize := r.buffer.Len()
	if dataSize == 0 {
		r.mu.Unlock()
		return
	}
	wav := make([]byte, 44+dataSize)
	writeWavHeader(wav[:44], r.sampleRate, dataSize)
	copy(wav[44:], r.buffer.Bytes())
	r.buffer.Reset()
	r.mu.Unlock()
	if r.onUtterance != nil {
		r.onUtterance(wav)
	}
}

func computeRMS(pcm []byte) float64 {
	var sum int64
	for i := 0; i < len(pcm)-1; i += 2 {
		sample := int64(int16(binary.LittleEndian.Uint16(pcm[i:])))
		sum += sample * sample
	}
	return math.Sqrt(float64(sum) / float64(len(pcm)/2))
}

func writeWavHeader(header []byte, sampleRate, dataSize int) {
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate)*1*(16/8))
	binary.LittleEndian.PutUint16(header[32:34], 1*(16/8))
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))
}
