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
	appLogger *slog.Logger // nil until first use and after SetOutput/SetFormat
	logOut    io.Writer    // nil means os.Stdout
	logFormat string       // resolved format; empty until resolved
)

// writeMu serializes writes from every logger built by this package, so lines
// from handlers created at different times (after SetFormat or SetOutput)
// never interleave on a shared writer.
var writeMu sync.Mutex

type syncWriter struct{ w io.Writer }

func (s syncWriter) Write(p []byte) (int, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	return s.w.Write(p)
}

// newLogger writes to w: "text" selects slog's text handler, "string" the
// compact line handler, anything else JSON.
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
	defer logMu.Unlock()
	if appLogger == nil {
		rebuildLocked(os.Getenv("LOG_FORMAT"), "LOG_FORMAT")
	}
	return appLogger
}

// rebuildLocked builds appLogger. When no format has been resolved yet, raw is
// resolved and an unknown value produces one warning naming source.
func rebuildLocked(raw, source string) {
	var warn string
	if logFormat == "" {
		var ok bool
		logFormat, ok = resolveFormat(raw)
		if !ok {
			warn = fmt.Sprintf("logus: unrecognized %s %q, using json", source, strings.TrimSpace(raw))
		}
	}
	var w io.Writer = os.Stdout
	if logOut != nil {
		w = logOut
	}
	appLogger = newLogger(syncWriter{w}, logFormat)
	if warn != "" {
		appLogger.Warn(warn)
	}
}

// SetOutput redirects every line to w; nil restores os.Stdout. It is safe for
// concurrent use.
func SetOutput(w io.Writer) {
	logMu.Lock()
	defer logMu.Unlock()
	logOut = w
	rebuildLocked(os.Getenv("LOG_FORMAT"), "LOG_FORMAT")
}

// SetFormat selects json, text or string (case-insensitive). An empty name
// means json; an unknown one falls back to json and writes one warning.
func SetFormat(format string) {
	logMu.Lock()
	defer logMu.Unlock()
	logFormat = ""
	rebuildLocked(format, "log format")
}
