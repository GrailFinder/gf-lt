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

func (o *KokoroOrator) fetchAudio(text string) ([]byte, error) {
	body, err := o.requestSound(text)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio: %w", err)
	}
	return data, nil
}

func (o *KokoroOrator) playAudio(data []byte) error {
	var stderrBuf bytes.Buffer
	cmd := exec.Command("ffplay", "-nodisp", "-autoexit", "-i", "pipe:0")
	cmd.Stderr = &stderrBuf
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
		_, err := io.Copy(stdin, bytes.NewReader(data))
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
			if !strings.Contains(copyErrVal.Error(), "broken pipe") {
				o.logger.Error("stdin copy failed", "stderr", stderrBuf.String(), "error", copyErrVal)
				if o.cmd != nil && o.cmd.Process != nil {
					o.cmd.Process.Kill()
				}
				<-done
				return copyErrVal
			}
		}
		return <-done
	case err := <-done:
		if err != nil {
			o.logger.Error("ffplay exited with error", "stderr", stderrBuf.String(), "exit", err)
		}
		return err
	}
}

func (o *KokoroOrator) Speak(text string) error {
	o.logger.Debug("fn: Speak is called", "text-len", len(text))
	data, err := o.fetchAudio(text)
	if err != nil {
		return err
	}
	return o.playAudio(data)
}

type audioResult struct {
	data []byte
	err  error
}

func (o *KokoroOrator) speakSentences(sentences []string) {
	if len(sentences) == 0 {
		return
	}
	data, err := o.fetchAudio(sentences[0])
	if err != nil {
		o.logger.Error("fetch failed", "sentence", sentences[0], "error", err)
		return
	}
	for i := 0; i < len(sentences); i++ {
		o.mu.Lock()
		interrupted := o.interrupt
		o.mu.Unlock()
		if interrupted {
			return
		}
		var nextCh chan audioResult
		if i+1 < len(sentences) {
			nextCh = make(chan audioResult, 1)
			idx := i + 1
			go func() {
				d, err := o.fetchAudio(sentences[idx])
				nextCh <- audioResult{d, err}
			}()
		}
		o.logger.Debug("playing sentence", "sentence", sentences[i])
		if err := o.playAudio(data); err != nil {
			o.logger.Error("playback failed", "sentence", sentences[i], "error", err)
			return
		}
		if nextCh != nil {
			result := <-nextCh
			if result.err != nil {
				o.logger.Error("fetch failed", "sentence", sentences[i+1], "error", result.err)
				return
			}
			data = result.data
		}
	}
}
func (o *KokoroOrator) requestSound(text string) (io.ReadCloser, error) {
	if o.URL == "" {
		return nil, fmt.Errorf("TTS URL is empty")
	}
	payload := map[string]interface{}{
		"model":           "tts-1",
		"input":           text,
		"voice":           o.Voice,
		"response_format": "mp3",
		"speed":           o.Speed,
		"stream_format":   "audio",
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
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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
			var texts []string
			for _, sentence := range completeSentences {
				cleanedText := models.CleanText(sentence.Text)
				if cleanedText != "" {
					texts = append(texts, cleanedText)
				}
			}
			if len(texts) > 0 {
				o.mu.Lock()
				interrupted := o.interrupt
				o.mu.Unlock()
				if !interrupted {
					o.speakSentences(texts)
				}
			}
		case <-TTSFlushChan:
			o.logger.Debug("got flushchan signal start")
			if len(TTSTextChan) > 0 {
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
			o.logger.Debug("calling speakSentences with remainder", "rem", remaining)
			var texts []string
			for _, rs := range tokenizer.Tokenize(remaining) {
				o.mu.Lock()
				interrupt := o.interrupt
				o.mu.Unlock()
				if interrupt {
					break
				}
				texts = append(texts, rs.Text)
			}
			if len(texts) > 0 {
				o.speakSentences(texts)
			}
		}
	}
}
