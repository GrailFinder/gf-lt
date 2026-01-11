//go:build !extra

package main

import (
	"gf-lt/config"
	"log/slog"
)

// Interfaces and implementations when extra modules are not included

type Orator interface {
	Speak(text string) error
	Stop()
	GetLogger() *slog.Logger
}

type STT interface {
	StartRecording() error
	StopRecording() (string, error)
	IsRecording() bool
}

// DefaultOrator is a no-op implementation when TTS is not available
type DefaultOrator struct {
	logger *slog.Logger
}

func NewOrator(logger *slog.Logger, cfg *config.Config) Orator {
	return &DefaultOrator{logger: logger}
}

func (d *DefaultOrator) Speak(text string) error {
	d.logger.Debug("TTS not available - extra modules disabled")
	return nil
}

func (d *DefaultOrator) Stop() {
	// No-op
}

func (d *DefaultOrator) GetLogger() *slog.Logger {
	return d.logger
}

// DefaultSTT is a no-op implementation when STT is not available
type DefaultSTT struct {
	logger *slog.Logger
}

func NewSTT(logger *slog.Logger, cfg *config.Config) STT {
	return &DefaultSTT{logger: logger}
}

func (d *DefaultSTT) StartRecording() error {
	d.logger.Debug("STT not available - extra modules disabled")
	return nil
}

func (d *DefaultSTT) StopRecording() (string, error) {
	d.logger.Debug("STT not available - extra modules disabled")
	return "", nil
}

func (d *DefaultSTT) IsRecording() bool {
	return false
}

// TTS channels - no-op when extra is not available
var TTSTextChan = make(chan string, 10000)
var TTSFlushChan = make(chan bool, 1)
var TTSDoneChan = make(chan bool, 1)