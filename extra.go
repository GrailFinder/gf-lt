//go:build extra
// +build extra

package main

import (
	"gf-lt/config"
	"gf-lt/extra"
	"log/slog"
)

// Interfaces and implementations when extra modules are included

type Orator = extra.Orator
type STT = extra.STT

func NewOrator(logger *slog.Logger, cfg *config.Config) Orator {
	return extra.NewOrator(logger, cfg)
}

func NewSTT(logger *slog.Logger, cfg *config.Config) STT {
	return extra.NewSTT(logger, cfg)
}

// TTS channels from extra package
var TTSTextChan = extra.TTSTextChan
var TTSFlushChan = extra.TTSFlushChan
var TTSDoneChan = extra.TTSDoneChan