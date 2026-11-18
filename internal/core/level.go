package core

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Level is a severity. The values are aligned with log/slog so a Level converts
// to a slog.Level without translation.
type Level int

// The six standard levels, ordered trace < debug < info < warn < error < fatal.
// The public logus package mirrors these values as its LogLevel constants.
const (
	LevelTrace Level = -8
	LevelDebug Level = -4
	LevelInfo  Level = 0
	LevelWarn  Level = 4
	LevelError Level = 8
	LevelFatal Level = 12
)

// String returns the upper-case level name, or a numeric form for values that
// are not one of the six levels.
func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	}
	return "LEVEL(" + strconv.Itoa(int(l)) + ")"
}

func (l Level) valid() bool {
	switch l {
	case LevelTrace, LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal:
		return true
	}
	return false
}

var (
	// levelVar is the runtime threshold shared by every handler.
	levelVar  slog.LevelVar
	levelOnce sync.Once
)

// // parseLevel parses a LOG_LEVEL name (case-insensitive) or legacy v0.1.x number
// // (0-1 error, 2-5 info, 6 debug); ok is false otherwise and the level is info.
func parseLevel(v string) (level Level, ok bool) {
	switch normalizeKey(v) {
	case "trace":
		return LevelTrace, true
	case "debug":
		return LevelDebug, true
	case "info":
		return LevelInfo, true
	case "warn", "warning":
		return LevelWarn, true
	case "error":
		return LevelError, true
	case "fatal":
		return LevelFatal, true
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		switch {
		case n == 0 || n == 1:
			return LevelError, true
		case n >= 2 && n <= 5:
			return LevelInfo, true
		case n == 6:
			return LevelDebug, true
		}
	}
	return LevelInfo, false
}

// ensureLevel reads LOG_LEVEL once, on first use. An unrecognized value falls
// back to info and writes one warning through the logger.
func ensureLevel() {
	levelOnce.Do(func() {
		level := LevelInfo
		raw, set := os.LookupEnv("LOG_LEVEL")
		valid := true
		if set {
			level, valid = parseLevel(raw)
		}
		levelVar.Set(slog.Level(level))
		if !valid {
			getLogger().Warn(fmt.Sprintf("logus: unrecognized LOG_LEVEL %q, using info", raw),
				slog.String("server_ip", getEnv("ServerIP", "")))
		}
	})
}

// SetLevel changes the threshold. It is safe for concurrent use and, when
// called before the first log, takes precedence over LOG_LEVEL.
func SetLevel(l Level) {
	levelOnce.Do(func() {})
	levelVar.Set(slog.Level(l))
}

// GetLevel returns the current threshold, reading LOG_LEVEL first if the
// logger has not been used yet.
func GetLevel() Level {
	ensureLevel()
	return Level(levelVar.Level())
}

// replaceLevelAttr renders the level attribute by name instead of slog's
// offset form such as "DEBUG-4" or "ERROR+4".
func replaceLevelAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.LevelKey {
		if l, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(Level(l).String())
		}
	}
	return a
}
