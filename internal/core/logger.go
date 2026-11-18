package core

import (
	"io"
	"log/slog"
	"os"
	"sync"
)

var (
	loggerOnce sync.Once
	appLogger  *slog.Logger
)

// // newLogger writes to w: "text" selects slog's text handler, "string" the
// // compact line handler, anything else JSON.
func newLogger(w io.Writer, format string) *slog.Logger {
	options := &slog.HandlerOptions{Level: &levelVar, ReplaceAttr: replaceLevelAttr}
	switch normalizeKey(format) {
	case "text":
		return slog.New(slog.NewTextHandler(w, options))
	case "string":
		return slog.New(newStringHandler(w, &levelVar))
	default:
		return slog.New(slog.NewJSONHandler(w, options))
	}
}

// getLogger returns the process-wide logger. LOG_FORMAT is read once, on the
// first call.
func getLogger() *slog.Logger {
	loggerOnce.Do(func() {
		appLogger = newLogger(os.Stdout, normalizeKey(getEnv("LOG_FORMAT", "json")))
	})

	return appLogger
}
