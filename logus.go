// Package logus is a small structured logging module built on log/slog. Each
// line carries the server IP, node name, caller location, a log id and an
// optional user id. It recognizes gin-style contexts through duck typing and
// has no web framework dependency.
package logus

import (
	"context"

	"github.com/google/uuid"

	"github.com/zazhedho/logus/internal/core"
)

// Level is a log severity. The values are aligned with log/slog.
type Level int

// The six standard levels, ordered trace < debug < info < warn < error <
// fatal. A call is written when its level is at or above the configured
// threshold (LOG_LEVEL, or SetLevel).
const (
	// LogLevelTrace is the finest-grained tracing, normally off.
	LogLevelTrace Level = -8
	// LogLevelDebug is diagnostic detail for development.
	LogLevelDebug Level = -4
	// LogLevelInfo is a normal, significant event.
	LogLevelInfo Level = 0
	// LogLevelWarn means something is off but the service continues.
	LogLevelWarn Level = 4
	// LogLevelError means an operation failed and needs attention.
	LogLevelError Level = 8
	// LogLevelFatal marks an unrecoverable condition. It only labels the
	// line: logus never exits, panics or stops anything.
	LogLevelFatal Level = 12
)

// String returns TRACE, DEBUG, INFO, WARN, ERROR or FATAL.
func (l Level) String() string {
	return core.Level(l).String()
}

const (
	// CtxKeyID is the gin-style context key under which the request id is stored.
	CtxKeyID = "CTX_ID"
	// CtxKeyUserID is the gin-style context key under which the user id is stored.
	CtxKeyUserID = "userId"
)

// SetLevel changes the threshold at runtime. It is safe for concurrent use.
// Called before the first log it takes precedence over LOG_LEVEL.
func SetLevel(l Level) {
	core.SetLevel(core.Level(l))
}

// GetLevel returns the current threshold, reading LOG_LEVEL first if the
// logger has not been used yet.
func GetLevel() Level {
	return Level(core.GetLevel())
}

// WriteLog writes a log line at the given level when it is at or above the
// threshold (default info). It logs with context.Background(), so the line
// still carries a log_id: a freshly generated id per call, since none is
// stored. Levels other than the six standard ones are ignored.
func WriteLog(level Level, msg ...any) {
	// core.Log must be called directly from here: its caller-depth
	// assumes exactly one layer between it and the user's code.
	core.Log(context.Background(), core.Level(level), msg...)
}

// WriteLogWithContext writes a log line enriched with the log id and user id
// found in ctx. The threshold is the same as for WriteLog.
func WriteLogWithContext(ctx context.Context, level Level, msg ...any) {
	// See WriteLog: keep this a direct call.
	core.Log(ctx, core.Level(level), msg...)
}

// WithLogMetadata returns a copy of ctx carrying an explicit log id and user
// id. Empty values do not overwrite existing ones.
func WithLogMetadata(ctx context.Context, logID, userID string) context.Context {
	return core.WithLogMetadata(ctx, logID, userID)
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

// RequestID resolves the request id for ctx. It looks, in order, at the typed
// value set by WithRequestID, a Get method keyed by CtxKeyID, and a plain
// string context key CtxKeyID. Get may have the signature Get(string) (any,
// bool) or Get(any) (any, bool), the latter being *gin.Context in gin v1.11
// and later. Values may be a uuid.UUID or a parsable string. When nothing
// valid is found a new id is generated and, if ctx has a Set(string, any) or
// Set(any, any) method, stored under CtxKeyID.
func RequestID(ctx context.Context) uuid.UUID {
	return core.RequestID(ctx)
}
