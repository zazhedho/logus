package core

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	logMu     sync.RWMutex
	appLogger *slog.Logger // nil until first use and after SetFormat
	logFormat string       // resolved format; empty until resolved
)

// writeMu is the single write lock. Every line, in every format, is written
// while holding it, and the destination curOut is read and replaced only under
// it. SetOutput therefore switches atomically: once it returns, no write can
// reach the previous writer. Because the lock is not reentrant, a writer given
// to SetOutput must not call back into logus.
var (
	writeMu sync.Mutex
	curOut  io.Writer // nil means os.Stdout; guarded by writeMu
)

// dest returns the current destination. The caller must hold writeMu.
func dest() io.Writer {
	if curOut != nil {
		return curOut
	}
	return os.Stdout
}

// destWriter writes to the current destination without locking; it is for
// callers that already hold writeMu (the string handler).
type destWriter struct{}

func (destWriter) Write(p []byte) (int, error) { return dest().Write(p) }

// lockedWriter takes writeMu around each write (json and text handlers).
type lockedWriter struct{}

func (lockedWriter) Write(p []byte) (int, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	return dest().Write(p)
}

// newLogger builds a logger for the package destination: "text" selects slog's
// text handler, "string" the compact line handler, anything else JSON.
func newLogger(format string) *slog.Logger {
	options := &slog.HandlerOptions{Level: &levelVar, ReplaceAttr: replaceLevelAttr}
	switch normalizeKey(format) {
	case "text":
		return slog.New(slog.NewTextHandler(lockedWriter{}, options))
	case "string":
		return slog.New(newStringHandlerLocked(destWriter{}, &levelVar, &writeMu))
	default:
		return slog.New(slog.NewJSONHandler(lockedWriter{}, options))
	}
}

// resolveFormat maps a format name to one of json, text or string. ok is false
// for an unknown non-empty name, which resolves to json.
func resolveFormat(v string) (format string, ok bool) {
	switch f := normalizeKey(v); f {
	case "json", "text", "string":
		return f, true
	case "":
		return "json", true
	}
	return "json", false
}

// getLogger returns the process-wide logger, building it on first use. The
// format comes from SetFormat or, failing that, LOG_FORMAT.
func getLogger() *slog.Logger {
	logMu.RLock()
	l := appLogger
	logMu.RUnlock()
	if l != nil {
		return l
	}
	logMu.Lock()
	var warn string
	if appLogger == nil {
		warn = rebuildLocked(os.Getenv("LOG_FORMAT"), "LOG_FORMAT")
	}
	l = appLogger
	logMu.Unlock()
	if warn != "" {
		l.Warn(warn) // after unlocking: writing never happens under logMu
	}
	return l
}

// rebuildLocked builds appLogger. When no format has been resolved yet, raw is
// resolved; for an unknown value it returns the warning to emit once logMu is
// released, naming source.
func rebuildLocked(raw, source string) (warn string) {
	if logFormat == "" {
		var ok bool
		logFormat, ok = resolveFormat(raw)
		if !ok {
			warn = fmt.Sprintf("logus: unrecognized %s %q, using json", source, strings.TrimSpace(raw))
		}
	}
	appLogger = newLogger(logFormat)
	return warn
}

// SetOutput redirects every line to w; nil restores os.Stdout. It is safe for
// concurrent use. After it returns, no further writes reach the previous
// writer. w must not call logus: the write lock is not reentrant.
func SetOutput(w io.Writer) {
	writeMu.Lock()
	curOut = w
	writeMu.Unlock()
}

// SetFormat selects json, text or string (case-insensitive). An empty name
// means json; an unknown one falls back to json and writes one warning.
func SetFormat(format string) {
	logMu.Lock()
	logFormat = ""
	warn := rebuildLocked(format, "log format")
	l := appLogger
	logMu.Unlock()
	if warn != "" {
		l.Warn(warn)
	}
}
