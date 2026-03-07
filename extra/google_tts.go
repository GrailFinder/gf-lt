//go:build extra
// +build extra

package extra

import (
	"fmt"
	"gf-lt/models"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"

	google_translate_tts "github.com/GrailFinder/google-translate-tts"
	"github.com/neurosnap/sentences/english"
)

type GoogleTranslateOrator struct {
	logger *slog.Logger
	mu     sync.Mutex
	speech *google_translate_tts.Speech
	// fields for playback control
	cmd    *exec.Cmd
	cmdMu  sync.Mutex
	stopCh chan struct{}
	// text buffer and interrupt flag
	textBuffer strings.Builder
	interrupt  bool
}

func (o *GoogleTranslateOrator) stoproutine() {
	for {
		<-TTSDoneChan
		o.logger.Debug("orator got done signal")
		o.Stop()
		for len(TTSTextChan) > 0 {
			<-TTSTextChan
		}
		o.mu.Lock()
		o.textBuffer.Reset()
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
	// Generate MP3 data directly as an io.Reader
	reader, err := o.speech.GenerateSpeech(text)
	if err != nil {
		return fmt.Errorf("generate speech failed: %w", err)
	}
	// Wrap in io.NopCloser since GenerateSpeech returns io.Reader (no close needed)
	body := io.NopCloser(reader)
	defer body.Close()
	// Exactly the same ffplay piping as KokoroOrator
	cmd := exec.Command("ffplay", "-nodisp", "-autoexit", "-i", "pipe:0")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	o.cmdMu.Lock()
	o.cmd = cmd
	o.stopCh = make(chan struct{})
	o.cmdMu.Unlock()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffplay: %w", err)
	}
	copyErr := make(chan error, 1)
	go func() {
		_, err := io.Copy(stdin, body)
		stdin.Close()
		copyErr <- err
	}()
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case <-o.stopCh:
		if o.cmd != nil && o.cmd.Process != nil {
			o.cmd.Process.Kill()
		}
		<-done
		return nil
	case copyErrVal := <-copyErr:
		if copyErrVal != nil {
			if o.cmd != nil && o.cmd.Process != nil {
				o.cmd.Process.Kill()
			}
			<-done
			return copyErrVal
		}
		return <-done
	case err := <-done:
		return err
	}
}

func (o *GoogleTranslateOrator) Stop() {
	o.cmdMu.Lock()
	defer o.cmdMu.Unlock()
	// Signal any running Speak to stop
	if o.stopCh != nil {
		select {
		case <-o.stopCh: // already closed
		default:
			close(o.stopCh)
		}
		o.stopCh = nil
	}
	// Kill the external player process if it's still running
	if o.cmd != nil && o.cmd.Process != nil {
		o.cmd.Process.Kill()
		o.cmd.Wait() // clean up zombie process
		o.cmd = nil
	}
	// Also reset text buffer and interrupt flag (with o.mu)
	o.mu.Lock()
	o.textBuffer.Reset()
	o.interrupt = true
	o.mu.Unlock()
}
