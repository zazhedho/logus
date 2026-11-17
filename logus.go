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

const (
	// CtxKeyID is the gin-style context key under which the request id is stored.
	CtxKeyID = "CTX_ID"
	// CtxKeyUserID is the gin-style context key under which the user id is stored.
	CtxKeyUserID = "userId"
)

// WriteLog writes a log line at the given level. When LOG_LEVEL is unset the
// threshold is LogLevelWarn. It logs with context.Background(), so the line
// still carries a log_id: a freshly generated id per call, since none is
// stored.
func WriteLog(level int, msg ...any) {
	// core.Log must be called directly from here: its caller-depth
	// assumes exactly one layer between it and the user's code.
	core.Log(context.Background(), core.LevelWarn, level, msg...)
}

// WriteLogWithContext writes a log line enriched with the log id and user id
// found in ctx. When LOG_LEVEL is unset the threshold is LogLevelDebug.
func WriteLogWithContext(ctx context.Context, level int, msg ...any) {
	// See WriteLog: keep this a direct call.
	core.Log(ctx, core.LevelDebug, level, msg...)
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

// RequestIDFromContext returns the request id stored by WithRequestID.
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
