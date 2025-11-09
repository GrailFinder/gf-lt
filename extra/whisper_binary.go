package extra

import (
	"context"
	"gf-lt/config"
	"log/slog"
	"os/exec"
	"sync"
)

type WhisperBinary struct {
	whisperPath string
	modelPath   string
	lang        string
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	running     bool
	cmd         *exec.Cmd
}

func NewWhisperBinary(logger *slog.Logger, cfg *config.Config) *WhisperBinary {
	ctx, cancel := context.WithCancel(context.Background())
	return &WhisperBinary{
		whisperPath: cfg.WhisperBinaryPath,
		modelPath:   cfg.WhisperModelPath,
		lang:        cfg.STT_LANG,
		ctx:         ctx,
		cancel:      cancel,
	}
}
