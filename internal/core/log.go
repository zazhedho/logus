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
	"sync"
)

type userIDContextKey struct{}

// callerSkip counts frames from callerAttrs to the user code: callerAttrs,
// emit, Log (or Logf), the public logus function, the caller. Public entry
// points must call Log or Logf directly.
const callerSkip = 4

// WithUserID returns a copy of ctx carrying the user id. A nil ctx is treated
// as context.Background().
func WithUserID(ctx context.Context, userID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, userIDContextKey{}, userID)
}

// UserID returns the id stored by WithUserID or, failing that, the value under
// the gin-style key CtxKeyUserID. It is empty when neither is present.
func UserID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(userIDContextKey{}).(string); ok && id != "" {
		return id
	}
	if value, exists := ctxGet(ctx, CtxKeyUserID); exists {
		return interfaceString(value)
	}
	return ""
}

// enabled reports whether a line at level would be written.
func enabled(level Level) bool {
	if !level.valid() {
		return false
	}
	getLogger()
	ensureLevel()
	return level >= Level(levelVar.Level())
}

// joinArgs formats args like fmt.Sprint but always separates operands with a
// single space.
func joinArgs(args []any) string {
	return strings.TrimSuffix(fmt.Sprintln(args...), "\n")
}

// Log writes args joined by single spaces when level is at or above the
// threshold; unknown levels are ignored. Call it directly from the public
// function (see callerSkip).
func Log(ctx context.Context, level Level, args ...any) {
	if !enabled(level) {
		return
	}
	emit(ctx, level, joinArgs(args))
}

// Logf is Log with a fmt.Sprintf-style message. Call it directly from the
// public function (see callerSkip).
func Logf(ctx context.Context, level Level, format string, args ...any) {
	if !enabled(level) {
		return
	}
	emit(ctx, level, fmt.Sprintf(format, args...))
}

func emit(ctx context.Context, level Level, msg string) {
	st := getStatic()
	attrs := make([]slog.Attr, 0, 8)
	if st.serverIP != "" {
		attrs = append(attrs, slog.String("server_ip", st.serverIP))
	}
	attrs = append(attrs, callerAttrs(callerSkip, st.wd)...)
	if st.node != "" {
		attrs = append(attrs, slog.String("node", st.node))
	}
	if ctx != nil {
		attrs = append(attrs, slog.String("log_id", RequestID(ctx).String()))
		if userID := UserID(ctx); userID != "" {
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
	getLogger().Log(logCtx, slog.Level(level), msg, fields...)
}

func callerAttrs(skip int, wd string) []slog.Attr {
	pc, file, line, ok := runtime.Caller(skip)
	if !ok {
		return nil
	}

	sourceFile := normalizeSourceFile(file, wd)
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

func normalizeSourceFile(file, wd string) string {
	if wd != "" {
		if rel, err := filepath.Rel(wd, file); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(file)
}

// static holds values that do not change for the life of the process.
type static struct {
	wd, serverIP, node string
}

var (
	staticOnce sync.Once
	staticVals static
)

// getStatic resolves the working directory, SERVER_IP (falling back to the
// legacy ServerIP) and NODE once, on first use.
func getStatic() static {
	staticOnce.Do(func() {
		wd, _ := os.Getwd()
		ip := getEnv("SERVER_IP", "")
		if ip == "" {
			ip = getEnv("ServerIP", "")
		}
		staticVals = static{wd: wd, serverIP: ip, node: getEnv("NODE", "")}
	})
	return staticVals
}
