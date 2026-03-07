//go:build extra
// +build extra

package extra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gf-lt/models"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	"github.com/neurosnap/sentences/english"
)

type KokoroOrator struct {
	logger   *slog.Logger
	mu       sync.Mutex
	URL      string
	Format   models.AudioFormat
	Stream   bool
	Speed    float32
	Language string
	Voice    string
	// fields for playback control
	cmd    *exec.Cmd
	cmdMu  sync.Mutex
	stopCh chan struct{}
	// textBuffer, interrupt etc. remain the same
	textBuffer strings.Builder
	interrupt  bool
}

func (o *KokoroOrator) GetLogger() *slog.Logger {
	return o.logger
}

func (o *KokoroOrator) Speak(text string) error {
	o.logger.Debug("fn: Speak is called", "text-len", len(text))
	body, err := o.requestSound(text)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer body.Close()
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
	// Copy audio in background
	copyErr := make(chan error, 1)
	go func() {
		_, err := io.Copy(stdin, body)
		stdin.Close()
		copyErr <- err
	}()
	// Wait for player in background
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	// Wait for BOTH copy and player, but ensure we block until done
	select {
	case <-o.stopCh:
		// Stop requested: kill player and wait for it to exit
		if o.cmd != nil && o.cmd.Process != nil {
			o.cmd.Process.Kill()
		}
		<-done // Wait for process to actually exit
		return nil
	case copyErrVal := <-copyErr:
		if copyErrVal != nil {
			// Copy failed: kill player and wait
			if o.cmd != nil && o.cmd.Process != nil {
				o.cmd.Process.Kill()
			}
			<-done
			return copyErrVal
		}
		// Copy succeeded, now wait for playback to complete
		return <-done
	case err := <-done:
		// Playback finished normally (copy must have succeeded or player would have exited early)
		return err
	}
}
func (o *KokoroOrator) requestSound(text string) (io.ReadCloser, error) {
	if o.URL == "" {
		return nil, fmt.Errorf("TTS URL is empty")
	}
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

func (o *KokoroOrator) stoproutine() {
	for {
		<-TTSDoneChan
		o.logger.Debug("orator got done signal")
		// 1. Stop any ongoing playback (kills external player, closes stopCh)
		o.Stop()
		// 2. Drain any pending text chunks
		for len(TTSTextChan) > 0 {
			<-TTSTextChan
		}
		// 3. Reset internal state
		o.mu.Lock()
		o.textBuffer.Reset()
		o.interrupt = true
		o.mu.Unlock()
	}
}

func (o *KokoroOrator) Stop() {
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

func (o *KokoroOrator) readroutine() {
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
			// flush remaining text
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
					o.logger.Error("tts failed", "sentence", rs, "error", err)
				}
			}
		}
	}
}
