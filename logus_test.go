package logus_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/zazhedho/logus"
	"github.com/zazhedho/logus/internal/core"
)

// logFile is where the process-wide logger writes during these tests. The
// logger binds to os.Stdout on first use and reads LOG_FORMAT only once, so
// TestMain points os.Stdout at a file, forces the JSON format, triggers
// logger creation, and restores os.Stdout.
var logFile *os.File

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "logus-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	f, err := os.CreateTemp(dir, "out-*.log")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logFile = f

	origStdout := os.Stdout
	os.Stdout = f
	_ = os.Setenv("LOG_FORMAT", "json")
	_ = os.Setenv("LOG_LEVEL", "0")
	logus.WriteLog(logus.LogLevelPanic, "logger warm-up")
	_ = os.Unsetenv("LOG_LEVEL")
	os.Stdout = origStdout

	code := m.Run()
	_ = f.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// fakeGinContext mimics the Get/Set surface of *gin.Context without importing gin.
type fakeGinContext struct {
	context.Context
	values map[string]any
}

func newFakeGinContext() *fakeGinContext {
	return &fakeGinContext{Context: context.Background(), values: map[string]any{}}
}

func (f *fakeGinContext) Get(key string) (any, bool) {
	v, ok := f.values[key]
	return v, ok
}

func (f *fakeGinContext) Set(key string, value any) {
	f.values[key] = value
}

// fakeAnyKeyContext mimics the Get/Set surface of *gin.Context from gin v1.11
// (any-typed keys) without importing gin. Its Value method is inherited from
// the embedded context and never consults the map, so tests exercise the
// Get/Set path rather than the ctx.Value fallback.
type fakeAnyKeyContext struct {
	context.Context
	values map[any]any
}

func newFakeAnyKeyContext() *fakeAnyKeyContext {
	return &fakeAnyKeyContext{Context: context.Background(), values: map[any]any{}}
}

func (f *fakeAnyKeyContext) Get(key any) (any, bool) {
	v, ok := f.values[key]
	return v, ok
}

func (f *fakeAnyKeyContext) Set(key any, value any) {
	f.values[key] = value
}

// capture reads the log lines written since it was created or last reset.
type capture struct {
	t      *testing.T
	offset int64
}

func captureLogs(t *testing.T) *capture {
	t.Helper()
	c := &capture{t: t}
	c.reset()
	return c
}

func (c *capture) reset() {
	c.t.Helper()
	st, err := logFile.Stat()
	if err != nil {
		c.t.Fatalf("stat log file: %v", err)
	}
	c.offset = st.Size()
}

func (c *capture) lines() []string {
	c.t.Helper()
	data, err := os.ReadFile(logFile.Name())
	if err != nil {
		c.t.Fatalf("read log file: %v", err)
	}
	text := strings.TrimSpace(string(data[c.offset:]))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// last parses the last JSON line; it fails the test when there is none.
func (c *capture) last(t *testing.T) map[string]any {
	t.Helper()
	lines := c.lines()
	if len(lines) == 0 {
		t.Fatal("no log output")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("parse %q: %v", lines[len(lines)-1], err)
	}
	return m
}

// unsetEnv removes key for the duration of the test and restores the old
// value via t.Cleanup.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// TestPublicConstantsMatchCore guards the literal constant values declared in
// logus.go against the internal implementation's copies.
func TestPublicConstantsMatchCore(t *testing.T) {
	pairs := [][2]any{
		{logus.LogLevelPanic, core.LevelPanic}, {logus.LogLevelError, core.LevelError},
		{logus.LogLevelFail, core.LevelFail}, {logus.LogLevelInfo, core.LevelInfo},
		{logus.LogLevelData, core.LevelData}, {logus.LogLevelWarn, core.LevelWarn},
		{logus.LogLevelDebug, core.LevelDebug},
		{logus.CtxKeyID, core.CtxKeyID}, {logus.CtxKeyUserID, core.CtxKeyUserID},
	}
	for _, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("public %v != core %v", p[0], p[1])
		}
	}
}

// TestConcurrentWritesThroughPublicAPI checks that parallel WriteLog calls
// each produce one whole JSON line.
func TestConcurrentWritesThroughPublicAPI(t *testing.T) {
	t.Setenv("LOG_LEVEL", "6")
	buf := captureLogs(t)
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				logus.WriteLogWithContext(context.Background(), logus.LogLevelInfo, "complete-message")
			}
		}()
	}
	wg.Wait()
	lines := buf.lines()
	if len(lines) != 400 {
		t.Fatalf("lines = %d, want 400", len(lines))
	}
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("torn line %q: %v", line, err)
		}
	}
}

func TestWriteLogDropsDebugWhenLogLevelUnset(t *testing.T) {
	buf := captureLogs(t)
	unsetEnv(t, "LOG_LEVEL")
	logus.WriteLog(logus.LogLevelDebug, "d")
	if len(buf.lines()) != 0 {
		t.Fatalf("expected no output, got %q", strings.Join(buf.lines(), "\n"))
	}
	logus.WriteLog(logus.LogLevelWarn, "w")
	if n := len(buf.lines()); n != 1 {
		t.Fatalf("expected 1 line, got %d", n)
	}
	if m := buf.last(t); m["msg"] != "w" {
		t.Fatalf("msg = %v", m["msg"])
	}
}

func TestWriteLogWithContextLogsDebugWhenLogLevelUnset(t *testing.T) {
	buf := captureLogs(t)
	unsetEnv(t, "LOG_LEVEL")
	logus.WriteLogWithContext(context.Background(), logus.LogLevelDebug, "d")
	if m := buf.last(t); m["msg"] != "d" {
		t.Fatalf("msg = %v", m["msg"])
	}
}

func TestLogLevelEnvFilters(t *testing.T) {
	buf := captureLogs(t)
	t.Setenv("LOG_LEVEL", "3")
	for _, lvl := range []int{logus.LogLevelWarn, logus.LogLevelData} {
		logus.WriteLog(lvl, "x")
		if len(buf.lines()) != 0 {
			t.Fatalf("level %d should be dropped, got %q", lvl, strings.Join(buf.lines(), "\n"))
		}
	}
	for _, lvl := range []int{logus.LogLevelInfo, logus.LogLevelFail, logus.LogLevelError, logus.LogLevelPanic} {
		buf.reset()
		logus.WriteLog(lvl, "x")
		if len(buf.lines()) == 0 {
			t.Fatalf("level %d should be logged", lvl)
		}
	}
}

func TestLogLevelWhitespaceIsTrimmed(t *testing.T) {
	buf := captureLogs(t)
	t.Setenv("LOG_LEVEL", " 6 ")
	logus.WriteLog(logus.LogLevelDebug, "d")
	if len(buf.lines()) == 0 {
		t.Fatal("debug should be logged")
	}
}

func TestLogLevelNotANumberLogsOnlyPanic(t *testing.T) {
	for _, v := range []string{"abc", ""} {
		t.Run(fmt.Sprintf("%q", v), func(t *testing.T) {
			buf := captureLogs(t)
			t.Setenv("LOG_LEVEL", v)
			logus.WriteLog(logus.LogLevelError, "e")
			if len(buf.lines()) != 0 {
				t.Fatalf("error should be dropped, got %q", strings.Join(buf.lines(), "\n"))
			}
			logus.WriteLog(logus.LogLevelPanic, "p")
			if m := buf.last(t); m["msg"] != "p" {
				t.Fatalf("msg = %v", m["msg"])
			}
		})
	}
}

func TestUnknownLevelIsIgnored(t *testing.T) {
	buf := captureLogs(t)
	t.Setenv("LOG_LEVEL", "6")
	logus.WriteLog(999, "x")
	logus.WriteLog(-1, "x")
	if len(buf.lines()) != 0 {
		t.Fatalf("expected no output, got %q", strings.Join(buf.lines(), "\n"))
	}
}

func TestMessageArgsAreJoinedWithSprint(t *testing.T) {
	buf := captureLogs(t)
	t.Setenv("LOG_LEVEL", "6")
	err := errors.New("boom")
	logus.WriteLog(logus.LogLevelInfo, "a", 1, err, nil)
	if got, want := buf.last(t)["msg"], fmt.Sprint("a", 1, err, nil); got != want {
		t.Fatalf("msg = %q, want %q", got, want)
	}
}

func TestSlogLevelMapping(t *testing.T) {
	t.Setenv("LOG_LEVEL", "6")
	want := map[int]string{
		logus.LogLevelPanic: "ERROR", logus.LogLevelError: "ERROR", logus.LogLevelWarn: "WARN",
		logus.LogLevelDebug: "DEBUG", logus.LogLevelFail: "INFO", logus.LogLevelInfo: "INFO", logus.LogLevelData: "INFO",
	}
	for lvl, name := range want {
		buf := captureLogs(t)
		logus.WriteLog(lvl, "x")
		if got := buf.last(t)["level"]; got != name {
			t.Errorf("level %d -> %v, want %s", lvl, got, name)
		}
	}
}

func TestServerIPAndNodeFields(t *testing.T) {
	t.Setenv("LOG_LEVEL", "6")
	t.Run("set", func(t *testing.T) {
		buf := captureLogs(t)
		t.Setenv("ServerIP", "10.0.0.1")
		t.Setenv("NODE", "api-1")
		logus.WriteLog(logus.LogLevelInfo, "x")
		m := buf.last(t)
		if m["server_ip"] != "10.0.0.1" || m["node"] != "api-1" {
			t.Fatalf("got server_ip=%v node=%v", m["server_ip"], m["node"])
		}
	})
	t.Run("unset", func(t *testing.T) {
		buf := captureLogs(t)
		unsetEnv(t, "ServerIP")
		unsetEnv(t, "NODE")
		logus.WriteLog(logus.LogLevelInfo, "x")
		m := buf.last(t)
		if _, ok := m["node"]; ok {
			t.Fatal("node key should be absent")
		}
		if v, ok := m["server_ip"]; !ok || v != "" {
			t.Fatalf("server_ip = %v (present=%v), want empty string", v, ok)
		}
	})
}

func TestSourceAttributionPointsAtCaller(t *testing.T) {
	t.Setenv("LOG_LEVEL", "6")
	var wantLine int
	calls := map[string]func(){
		"WriteLog": func() {
			_, _, markLine, _ := runtime.Caller(0)
			wantLine = markLine + 2 // the call sits two lines below the mark
			logus.WriteLog(logus.LogLevelInfo, "x")
		},
		"WriteLogWithContext": func() {
			_, _, markLine, _ := runtime.Caller(0)
			wantLine = markLine + 2 // the call sits two lines below the mark
			logus.WriteLogWithContext(context.Background(), logus.LogLevelInfo, "x")
		},
	}
	for name, call := range calls {
		buf := captureLogs(t)
		call()
		m := buf.last(t)
		f, _ := m["source_file"].(string)
		// The reported file must be this test file, not the delegating
		// logus.go or anything under internal/.
		if f != "logus_test.go" || strings.Contains(f, "internal/") || strings.HasSuffix(f, "/logus.go") || f == "logus.go" {
			t.Errorf("%s: source_file = %q, want logus_test.go", name, f)
		}
		if fn, _ := m["source_function"].(string); !strings.Contains(fn, "TestSourceAttributionPointsAtCaller") {
			t.Errorf("%s: source_function = %v", name, m["source_function"])
		}
		if l, _ := m["source_line"].(float64); int(l) != wantLine {
			t.Errorf("%s: source_line = %v, want %d", name, m["source_line"], wantLine)
		}
	}
}

func TestLogIDFromMetadata(t *testing.T) {
	buf := captureLogs(t)
	t.Setenv("LOG_LEVEL", "6")
	ctx := logus.WithLogMetadata(logus.WithRequestID(context.Background(), uuid.New()), "log-1", "user-1")
	logus.WriteLogWithContext(ctx, logus.LogLevelInfo, "x")
	m := buf.last(t)
	if m["log_id"] != "log-1" || m["user_id"] != "user-1" {
		t.Fatalf("got log_id=%v user_id=%v", m["log_id"], m["user_id"])
	}
}

func TestLogIDFallsBackToRequestID(t *testing.T) {
	buf := captureLogs(t)
	t.Setenv("LOG_LEVEL", "6")
	id := uuid.New()
	logus.WriteLogWithContext(logus.WithRequestID(context.Background(), id), logus.LogLevelInfo, "x")
	if got := buf.last(t)["log_id"]; got != id.String() {
		t.Fatalf("log_id = %v, want %v", got, id)
	}
}

func TestUserIDFromGetter(t *testing.T) {
	buf := captureLogs(t)
	t.Setenv("LOG_LEVEL", "6")
	f := newFakeGinContext()
	f.Set(logus.CtxKeyUserID, "user-9")
	logus.WriteLogWithContext(f, logus.LogLevelInfo, "x")
	m := buf.last(t)
	if m["user_id"] != "user-9" {
		t.Fatalf("user_id = %v", m["user_id"])
	}
	stored, _ := f.Get(logus.CtxKeyID)
	if m["log_id"] != fmt.Sprint(stored) || m["log_id"] == "" {
		t.Fatalf("log_id = %v, stored = %v", m["log_id"], stored)
	}
}

func TestUserIDFromAnyKeyContext(t *testing.T) {
	buf := captureLogs(t)
	t.Setenv("LOG_LEVEL", "6")
	f := newFakeAnyKeyContext()
	f.Set(logus.CtxKeyUserID, "user-9")
	logus.WriteLogWithContext(f, logus.LogLevelInfo, "x")
	m := buf.last(t)
	if m["user_id"] != "user-9" {
		t.Fatalf("user_id = %v", m["user_id"])
	}
	stored, _ := f.Get(logus.CtxKeyID)
	if m["log_id"] != fmt.Sprint(stored) || m["log_id"] == "" {
		t.Fatalf("log_id = %v, stored = %v", m["log_id"], stored)
	}
}

func TestNoUserIDFieldWhenAbsent(t *testing.T) {
	buf := captureLogs(t)
	t.Setenv("LOG_LEVEL", "6")
	logus.WriteLogWithContext(context.Background(), logus.LogLevelInfo, "x")
	m := buf.last(t)
	if _, ok := m["user_id"]; ok {
		t.Fatal("user_id should be absent")
	}
	if s, _ := m["log_id"].(string); s == "" {
		t.Fatalf("log_id = %v, want non-empty", m["log_id"])
	}
}

func TestNilContextDoesNotPanic(t *testing.T) {
	buf := captureLogs(t)
	t.Setenv("LOG_LEVEL", "6")
	//nolint:staticcheck // nil context is deliberately tolerated
	logus.WriteLogWithContext(nil, logus.LogLevelInfo, "x")
	m := buf.last(t)
	if m["msg"] != "x" {
		t.Fatalf("msg = %v", m["msg"])
	}
	if _, ok := m["log_id"]; ok {
		t.Fatal("log_id should be absent for nil context")
	}
}

func TestWithLogMetadata(t *testing.T) {
	buf := captureLogs(t)
	t.Setenv("LOG_LEVEL", "6")
	//nolint:staticcheck // nil context is deliberately tolerated
	if ctx := logus.WithLogMetadata(nil, "l", "u"); ctx == nil {
		t.Fatal("nil ctx result")
	}
	ctx := logus.WithLogMetadata(logus.WithLogMetadata(context.Background(), "l", "u"), "", "")
	logus.WriteLogWithContext(ctx, logus.LogLevelInfo, "x")
	m := buf.last(t)
	if m["log_id"] != "l" || m["user_id"] != "u" {
		t.Fatalf("got log_id=%v user_id=%v", m["log_id"], m["user_id"])
	}
}

func TestRequestIDFromContextReturnsStoredID(t *testing.T) {
	id := uuid.New()
	got, ok := logus.RequestIDFromContext(logus.WithRequestID(context.Background(), id))
	if !ok || got != id {
		t.Fatalf("got (%v, %v), want (%v, true)", got, ok, id)
	}
}

func TestRequestIDFromContextMissing(t *testing.T) {
	if got, ok := logus.RequestIDFromContext(context.Background()); ok || got != uuid.Nil {
		t.Fatalf("background: got (%v, %v), want (Nil, false)", got, ok)
	}
	//nolint:staticcheck // nil context is deliberately tolerated
	if got, ok := logus.RequestIDFromContext(nil); ok || got != uuid.Nil {
		t.Fatalf("nil: got (%v, %v), want (Nil, false)", got, ok)
	}
}

func TestWithRequestIDNilContext(t *testing.T) {
	id := uuid.New()
	//nolint:staticcheck // nil context is deliberately tolerated
	ctx := logus.WithRequestID(nil, id)
	got, ok := logus.RequestIDFromContext(ctx)
	if !ok || got != id {
		t.Fatalf("got (%v, %v), want (%v, true)", got, ok, id)
	}
}

func TestRequestIDPrefersTypedContextValue(t *testing.T) {
	a := uuid.New()
	ctx := logus.WithRequestID(context.Background(), a)
	if got := logus.RequestID(ctx); got != a {
		t.Fatalf("got %v, want %v", got, a)
	}
}

func TestRequestIDReadsUUIDFromGetter(t *testing.T) {
	a := uuid.New()
	f := newFakeGinContext()
	f.Set(logus.CtxKeyID, a)
	if got := logus.RequestID(f); got != a {
		t.Fatalf("got %v, want %v", got, a)
	}
}

func TestRequestIDParsesStringFromGetter(t *testing.T) {
	a := uuid.New()
	f := newFakeGinContext()
	f.Set(logus.CtxKeyID, a.String())
	if got := logus.RequestID(f); got != a {
		t.Fatalf("got %v, want %v", got, a)
	}
}

func TestRequestIDReadsPlainStringKey(t *testing.T) {
	a := uuid.New()
	//nolint:staticcheck // plain string key is the legacy behavior under test
	ctx := context.WithValue(context.Background(), logus.CtxKeyID, a)
	if got := logus.RequestID(ctx); got != a {
		t.Fatalf("got %v, want %v", got, a)
	}
}

func TestRequestIDGeneratesAndStoresInSetter(t *testing.T) {
	f := newFakeGinContext()
	got := logus.RequestID(f)
	if got == uuid.Nil {
		t.Fatal("expected generated id")
	}
	if v, _ := f.Get(logus.CtxKeyID); v != got {
		t.Fatalf("stored %v, want %v", v, got)
	}
	if again := logus.RequestID(f); again != got {
		t.Fatalf("not stable: %v vs %v", again, got)
	}
}

func TestRequestIDReplacesInvalidStoredString(t *testing.T) {
	f := newFakeGinContext()
	f.Set(logus.CtxKeyID, "not-a-uuid")
	got := logus.RequestID(f)
	if got == uuid.Nil {
		t.Fatal("expected generated id")
	}
	if v, _ := f.Get(logus.CtxKeyID); v != got {
		t.Fatalf("stored %v, want %v", v, got)
	}
}

func TestRequestIDPlainContextGeneratesEachCall(t *testing.T) {
	ctx := context.Background()
	a, b := logus.RequestID(ctx), logus.RequestID(ctx)
	if a == uuid.Nil || b == uuid.Nil || a == b {
		t.Fatalf("expected two distinct non-nil ids, got %v and %v", a, b)
	}
}

func TestRequestIDNilContext(t *testing.T) {
	//nolint:staticcheck // nil context is deliberately tolerated
	if got := logus.RequestID(nil); got == uuid.Nil {
		t.Fatal("expected generated id")
	}
}

func TestRequestIDTreatsStoredNilUUIDAsMissing(t *testing.T) {
	t.Run("typed context value", func(t *testing.T) {
		if got := logus.RequestID(logus.WithRequestID(context.Background(), uuid.Nil)); got == uuid.Nil {
			t.Fatal("expected generated id")
		}
	})
	t.Run("getter value", func(t *testing.T) {
		f := newFakeGinContext()
		f.Set(logus.CtxKeyID, uuid.Nil)
		got := logus.RequestID(f)
		if got == uuid.Nil {
			t.Fatal("expected generated id")
		}
		if v, _ := f.Get(logus.CtxKeyID); v != got {
			t.Fatalf("stored %v, want %v", v, got)
		}
	})
}

func TestRequestIDStoresGeneratedIDOnAnyKeyContext(t *testing.T) {
	f := newFakeAnyKeyContext()
	got := logus.RequestID(f)
	if got == uuid.Nil {
		t.Fatal("expected generated id")
	}
	if v, _ := f.Get(logus.CtxKeyID); v != got {
		t.Fatalf("stored %v, want %v", v, got)
	}
	if again := logus.RequestID(f); again != got {
		t.Fatalf("not stable: %v vs %v", again, got)
	}
}

func TestRequestIDReadsFromAnyKeyContext(t *testing.T) {
	a := uuid.New()
	f := newFakeAnyKeyContext()
	f.Set(logus.CtxKeyID, a)
	if got := logus.RequestID(f); got != a {
		t.Fatalf("uuid value: got %v, want %v", got, a)
	}

	b := uuid.New()
	g := newFakeAnyKeyContext()
	g.Set(logus.CtxKeyID, b.String())
	if got := logus.RequestID(g); got != b {
		t.Fatalf("string value: got %v, want %v", got, b)
	}
}
