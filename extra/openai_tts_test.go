//go:build extra
// +build extra

package extra

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gf-lt/config"

	"github.com/BurntSushi/toml"
)

// captureSpeech stands up a stub that records the JSON body gf-lt sends to
// /v1/audio/speech and replies with a minimal 200.
func captureSpeech(t *testing.T) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	got := map[string]interface{}{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("unmarshal body %q: %v", body, err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func oratorForURL(url string, mutate func(*config.Config)) *OpenAICompatOrator {
	cfg := &config.Config{
		TTS_URL:   url,
		TTS_MODEL: "omnivoice",
		// Zero so tests start from "unset" and opt in explicitly.
		TTS_SPEED: 0,
	}
	if mutate != nil {
		mutate(cfg)
	}
	return &OpenAICompatOrator{
		URL:      url,
		Format:   resolveFormat(cfg),
		Speed:    cfg.TTS_SPEED,
		Voice:    cfg.TTS_VOICE,
		Model:    cfg.TTS_MODEL,
		VoiceRef: cfg.TTS_VOICE_REF,
		RefText:  cfg.TTS_REFERENCE_TEXT,
	}
}

// audio.cpp only encodes WAV, so the default must never be mp3.
func TestSpeechDefaultsToWav(t *testing.T) {
	srv, got := captureSpeech(t)
	o := oratorForURL(srv.URL, nil)
	if o.Format != "wav" {
		t.Fatalf("default format = %q, want wav", o.Format)
	}
	if _, err := o.requestSound("Hi."); err != nil {
		t.Fatal(err)
	}
	if rf := (*got)["response_format"]; rf != "wav" {
		t.Errorf("response_format = %v, want wav", rf)
	}
}

// An unset speed must be omitted: audio.cpp rejects an unsupported speed.
func TestSpeechOmitsUnsetSpeed(t *testing.T) {
	srv, got := captureSpeech(t)
	o := oratorForURL(srv.URL, func(c *config.Config) { c.TTS_SPEED = 0 })
	if _, err := o.requestSound("Hi."); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*got)["speed"]; ok {
		t.Errorf("speed should be omitted when unset, got %v", (*got)["speed"])
	}
}

func TestSpeechIncludesPositiveSpeed(t *testing.T) {
	srv, got := captureSpeech(t)
	o := oratorForURL(srv.URL, func(c *config.Config) { c.TTS_SPEED = 1.1 })
	if _, err := o.requestSound("Hi."); err != nil {
		t.Fatal(err)
	}
	if sp := (*got)["speed"]; sp != 1.1 {
		t.Errorf("speed = %v, want 1.1", sp)
	}
}

// With a clone reference set, "voice" must be omitted so the server cannot
// prefer a preset and ignore the reference audio.
func TestSpeechCloneSendsVoiceRefNotVoice(t *testing.T) {
	srv, got := captureSpeech(t)
	o := oratorForURL(srv.URL, func(c *config.Config) {
		c.TTS_VOICE_REF = "/tmp/ref.wav"
		c.TTS_REFERENCE_TEXT = "reference transcript"
		c.TTS_VOICE = "alba" // stale preset that must not be sent
	})
	if _, err := o.requestSound("Hi."); err != nil {
		t.Fatal(err)
	}
	if vr := (*got)["voice_ref"]; vr != "/tmp/ref.wav" {
		t.Errorf("voice_ref = %v, want /tmp/ref.wav", vr)
	}
	if rt := (*got)["reference_text"]; rt != "reference transcript" {
		t.Errorf("reference_text = %v, want reference transcript", rt)
	}
	if _, ok := (*got)["voice"]; ok {
		t.Errorf("voice must be omitted on the clone path, got %v", (*got)["voice"])
	}
}

// Without a clone reference, plain preset "voice" is still sent.
func TestSpeechNonCloneSendsVoice(t *testing.T) {
	srv, got := captureSpeech(t)
	o := oratorForURL(srv.URL, func(c *config.Config) { c.TTS_VOICE = "alba" })
	if _, err := o.requestSound("Hi."); err != nil {
		t.Fatal(err)
	}
	if v := (*got)["voice"]; v != "alba" {
		t.Errorf("voice = %v, want alba", v)
	}
	if _, ok := (*got)["voice_ref"]; ok {
		t.Errorf("voice_ref should be absent, got %v", (*got)["voice_ref"])
	}
}

func TestSpeechSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":{"message":"OmniVoice native voice clone currently requires reference_text"}}`)
	}))
	defer srv.Close()
	o := oratorForURL(srv.URL, func(c *config.Config) { c.TTS_VOICE_REF = "/tmp/ref.wav" })
	_, err := o.requestSound("Hi.")
	if err == nil {
		t.Fatal("expected an error from a 500 response")
	}
	if !strings.Contains(err.Error(), "requires reference_text") {
		t.Errorf("error should carry the server message, got: %v", err)
	}
}

// The checked-in config.toml must actually resolve to an audio.cpp setup.
func TestRepoConfigTargetsAudioCpp(t *testing.T) {
	raw, err := os.ReadFile("../config.toml")
	if err != nil {
		t.Skipf("config.toml not readable: %v", err)
	}
	var cfg config.Config
	if _, err := toml.Decode(string(raw), &cfg); err != nil {
		t.Fatalf("config.toml does not parse: %v", err)
	}
	if cfg.TTS_FORMAT != "wav" {
		t.Errorf("TTS_FORMAT = %q, want wav", cfg.TTS_FORMAT)
	}
	if cfg.TTS_VOICE_REF == "" {
		t.Skip("config.toml sets no clone reference; nothing to assert")
	}
	if cfg.TTS_REFERENCE_TEXT == "" {
		t.Error("TTS_VOICE_REF is set but TTS_REFERENCE_TEXT is empty; OmniVoice returns HTTP 500")
	}
}
