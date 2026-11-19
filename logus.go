// Package logus is structured logging on log/slog: each line carries server
// IP, node name, caller, log id and optional user id. gin contexts are
// recognized by duck typing; no web framework dependency.
package logus

import (
	"context"
	"io"

	"github.com/google/uuid"

	"github.com/zazhedho/logus/internal/core"
)

// Level is a log severity. The values are aligned with log/slog. It is an
// alias of the implementation type, so there is a single definition.
type Level = core.Level

// The six standard levels, trace < debug < info < warn < error < fatal; calls
// below the threshold (LOG_LEVEL or SetLevel) are dropped.
const (
	// LevelTrace is the finest-grained tracing, normally off.
	LevelTrace = core.LevelTrace
	// LevelDebug is diagnostic detail for development.
	LevelDebug = core.LevelDebug
	// LevelInfo is a normal, significant event.
	LevelInfo = core.LevelInfo
	// LevelWarn means something is off but the service continues.
	LevelWarn = core.LevelWarn
	// LevelError means an operation failed and needs attention.
	LevelError = core.LevelError
	// LevelFatal marks an unrecoverable condition. It only labels the
	// line: logus never exits, panics or stops anything.
	LevelFatal = core.LevelFatal
)

// Gin-style context keys, as used by Get and Set on a *gin.Context.
const (
	// CtxKeyRequestID is the key under which the request id is stored.
	CtxKeyRequestID = core.CtxKeyRequestID
	// CtxKeyUserID is the key under which the user id is stored.
	CtxKeyUserID = core.CtxKeyUserID
)

// SetLevel changes the threshold at runtime. It is safe for concurrent use.
// Called before the first log it takes precedence over LOG_LEVEL. A value that
// is not one of the six levels is ignored and the threshold is left unchanged.
func SetLevel(l Level) {
	core.SetLevel(l)
}

// GetLevel returns the current threshold, reading LOG_LEVEL first if the
// logger has not been used yet.
func GetLevel() Level {
	return core.GetLevel()
}

// SetOutput redirects all log lines to w; nil restores os.Stdout. It is safe
// for concurrent use and takes effect for subsequent lines.
func SetOutput(w io.Writer) {
	core.SetOutput(w)
}

// SetFormat selects the output format: "json", "text" or "string",
// case-insensitive. An empty name means json; an unknown one falls back to json
// and writes one warning. Called before the first log it takes precedence over
// LOG_FORMAT.
func SetFormat(format string) {
	core.SetFormat(format)
}

// The logging functions below call core directly, one layer between core and
// the user's code, so the reported caller is correct. Do not add wrappers.

// Log logs args joined by single spaces at level, with context.Background(), so
// log_id is freshly generated per call. Levels outside the six standard ones
// are ignored.
func Log(level Level, args ...any) {
	core.Log(context.Background(), level, args...)
}

// Logf is Log with a fmt.Sprintf-style message.
func Logf(level Level, format string, args ...any) {
	core.Logf(context.Background(), level, format, args...)
}

// LogContext is Log enriched with the log id and user id found in ctx. The
// threshold is the same as for Log.
func LogContext(ctx context.Context, level Level, args ...any) {
	core.Log(ctx, level, args...)
}

// LogContextf is LogContext with a fmt.Sprintf-style message.
func LogContextf(ctx context.Context, level Level, format string, args ...any) {
	core.Logf(ctx, level, format, args...)
}

// WithRequestID returns a copy of ctx carrying id. A nil ctx is treated as
// context.Background().
func WithRequestID(ctx context.Context, id uuid.UUID) context.Context {
	return core.WithRequestID(ctx, id)
}

// RequestIDFromContext returns the id stored by WithRequestID. ok is false when
// ctx is nil or holds no id, or the stored id is the zero UUID.
func RequestIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	return core.RequestIDFromContext(ctx)
}

// RequestID returns the request id carried by ctx, or a newly generated one
// (stored via ctx.Set when available). It reads WithRequestID or the gin-style
// CtxKeyRequestID.
func RequestID(ctx context.Context) uuid.UUID {
	return core.RequestID(ctx)
}

// WithUserID returns a copy of ctx carrying the user id. A nil ctx is treated
// as context.Background().
func WithUserID(ctx context.Context, userID string) context.Context {
	return core.WithUserID(ctx, userID)
}

// UserID returns the id stored by WithUserID or, failing that, the value under
// the gin-style key CtxKeyUserID. It is empty when neither is present.
func UserID(ctx context.Context) string {
	return core.UserID(ctx)
}
