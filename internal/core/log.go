// Package core holds the implementation behind the public logus API. It is
// internal so the repository root exposes nothing but the logus package.
package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type logIDContextKey struct{}
type userIDContextKey struct{}

// callerSkip is the runtime.Caller depth, counted from callerAttrs, of the
// user code calling the public logus.WriteLog or logus.WriteLogWithContext:
// callerAttrs (0), Log (1), the public logus function (2), the caller (3).
// Every public entry point must call Log directly.
const callerSkip = 3

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

// Log writes one log line when level is at or above the threshold. Levels
// that are not one of the six standard ones are ignored. It must be called
// directly by the public logus function so the reported source location is
// that function's caller.
func Log(ctx context.Context, level Level, msg ...any) {
	if !level.valid() {
		return
	}

	logger := getLogger()
	ensureLevel()
	if level < Level(levelVar.Level()) {
		return
	}

	attrs := []slog.Attr{
		slog.String("server_ip", getEnv("ServerIP", "")),
	}
	attrs = append(attrs, callerAttrs(callerSkip)...)
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

	logger.Log(logCtx, slog.Level(level), fmt.Sprint(msg...), fields...)
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
