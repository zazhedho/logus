// Package logus is a small structured logging module built on log/slog. Each
// line carries the server IP, node name, caller location, a log id and an
// optional user id. It recognizes gin-style contexts through duck typing and
// has no web framework dependency.
package logus

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Log levels. Lower numbers are more severe. A call is written only when the
// LOG_LEVEL environment variable, read as an integer, is greater than or equal
// to the call's level; for example LOG_LEVEL=3 writes Panic, Error, Fail and
// Info but drops Data, Warn and Debug.
const (
	LogLevelPanic = 0
	LogLevelError = 1
	LogLevelFail  = 2
	LogLevelInfo  = 3
	LogLevelData  = 4
	LogLevelWarn  = 5
	LogLevelDebug = 6
)

var logLevelMap = map[int]string{
	LogLevelPanic: "PANIC",
	LogLevelError: "ERROR",
	LogLevelFail:  "FAIL ",
	LogLevelInfo:  "INFO ",
	LogLevelData:  "DATA ",
	LogLevelDebug: "DEBUG",
	LogLevelWarn:  "WARN ",
}

var (
	loggerOnce sync.Once
	appLogger  *slog.Logger
)

type logIDContextKey struct{}
type userIDContextKey struct{}

// WriteLog writes a log line at the given level. When LOG_LEVEL is unset the
// threshold is LogLevelWarn. It logs with context.Background(), so the line
// still carries a log_id: a freshly generated id per call, since none is
// stored.
func WriteLog(level int, msg ...any) {
	writeLog(context.Background(), 5, level, msg...)
}

// WriteLogWithContext writes a log line enriched with the log id and user id
// found in ctx. When LOG_LEVEL is unset the threshold is LogLevelDebug.
func WriteLogWithContext(ctx context.Context, level int, msg ...any) {
	writeLog(ctx, 6, level, msg...)
}

// WithLogMetadata returns a copy of ctx carrying an explicit log id and user
// id. Empty values do not overwrite existing ones.
func WithLogMetadata(ctx context.Context, logID, userID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if logID != "" {
		ctx = context.WithValue(ctx, logIDContextKey{}, logID)
	}
	if userID != "" {
		ctx = context.WithValue(ctx, userIDContextKey{}, userID)
	}
	return ctx
}

func writeLog(ctx context.Context, defaultLogLevel, level int, msg ...any) {
	if _, ok := logLevelMap[level]; !ok {
		return
	}

	if logLevel, _ := strconv.Atoi(getEnv("LOG_LEVEL", strconv.Itoa(defaultLogLevel))); logLevel < level {
		return
	}

	logger := getLogger()
	attrs := []slog.Attr{
		slog.String("server_ip", getEnv("ServerIP", "")),
	}
	attrs = append(attrs, callerAttrs(3)...)
	if node := getEnv("NODE", ""); node != "" {
		attrs = append(attrs, slog.String("node", node))
	}

	if ctx != nil {
		logID := interfaceString(ctx.Value(logIDContextKey{}))
		if logID == "" {
			logID = RequestID(ctx).String()
		}
		attrs = append(attrs, slog.String("log_id", logID))

		userID := interfaceString(ctx.Value(userIDContextKey{}))
		if userID == "" {
			if value, exists := ctxGet(ctx, CtxKeyUserID); exists {
				userID = interfaceString(value)
			}
		}
		if userID != "" {
			attrs = append(attrs, slog.String("user_id", userID))
		}
	}

	logCtx := ctx
	if logCtx == nil {
		logCtx = context.Background()
	}
	fields := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		fields = append(fields, attr)
	}

	logger.Log(logCtx, mapLevelToSlog(level), fmt.Sprint(msg...), fields...)
}

// newLogger builds a logger writing to w. The normalized format "text" selects
// slog's text handler, "string" the compact bracketed line handler, and any
// other value the JSON handler.
func newLogger(w io.Writer, format string) *slog.Logger {
	options := &slog.HandlerOptions{Level: slog.LevelDebug}
	switch normalizeKey(format) {
	case "text":
		return slog.New(slog.NewTextHandler(w, options))
	case "string":
		return slog.New(newStringHandler(w, options.Level))
	default:
		return slog.New(slog.NewJSONHandler(w, options))
	}
}

func getLogger() *slog.Logger {
	loggerOnce.Do(func() {
		appLogger = newLogger(os.Stdout, normalizeKey(getEnv("LOG_FORMAT", "json")))
	})

	return appLogger
}

func mapLevelToSlog(level int) slog.Level {
	switch level {
	case LogLevelPanic, LogLevelError:
		return slog.LevelError
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelDebug:
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

func callerAttrs(skip int) []slog.Attr {
	pc, file, line, ok := runtime.Caller(skip)
	if !ok {
		return nil
	}

	sourceFile := normalizeSourceFile(file)
	sourceFunction := ""
	if fn := runtime.FuncForPC(pc); fn != nil {
		sourceFunction = fn.Name()
	}

	return []slog.Attr{
		slog.String("source_file", sourceFile),
		slog.Int("source_line", line),
		slog.String("source_function", sourceFunction),
	}
}

func normalizeSourceFile(file string) string {
	wd, err := os.Getwd()
	if err == nil {
		if rel, relErr := filepath.Rel(wd, file); relErr == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}

	return filepath.ToSlash(file)
}
