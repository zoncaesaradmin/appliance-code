package process

import (
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func NewLogger(logPath string) *slog.Logger {
	writers := []io.Writer{os.Stdout}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err == nil {
		if file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			writers = append(writers, file)
		}
	}
	return slog.New(slog.NewTextHandler(io.MultiWriter(writers...), nil))
}

func ShutdownSignal() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	return ch
}
