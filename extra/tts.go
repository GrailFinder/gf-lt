//go:build extra
// +build extra

package extra

import (
	"fmt"
	"gf-lt/config"
	"gf-lt/models"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	google_translate_tts "github.com/GrailFinder/google-translate-tts"
	"github.com/GrailFinder/google-translate-tts/handlers"
	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/neurosnap/sentences/english"
)

var (
	TTSTextChan  = make(chan string, 10000)
	TTSFlushChan = make(chan bool, 1)
	TTSDoneChan  = make(chan bool, 1)
	// endsWithPunctuation = regexp.MustCompile(`[;.!?]$`)
)

type Orator interface {
	Speak(text string) error
	Stop()
	// pause and resume?
	GetLogger() *slog.Logger
}

// Google Translate TTS implementation
type GoogleTranslateOrator struct {
	logger        *slog.Logger
	mu            sync.Mutex
	speech        *google_translate_tts.Speech
	currentStream *beep.Ctrl
	currentDone   chan bool
	textBuffer    strings.Builder
	interrupt     bool
}

func NewOrator(log *slog.Logger, cfg *config.Config) Orator {
	provider := cfg.TTS_PROVIDER
	if provider == "" {
		provider = "google" // does not require local setup
	}
	switch strings.ToLower(provider) {
	case "kokoro": // kokoro
		orator := &KokoroOrator{
			logger:   log,
			URL:      cfg.TTS_URL,
			Format:   models.AFMP3,
			Stream:   false,
			Speed:    cfg.TTS_SPEED,
			Language: "a",
			Voice:    "af_bella(1)+af_sky(1)",
		}
		go orator.readroutine()
		go orator.stoproutine()
		return orator
	default:
		language := cfg.TTS_LANGUAGE
		if language == "" {
			language = "en"
		}
		speech := &google_translate_tts.Speech{
			Folder:   os.TempDir() + "/gf-lt-tts", // Temporary directory for caching
			Language: language,
			Proxy:    "", // Proxy not supported
			Speed:    cfg.TTS_SPEED,
			Handler:  &handlers.Beep{},
		}
		orator := &GoogleTranslateOrator{
			logger: log,
			speech: speech,
		}
		go orator.readroutine()
		go orator.stoproutine()
		return orator
	}
}

func (o *GoogleTranslateOrator) stoproutine() {
	for {
		<-TTSDoneChan
		o.logger.Debug("orator got done signal")
		o.Stop()
		// drain the channel
		for len(TTSTextChan) > 0 {
			<-TTSTextChan
		}
		o.mu.Lock()
		o.textBuffer.Reset()
		if o.currentDone != nil {
			select {
			case o.currentDone <- true:
			default:
				// Channel might be closed, ignore
			}
		}
		o.interrupt = true
		o.mu.Unlock()
	}
}

func (o *GoogleTranslateOrator) readroutine() {
	tokenizer, _ := english.NewSentenceTokenizer(nil)
	for {
		select {
		case chunk := <-TTSTextChan:
			o.mu.Lock()
			o.interrupt = false
			_, err := o.textBuffer.WriteString(chunk)
			if err != nil {
				o.logger.Warn("failed to write to stringbuilder", "error", err)
				o.mu.Unlock()
				continue
			}
			text := o.textBuffer.String()
			sentences := tokenizer.Tokenize(text)
			o.logger.Debug("adding chunk", "chunk", chunk, "text", text, "sen-len", len(sentences))
			if len(sentences) <= 1 {
				o.mu.Unlock()
				continue
			}
			completeSentences := sentences[:len(sentences)-1]
			remaining := sentences[len(sentences)-1].Text
			o.textBuffer.Reset()
			o.textBuffer.WriteString(remaining)
			o.mu.Unlock()

			for _, sentence := range completeSentences {
				o.mu.Lock()
				interrupted := o.interrupt
				o.mu.Unlock()
				if interrupted {
					return
				}
				cleanedText := models.CleanText(sentence.Text)
				if cleanedText == "" {
					continue
				}
				o.logger.Debug("calling Speak with sentence", "sent", cleanedText)
				if err := o.Speak(cleanedText); err != nil {
					o.logger.Error("tts failed", "sentence", cleanedText, "error", err)
				}
			}
		case <-TTSFlushChan:
			o.logger.Debug("got flushchan signal start")
			// lln is done get the whole message out
			if len(TTSTextChan) > 0 { // otherwise might get stuck
				for chunk := range TTSTextChan {
					o.mu.Lock()
					_, err := o.textBuffer.WriteString(chunk)
					o.mu.Unlock()
					if err != nil {
						o.logger.Warn("failed to write to stringbuilder", "error", err)
						continue
					}
					if len(TTSTextChan) == 0 {
						break
					}
				}
			}
			o.mu.Lock()
			remaining := o.textBuffer.String()
			remaining = models.CleanText(remaining)
			o.textBuffer.Reset()
			o.mu.Unlock()
			if remaining == "" {
				continue
			}
			o.logger.Debug("calling Speak with remainder", "rem", remaining)
			sentencesRem := tokenizer.Tokenize(remaining)
			for _, rs := range sentencesRem { // to avoid dumping large volume of text
				o.mu.Lock()
				interrupt := o.interrupt
				o.mu.Unlock()
				if interrupt {
					break
				}
				if err := o.Speak(rs.Text); err != nil {
					o.logger.Error("tts failed", "sentence", rs.Text, "error", err)
				}
			}
		}
	}
}

func (o *GoogleTranslateOrator) GetLogger() *slog.Logger {
	return o.logger
}

func (o *GoogleTranslateOrator) Speak(text string) error {
	o.logger.Debug("fn: Speak is called", "text-len", len(text))
	// Generate MP3 data using google-translate-tts
	reader, err := o.speech.GenerateSpeech(text)
	if err != nil {
		o.logger.Error("generate speech failed", "error", err)
		return fmt.Errorf("generate speech failed: %w", err)
	}
	// Decode the mp3 audio from reader (wrap with NopCloser for io.ReadCloser)
	streamer, format, err := mp3.Decode(io.NopCloser(reader))
	if err != nil {
		o.logger.Error("mp3 decode failed", "error", err)
		return fmt.Errorf("mp3 decode failed: %w", err)
	}
	defer streamer.Close()
	playbackStreamer := beep.Streamer(streamer)
	speed := o.speech.Speed
	if speed <= 0 {
		speed = 1.0
	}
	if speed != 1.0 {
		playbackStreamer = beep.ResampleRatio(3, float64(speed), streamer)
	}
	// Initialize speaker with the format's sample rate
	if err := speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10)); err != nil {
		o.logger.Debug("failed to init speaker", "error", err)
	}
	done := make(chan bool)
	o.mu.Lock()
	o.currentDone = done
	o.currentStream = &beep.Ctrl{Streamer: beep.Seq(playbackStreamer, beep.Callback(func() {
		o.mu.Lock()
		close(done)
		o.currentStream = nil
		o.currentDone = nil
		o.mu.Unlock()
	})), Paused: false}
	o.mu.Unlock()
	speaker.Play(o.currentStream)
	<-done // wait for playback to complete
	return nil
}

func (o *GoogleTranslateOrator) Stop() {
	o.logger.Debug("attempted to stop google translate orator")
	speaker.Lock()
	defer speaker.Unlock()
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.currentStream != nil {
		o.currentStream.Streamer = nil
	}
	// Also stop the speech handler if possible
	if o.speech != nil {
		_ = o.speech.Stop()
	}
}
