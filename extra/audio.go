package extra

import (
	"bytes"
	"elefant/models"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
	"github.com/neurosnap/sentences/english"
)

var (
	TTSTextChan = make(chan string, 1000)
	TTSDoneChan = make(chan bool, 1)
)

type Orator interface {
	Speak(text string) error
	GetLogger() *slog.Logger
}

// impl https://github.com/remsky/Kokoro-FastAPI
type KokoroOrator struct {
	logger   *slog.Logger
	URL      string
	Format   models.AudioFormat
	Stream   bool
	Speed    int8
	Language string
}

func readroutine(orator Orator) {
	tokenizer, _ := english.NewSentenceTokenizer(nil)
	var sentenceBuf bytes.Buffer
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
		case <-TTSDoneChan:
			// Flush remaining text
			if remaining := sentenceBuf.String(); remaining != "" {
				if err := orator.Speak(remaining); err != nil {
					orator.GetLogger().Error("tts failed", "sentence", remaining, "error", err)
				}
			}
			return
		}
	}
}

func InitOrator(log *slog.Logger, URL string) Orator {
	orator := &KokoroOrator{
		logger:   log,
		URL:      URL,
		Format:   models.AFMP3,
		Stream:   false,
		Speed:    1,
		Language: "a",
	}
	go readroutine(orator)
	return orator
}

// type AudioStream struct {
// 	TextChan chan string // Send text chunks here
// 	DoneChan chan bool   // Close when streaming ends
// }

// func RunOrator(orator Orator) *AudioStream {
// 	stream := &AudioStream{
// 		TextChan: make(chan string, 1000),
// 		DoneChan: make(chan bool),
// 	}
// 	go func() {
// 		tokenizer, _ := english.NewSentenceTokenizer(nil)
// 		var sentenceBuf bytes.Buffer
// 		for {
// 			select {
// 			case chunk := <-stream.TextChan:
// 				sentenceBuf.WriteString(chunk)
// 				text := sentenceBuf.String()
// 				sentences := tokenizer.Tokenize(text)
// 				for i, sentence := range sentences {
// 					if i == len(sentences)-1 {
// 						sentenceBuf.Reset()
// 						sentenceBuf.WriteString(sentence.Text)
// 						continue
// 					}
// 					// Send complete sentence to TTS
// 					if err := orator.Speak(sentence.Text); err != nil {
// 						orator.GetLogger().Error("tts failed", "sentence", sentence.Text, "error", err)
// 					}
// 				}
// 			case <-stream.DoneChan:
// 				// Flush remaining text
// 				if remaining := sentenceBuf.String(); remaining != "" {
// 					if err := orator.Speak(remaining); err != nil {
// 						orator.GetLogger().Error("tts failed", "sentence", remaining, "error", err)
// 					}
// 				}
// 				return
// 			}
// 		}
// 	}()
// 	return stream
// }

func (o *KokoroOrator) GetLogger() *slog.Logger {
	return o.logger
}

func (o *KokoroOrator) requestSound(text string) (io.ReadCloser, error) {
	payload := map[string]interface{}{
		"input":                text,
		"voice":                "af_bella(1)+af_sky(1)",
		"response_format":      "mp3",
		"download_format":      "mp3",
		"stream":               o.Stream,
		"speed":                o.Speed,
		"return_download_link": true,
		"lang_code":            o.Language,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	req, err := http.NewRequest("POST", o.URL, bytes.NewBuffer(payloadBytes))
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
	speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		close(done)
	})))
	<-done
	return nil
}
