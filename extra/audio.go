package extra

import (
	"bytes"
	"elefant/config"
	"elefant/models"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/neurosnap/sentences/english"
)

var (
	TTSTextChan  = make(chan string, 10000)
	TTSFlushChan = make(chan bool, 1)
	TTSDoneChan  = make(chan bool, 1)
)

type Orator interface {
	Speak(text string) error
	Stop()
	// pause and resume?
	GetSBuilder() strings.Builder
	GetLogger() *slog.Logger
}

// impl https://github.com/remsky/Kokoro-FastAPI
type KokoroOrator struct {
	logger        *slog.Logger
	URL           string
	Format        models.AudioFormat
	Stream        bool
	Speed         float32
	Language      string
	Voice         string
	currentStream *beep.Ctrl // Added for playback control
	textBuffer    strings.Builder
}

func stoproutine(orator Orator) {
	<-TTSDoneChan
	orator.GetLogger().Info("orator got done signal")
	orator.Stop()
	// drain the channel
	for len(TTSTextChan) > 0 {
		<-TTSTextChan
	}
}

func readroutine(orator Orator) {
	tokenizer, _ := english.NewSentenceTokenizer(nil)
	var sentenceBuf bytes.Buffer
	var remainder strings.Builder
	for {
		select {
		case chunk := <-TTSTextChan:
			sentenceBuf.WriteString(chunk)
			text := sentenceBuf.String()
			sentences := tokenizer.Tokenize(text)
			for i, sentence := range sentences {
				if i == len(sentences)-1 {
					sentenceBuf.Reset()
					sentenceBuf.WriteString(sentence.Text)
					continue
				}
				// Send complete sentence to TTS
				if err := orator.Speak(sentence.Text); err != nil {
					orator.GetLogger().Error("tts failed", "sentence", sentence.Text, "error", err)
				}
			}
		case <-TTSFlushChan:
			// lln is done get the whole message out
			// FIXME: loses one token
			for chunk := range TTSTextChan {
				// orator.GetLogger().Info("flushing", "chunk", chunk)
				// sentenceBuf.WriteString(chunk)
				remainder.WriteString(chunk) // I get text here
				if len(TTSTextChan) == 0 {
					break
				}
			}
			// INFO: if there is a lot of text it will take some time to make with tts at once
			// to avoid this pause, it might be better to keep splitting on sentences
			// but keepinig in mind that remainder could be ommited by tokenizer
			// Flush remaining text
			remaining := remainder.String()
			remainder.Reset()
			if remaining != "" {
				// orator.GetLogger().Info("flushing", "remaining", remaining)
				if err := orator.Speak(remaining); err != nil {
					orator.GetLogger().Error("tts failed", "sentence", remaining, "error", err)
				}
			}
			// case <-TTSDoneChan:
			// 	orator.GetLogger().Info("orator got done signal")
			// 	orator.Stop()
			// 	// it that the best way to empty channel?
			// 	close(TTSTextChan)
			// 	TTSTextChan = make(chan string, 10000)
			// 	return
		}
	}
}

func NewOrator(log *slog.Logger, cfg *config.Config) Orator {
	orator := &KokoroOrator{
		logger:   log,
		URL:      cfg.TTS_URL,
		Format:   models.AFMP3,
		Stream:   false,
		Speed:    cfg.TTS_SPEED,
		Language: "a",
		Voice:    "af_bella(1)+af_sky(1)",
	}
	go readroutine(orator)
	go stoproutine(orator)
	return orator
}

func (o *KokoroOrator) GetLogger() *slog.Logger {
	return o.logger
}

func (o *KokoroOrator) requestSound(text string) (io.ReadCloser, error) {
	payload := map[string]interface{}{
		"input":           text,
		"voice":           o.Voice,
		"response_format": o.Format,
		"download_format": o.Format,
		"stream":          o.Stream,
		"speed":           o.Speed,
		// "return_download_link": true,
		"lang_code": o.Language,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	req, err := http.NewRequest("POST", o.URL, bytes.NewBuffer(payloadBytes)) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (o *KokoroOrator) Speak(text string) error {
	o.logger.Info("fn: Speak is called", "text-len", len(text))
	body, err := o.requestSound(text)
	if err != nil {
		o.logger.Error("request failed", "error", err)
		return fmt.Errorf("request failed: %w", err)
	}
	defer body.Close()
	// Decode the mp3 audio from response body
	streamer, format, err := mp3.Decode(body)
	if err != nil {
		o.logger.Error("mp3 decode failed", "error", err)
		return fmt.Errorf("mp3 decode failed: %w", err)
	}
	defer streamer.Close()
	// here it spams with errors that speaker cannot be initialized more than once, but how would we deal with many audio records then?
	if err := speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10)); err != nil {
		o.logger.Debug("failed to init speaker", "error", err)
	}
	done := make(chan bool)
	// Create controllable stream and store reference
	o.currentStream = &beep.Ctrl{Streamer: beep.Seq(streamer, beep.Callback(func() {
		close(done)
		o.currentStream = nil
	})), Paused: false}
	speaker.Play(o.currentStream)
	<-done // we hang in this routine;
	return nil
}

// TODO: stop works; but new stream does not start afterwards
func (o *KokoroOrator) Stop() {
	// speaker.Clear()
	o.logger.Info("attempted to stop orator", "orator", o)
	speaker.Lock()
	defer speaker.Unlock()
	if o.currentStream != nil {
		o.currentStream.Paused = true
		o.currentStream.Streamer = nil
	}
}

func (o *KokoroOrator) GetSBuilder() strings.Builder {
	return o.textBuffer
}
