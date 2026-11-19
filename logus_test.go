package logus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/zazhedho/logus"
	"github.com/zazhedho/logus/internal/core"
)

// logFile receives the process-wide logger's output: TestMain points it there
// with SetOutput and forces JSON with SetFormat.
var logFile *os.File

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		runHelper(mode)
		os.Exit(0)
	}
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
	logus.SetOutput(f)
	logus.SetFormat("json")
	_ = os.Unsetenv("LOG_LEVEL")
	logus.SetLevel(logus.LevelTrace)

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

// fakeAnyKeyContext mimics *gin.Context (gin v1.11, any-typed keys) without gin;
// Value ignores the map, so tests exercise the Get/Set path.
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

// TestConcurrentWritesThroughPublicAPI checks that parallel WriteLog calls
// each produce one whole JSON line.
func TestConcurrentWritesThroughPublicAPI(t *testing.T) {
	buf := captureLogs(t)
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				logus.LogContext(context.Background(), logus.LevelInfo, "complete-message")
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

func TestMessageArgsAreJoinedWithSingleSpaces(t *testing.T) {
	buf := captureLogs(t)
	err := errors.New("boom")
	ctx := context.Background()
	for name, call := range map[string]func(){
		"Log":        func() { logus.Log(logus.LevelInfo, "a", "b", 1, 2, err, nil) },
		"LogContext": func() { logus.LogContext(ctx, logus.LevelInfo, "a", "b", 1, 2, err, nil) },
	} {
		buf.reset()
		call()
		if got, want := buf.last(t)["msg"], "a b 1 2 boom <nil>"; got != want {
			t.Errorf("%s: msg = %q, want %q", name, got, want)
		}
	}
}

func TestFormattedVariants(t *testing.T) {
	buf := captureLogs(t)
	ctx := logus.WithRequestID(context.Background(), uuid.New())
	logus.Logf(logus.LevelWarn, "took %dms for %s", 12, "q")
	if m := buf.last(t); m["msg"] != "took 12ms for q" || m["level"] != "WARN" {
		t.Fatalf("Logf: %v", m)
	}
	buf.reset()
	logus.LogContextf(ctx, logus.LevelError, "user %s failed", "u1")
	if m := buf.last(t); m["msg"] != "user u1 failed" || m["level"] != "ERROR" || m["log_id"] != logus.RequestID(ctx).String() {
		t.Fatalf("LogContextf: %v", m)
	}
}

func TestFormattedVariantsHonourThresholdAndInvalidLevels(t *testing.T) {
	t.Cleanup(func() { logus.SetLevel(logus.LevelTrace) })
	buf := captureLogs(t)
	logus.SetLevel(logus.LevelError)
	logus.Logf(logus.LevelInfo, "dropped %d", 1)
	logus.LogContextf(context.Background(), logus.LevelInfo, "dropped %d", 2)
	logus.Logf(logus.Level(3), "invalid")
	if got := buf.lines(); len(got) != 0 {
		t.Fatalf("expected no output, got %q", got)
	}
}

func TestUserIDHelpers(t *testing.T) {
	ctx := logus.WithUserID(context.Background(), "u-1")
	if got := logus.UserID(ctx); got != "u-1" {
		t.Fatalf("UserID = %q", got)
	}
	if got := logus.UserID(context.Background()); got != "" {
		t.Fatalf("UserID without value = %q", got)
	}
	if got := logus.UserID(nil); got != "" { //nolint:staticcheck // nil context is supported
		t.Fatalf("UserID(nil) = %q", got)
	}
	if ctx := logus.WithUserID(nil, "x"); ctx == nil { //nolint:staticcheck // nil context is supported
		t.Fatal("WithUserID(nil) returned nil")
	}
	f := newFakeGinContext()
	f.Set(logus.CtxKeyUserID, "gin-user")
	if got := logus.UserID(f); got != "gin-user" {
		t.Fatalf("UserID via Get = %q", got)
	}
	// An explicit WithUserID wins over the gin-style key.
	if got := logus.UserID(logus.WithUserID(f, "explicit")); got != "explicit" {
		t.Fatalf("UserID = %q, want explicit", got)
	}
}

func TestWithUserIDAppearsInLog(t *testing.T) {
	buf := captureLogs(t)
	logus.LogContext(logus.WithUserID(context.Background(), "u-7"), logus.LevelInfo, "x")
	if got := buf.last(t)["user_id"]; got != "u-7" {
		t.Fatalf("user_id = %v", got)
	}
}

func TestLevelAndCtxKeysAreAliasesOfCore(t *testing.T) {
	// Passing across the two names without conversion proves a type alias.
	viaCore := func(l core.Level) logus.Level { return l }
	viaPublic := func(l logus.Level) core.Level { return l }
	if viaCore(core.LevelWarn) != logus.LevelWarn || viaPublic(logus.LevelError) != core.LevelError {
		t.Fatal("alias values differ")
	}
}

func TestServerIPAndNodeFields(t *testing.T) {
	cases := []struct {
		name         string
		env          []string
		wantIP, node *string
	}{
		{"SERVER_IP and NODE", []string{"SERVER_IP=10.0.0.1", "NODE=api-1"}, str("10.0.0.1"), str("api-1")},
		{"legacy ServerIP", []string{"ServerIP=10.0.0.2"}, str("10.0.0.2"), nil},
		{"SERVER_IP wins", []string{"SERVER_IP=10.0.0.1", "ServerIP=10.9.9.9"}, str("10.0.0.1"), nil},
		{"unset omits both", nil, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := runChildEnv(t, "oneline", "json", str("trace"), c.env...)
			if len(lines) != 1 {
				t.Fatalf("lines = %q", lines)
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]*string{"server_ip": c.wantIP, "node": c.node} {
				got, present := m[key]
				if want == nil && present {
					t.Errorf("%s present (%v), want omitted", key, got)
				}
				if want != nil && got != *want {
					t.Errorf("%s = %v, want %q", key, got, *want)
				}
			}
		})
	}
}

func TestSourceAttributionPointsAtCaller(t *testing.T) {
	var wantLine int
	calls := map[string]func(){
		"Log": func() {
			_, _, markLine, _ := runtime.Caller(0)
			wantLine = markLine + 2 // the call sits two lines below the mark
			logus.Log(logus.LevelInfo, "x")
		},
		"Logf": func() {
			_, _, markLine, _ := runtime.Caller(0)
			wantLine = markLine + 2
			logus.Logf(logus.LevelInfo, "x %d", 1)
		},
		"LogContextf": func() {
			_, _, markLine, _ := runtime.Caller(0)
			wantLine = markLine + 2
			logus.LogContextf(context.Background(), logus.LevelInfo, "x %d", 1)
		},
		"LogContext": func() {
			_, _, markLine, _ := runtime.Caller(0)
			wantLine = markLine + 2 // the call sits two lines below the mark
			logus.LogContext(context.Background(), logus.LevelInfo, "x")
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

func TestLogIDFallsBackToRequestID(t *testing.T) {
	buf := captureLogs(t)
	id := uuid.New()
	logus.LogContext(logus.WithRequestID(context.Background(), id), logus.LevelInfo, "x")
	if got := buf.last(t)["log_id"]; got != id.String() {
		t.Fatalf("log_id = %v, want %v", got, id)
	}
}

func TestUserIDFromGetter(t *testing.T) {
	buf := captureLogs(t)
	f := newFakeGinContext()
	f.Set(logus.CtxKeyUserID, "user-9")
	logus.LogContext(f, logus.LevelInfo, "x")
	m := buf.last(t)
	if m["user_id"] != "user-9" {
		t.Fatalf("user_id = %v", m["user_id"])
	}
	stored, _ := f.Get(logus.CtxKeyRequestID)
	if m["log_id"] != fmt.Sprint(stored) || m["log_id"] == "" {
		t.Fatalf("log_id = %v, stored = %v", m["log_id"], stored)
	}
}

func TestUserIDFromAnyKeyContext(t *testing.T) {
	buf := captureLogs(t)
	f := newFakeAnyKeyContext()
	f.Set(logus.CtxKeyUserID, "user-9")
	logus.LogContext(f, logus.LevelInfo, "x")
	m := buf.last(t)
	if m["user_id"] != "user-9" {
		t.Fatalf("user_id = %v", m["user_id"])
	}
	stored, _ := f.Get(logus.CtxKeyRequestID)
	if m["log_id"] != fmt.Sprint(stored) || m["log_id"] == "" {
		t.Fatalf("log_id = %v, stored = %v", m["log_id"], stored)
	}
}

func TestNoUserIDFieldWhenAbsent(t *testing.T) {
	buf := captureLogs(t)
	logus.LogContext(context.Background(), logus.LevelInfo, "x")
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
	//nolint:staticcheck // nil context is deliberately tolerated
	logus.LogContext(nil, logus.LevelInfo, "x")
	m := buf.last(t)
	if m["msg"] != "x" {
		t.Fatalf("msg = %v", m["msg"])
	}
	if _, ok := m["log_id"]; ok {
		t.Fatal("log_id should be absent for nil context")
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
	f.Set(logus.CtxKeyRequestID, a)
	if got := logus.RequestID(f); got != a {
		t.Fatalf("got %v, want %v", got, a)
	}
}

func TestRequestIDParsesStringFromGetter(t *testing.T) {
	a := uuid.New()
	f := newFakeGinContext()
	f.Set(logus.CtxKeyRequestID, a.String())
	if got := logus.RequestID(f); got != a {
		t.Fatalf("got %v, want %v", got, a)
	}
}

func TestRequestIDReadsPlainStringKey(t *testing.T) {
	a := uuid.New()
	//nolint:staticcheck // plain string key is a supported lookup under test
	ctx := context.WithValue(context.Background(), logus.CtxKeyRequestID, a)
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
	if v, _ := f.Get(logus.CtxKeyRequestID); v != got {
		t.Fatalf("stored %v, want %v", v, got)
	}
	if again := logus.RequestID(f); again != got {
		t.Fatalf("not stable: %v vs %v", again, got)
	}
}

func TestRequestIDReplacesInvalidStoredString(t *testing.T) {
	f := newFakeGinContext()
	f.Set(logus.CtxKeyRequestID, "not-a-uuid")
	got := logus.RequestID(f)
	if got == uuid.Nil {
		t.Fatal("expected generated id")
	}
	if v, _ := f.Get(logus.CtxKeyRequestID); v != got {
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
		f.Set(logus.CtxKeyRequestID, uuid.Nil)
		got := logus.RequestID(f)
		if got == uuid.Nil {
			t.Fatal("expected generated id")
		}
		if v, _ := f.Get(logus.CtxKeyRequestID); v != got {
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
	if v, _ := f.Get(logus.CtxKeyRequestID); v != got {
		t.Fatalf("stored %v, want %v", v, got)
	}
	if again := logus.RequestID(f); again != got {
		t.Fatalf("not stable: %v vs %v", again, got)
	}
}

func TestRequestIDReadsFromAnyKeyContext(t *testing.T) {
	a := uuid.New()
	f := newFakeAnyKeyContext()
	f.Set(logus.CtxKeyRequestID, a)
	if got := logus.RequestID(f); got != a {
		t.Fatalf("uuid value: got %v, want %v", got, a)
	}

	b := uuid.New()
	g := newFakeAnyKeyContext()
	g.Set(logus.CtxKeyRequestID, b.String())
	if got := logus.RequestID(g); got != b {
		t.Fatalf("string value: got %v, want %v", got, b)
	}
}

// // helperEnv selects a child-process mode: LOG_LEVEL and LOG_FORMAT are read once
// // per process, so such tests re-run the binary and inspect its stdout.
const helperEnv = "LOGUS_TEST_HELPER"

type levelCase struct {
	level logus.Level
	name  string
}

var allLevels = []levelCase{
	{logus.LevelTrace, "TRACE"}, {logus.LevelDebug, "DEBUG"}, {logus.LevelInfo, "INFO"},
	{logus.LevelWarn, "WARN"}, {logus.LevelError, "ERROR"}, {logus.LevelFatal, "FATAL"},
}

// runHelper is the child side: it writes one probe line per level.
func runHelper(mode string) {
	probe := func(write func(logus.Level, string)) {
		for _, lc := range allLevels {
			write(lc.level, "probe-"+lc.name)
		}
	}
	plain := func(l logus.Level, msg string) { logus.Log(l, msg) }
	withCtx := func(l logus.Level, msg string) { logus.LogContext(context.Background(), l, msg) }
	switch mode {
	case "levels":
		probe(plain)
	case "levels-ctx":
		probe(withCtx)
	case "twice":
		probe(plain)
		probe(withCtx)
	case "setlevel-first":
		logus.SetLevel(logus.LevelDebug)
		probe(plain)
	case "oneline":
		logus.Log(logus.LevelInfo, "x")
	case "getlevel":
		fmt.Println(logus.GetLevel())
	case "readonce":
		logus.Log(logus.LevelDebug, "probe-first")
		_ = os.Setenv("LOG_LEVEL", "trace")
		logus.Log(logus.LevelDebug, "probe-second")
	}
}

// runChild runs the helper mode with LOG_FORMAT/LOG_LEVEL controlled by the
// caller. A nil level leaves LOG_LEVEL unset.
func runChild(t *testing.T, mode, format string, level *string) []string {
	t.Helper()
	return runChildEnv(t, mode, format, level)
}

// runChildEnv is runChild with extra environment entries; SERVER_IP, ServerIP
// and NODE are cleared first so the child sees only what the caller passes.
func runChildEnv(t *testing.T, mode, format string, level *string, extra ...string) []string {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "SERVER_IP=") || strings.HasPrefix(kv, "ServerIP=") || strings.HasPrefix(kv, "NODE=") ||
			strings.HasPrefix(kv, "LOG_LEVEL=") || strings.HasPrefix(kv, "LOG_FORMAT=") || strings.HasPrefix(kv, helperEnv+"=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, extra...)
	env = append(env, helperEnv+"="+mode, "LOG_FORMAT="+format)
	if level != nil {
		env = append(env, "LOG_LEVEL="+*level)
	}
	//nolint:gosec // re-runs this test binary, not external input
	cmd := exec.Command(os.Args[0])
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("child %s failed: %v", mode, err)
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func str(s string) *string { return &s }

// jsonRecords runs the child with JSON output and returns level/msg pairs.
func jsonRecords(t *testing.T, mode string, level *string) (levels, msgs []string) {
	t.Helper()
	for _, line := range runChild(t, mode, "json", level) {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("parse %q: %v", line, err)
		}
		levels = append(levels, fmt.Sprint(m["level"]))
		msgs = append(msgs, fmt.Sprint(m["msg"]))
	}
	return levels, msgs
}

// probed returns the names of the levels whose probe line was written,
// ignoring the LOG_LEVEL warning line.
func probed(msgs []string) []string {
	var got []string
	for _, m := range msgs {
		if name, ok := strings.CutPrefix(m, "probe-"); ok {
			got = append(got, name)
		}
	}
	return got
}

func warnings(msgs []string) int {
	n := 0
	for _, m := range msgs {
		if strings.Contains(m, "LOG_LEVEL") {
			n++
		}
	}
	return n
}

// namesFrom lists the level names at or above the first one named min.
func namesFrom(min string) []string {
	for i, lc := range allLevels {
		if lc.name == min {
			var out []string
			for _, l := range allLevels[i:] {
				out = append(out, l.name)
			}
			return out
		}
	}
	return nil
}

func TestPublicLevelValuesAlignWithSlogAndCore(t *testing.T) {
	want := []struct {
		got   logus.Level
		slog  slog.Level
		core  core.Level
		value int
	}{
		{logus.LevelTrace, -8, core.LevelTrace, -8}, {logus.LevelDebug, slog.LevelDebug, core.LevelDebug, -4},
		{logus.LevelInfo, slog.LevelInfo, core.LevelInfo, 0}, {logus.LevelWarn, slog.LevelWarn, core.LevelWarn, 4},
		{logus.LevelError, slog.LevelError, core.LevelError, 8}, {logus.LevelFatal, 12, core.LevelFatal, 12},
	}
	for _, w := range want {
		if int(w.got) != w.value || slog.Level(w.got) != w.slog || int(w.core) != w.value {
			t.Errorf("%v: public=%d slog=%d core=%d, want %d", w.got, int(w.got), int(w.slog), int(w.core), w.value)
		}
	}
	for _, p := range [][2]any{{logus.CtxKeyRequestID, core.CtxKeyRequestID}, {logus.CtxKeyUserID, core.CtxKeyUserID}} {
		if p[0] != p[1] {
			t.Errorf("public %v != core %v", p[0], p[1])
		}
	}
	for _, lc := range allLevels {
		if lc.level.String() != lc.name {
			t.Errorf("String() = %q, want %q", lc.level.String(), lc.name)
		}
	}
}

var offsetForm = regexp.MustCompile(`[A-Z][+-]\d`)

func TestEveryLevelAppearsByNameInAllFormats(t *testing.T) {
	for _, format := range []string{"json", "text", "string"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			lines := runChild(t, "levels", format, str("trace"))
			if len(lines) != len(allLevels) {
				t.Fatalf("lines = %d, want %d: %q", len(lines), len(allLevels), lines)
			}
			for i, lc := range allLevels {
				line := lines[i]
				if offsetForm.MatchString(line) {
					t.Errorf("%s: offset-form level in %q", lc.name, line)
				}
				var ok bool
				switch format {
				case "json":
					ok = strings.Contains(line, `"level":"`+lc.name+`"`)
				case "text":
					ok = strings.Contains(line, "level="+lc.name+" ")
				default:
					ok = strings.Contains(line, "]["+lc.name+"]")
				}
				if !ok {
					t.Errorf("%s not named in %s line %q", lc.name, format, line)
				}
			}
		})
	}
}

func TestThresholdWritesLevelsAtOrAbove(t *testing.T) {
	cases := []struct {
		env  *string
		from string
	}{
		{str("trace"), "TRACE"}, {str("debug"), "DEBUG"}, {str("info"), "INFO"},
		{str("warn"), "WARN"}, {str("error"), "ERROR"}, {str("fatal"), "FATAL"},
		{nil, "INFO"}, {str(""), "INFO"}, {str("   "), "INFO"},
	}
	for _, mode := range []string{"levels", "levels-ctx"} {
		for _, c := range cases {
			name := mode + "/unset"
			if c.env != nil {
				name = mode + "/" + *c.env
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, msgs := jsonRecords(t, mode, c.env)
				if got, want := probed(msgs), namesFrom(c.from); !slices.Equal(got, want) {
					t.Fatalf("written %v, want %v", got, want)
				}
				if warnings(msgs) != 0 {
					t.Fatalf("unexpected warning: %q", msgs)
				}
			})
		}
	}
}

func TestLogLevelParsing(t *testing.T) {
	cases := map[string]string{
		"TRACE": "TRACE", "Debug": "DEBUG", "  info  ": "INFO", " WARN": "WARN", "warning": "WARN", "WARNING": "WARN",
		"Error": "ERROR", "fatal": "FATAL",
		// legacy numbers
		"0": "ERROR", "1": "ERROR", "2": "INFO", "3": "INFO", "4": "INFO", "5": "INFO", "6": "DEBUG", " 6 ": "DEBUG",
	}
	for env, from := range cases {
		t.Run(fmt.Sprintf("%q", env), func(t *testing.T) {
			t.Parallel()
			_, msgs := jsonRecords(t, "levels", str(env))
			if got, want := probed(msgs), namesFrom(from); !slices.Equal(got, want) {
				t.Fatalf("written %v, want %v", got, want)
			}
			if warnings(msgs) != 0 {
				t.Fatalf("unexpected warning: %q", msgs)
			}
		})
	}
}

func TestUnknownLogLevelFallsBackToInfoWithOneWarning(t *testing.T) {
	for _, env := range []string{"verbose", "7", "-1", "3.5"} {
		t.Run(fmt.Sprintf("%q", env), func(t *testing.T) {
			t.Parallel()
			levels, msgs := jsonRecords(t, "twice", str(env))
			if got, want := probed(msgs), append(namesFrom("INFO"), namesFrom("INFO")...); !slices.Equal(got, want) {
				t.Fatalf("written %v, want %v", got, want)
			}
			if n := warnings(msgs); n != 1 {
				t.Fatalf("warning lines = %d, want 1 (%q)", n, msgs)
			}
			for i, m := range msgs {
				if strings.Contains(m, "LOG_LEVEL") && levels[i] != "WARN" {
					t.Errorf("warning level = %s, want WARN", levels[i])
				}
			}
		})
	}
}

func TestLogLevelIsReadOnce(t *testing.T) {
	_, msgs := jsonRecords(t, "readonce", str("error"))
	if len(msgs) != 0 {
		t.Fatalf("expected nothing written, got %q", msgs)
	}
}

func TestGetLevelReflectsEnvAndDefault(t *testing.T) {
	for env, want := range map[string]string{"warn": "WARN", "trace": "TRACE", "6": "DEBUG"} {
		if got := runChild(t, "getlevel", "json", str(env)); !slices.Equal(got, []string{want}) {
			t.Errorf("LOG_LEVEL=%s: GetLevel = %v, want %s", env, got, want)
		}
	}
	if got := runChild(t, "getlevel", "json", nil); !slices.Equal(got, []string{"INFO"}) {
		t.Errorf("unset: GetLevel = %v, want INFO", got)
	}
}

func TestSetLevelBeforeFirstLogBeatsEnv(t *testing.T) {
	_, msgs := jsonRecords(t, "setlevel-first", str("fatal"))
	if got, want := probed(msgs), namesFrom("DEBUG"); !slices.Equal(got, want) {
		t.Fatalf("written %v, want %v", got, want)
	}
}

func TestSetLevelChangesThresholdImmediately(t *testing.T) {
	t.Cleanup(func() { logus.SetLevel(logus.LevelTrace) })
	buf := captureLogs(t)
	for _, from := range allLevels {
		logus.SetLevel(from.level)
		if got := logus.GetLevel(); got != from.level {
			t.Fatalf("GetLevel = %v, want %v", got, from.level)
		}
		buf.reset()
		for _, lc := range allLevels {
			logus.Log(lc.level, "probe-"+lc.name)
		}
		lines := buf.lines()
		msgs := make([]string, 0, len(lines))
		for _, line := range lines {
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("parse %q: %v", line, err)
			}
			msgs = append(msgs, fmt.Sprint(m["msg"]))
		}
		if got, want := probed(msgs), namesFrom(from.name); !slices.Equal(got, want) {
			t.Fatalf("threshold %s: written %v, want %v", from.name, got, want)
		}
	}
}

func TestSetLevelIsSafeForConcurrentUse(t *testing.T) {
	t.Cleanup(func() { logus.SetLevel(logus.LevelTrace) })
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				logus.SetLevel(allLevels[i%len(allLevels)].level)
				_ = logus.GetLevel()
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				logus.Log(allLevels[i%len(allLevels)].level, "race")
				logus.LogContext(context.Background(), logus.LevelFatal, "race")
			}
		}()
	}
	wg.Wait()
}

func TestFatalOnlyLabelsAndReturns(t *testing.T) {
	buf := captureLogs(t)
	returned := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("WriteLog(Fatal) panicked: %v", r)
			}
		}()
		logus.Log(logus.LevelFatal, "unrecoverable")
		logus.LogContext(context.Background(), logus.LevelFatal, "unrecoverable too")
		returned = true
	}()
	if !returned {
		t.Fatal("did not return normally")
	}
	lines := buf.lines()
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if m := buf.last(t); m["level"] != "FATAL" {
		t.Fatalf("level = %v, want FATAL", m["level"])
	}
}

func TestUnknownLevelValueIsIgnored(t *testing.T) {
	buf := captureLogs(t)
	logus.Log(logus.Level(999), "x")
	logus.Log(logus.Level(-1), "x")
	logus.Log(logus.Level(5), "x")
	if len(buf.lines()) != 0 {
		t.Fatalf("expected no output, got %q", strings.Join(buf.lines(), "\n"))
	}
}

func TestInvalidLogFormatWarnsOnceAndFallsBackToJSON(t *testing.T) {
	lines := runChild(t, "levels", "bogus", str("trace"))
	warns := 0
	probes := 0
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("fallback output is not JSON: %q", line)
		}
		msg := fmt.Sprint(m["msg"])
		if strings.Contains(msg, "LOG_FORMAT") {
			warns++
			if m["level"] != "WARN" {
				t.Errorf("warning level = %v, want WARN", m["level"])
			}
		}
		if strings.HasPrefix(msg, "probe-") {
			probes++
		}
	}
	if warns != 1 || probes != len(allLevels) {
		t.Fatalf("warnings=%d probes=%d, lines=%q", warns, probes, lines)
	}
}

func TestSetOutputRedirectsLines(t *testing.T) {
	var buf bytes.Buffer
	logus.SetOutput(&buf)
	t.Cleanup(func() { logus.SetOutput(logFile) })
	logus.Log(logus.LevelInfo, "to buffer")
	logus.LogContext(context.Background(), logus.LevelInfo, "again")
	if n := strings.Count(buf.String(), "\n"); n != 2 || !strings.Contains(buf.String(), "to buffer") {
		t.Fatalf("buffer = %q", buf.String())
	}
}

func TestSetFormatSwitchesFormat(t *testing.T) {
	var buf bytes.Buffer
	logus.SetOutput(&buf)
	t.Cleanup(func() { logus.SetOutput(logFile); logus.SetFormat("json") })
	logus.SetFormat("string")
	logus.Log(logus.LevelInfo, "compact")
	if line := buf.String(); strings.HasPrefix(line, "{") || !strings.Contains(line, "[INFO]") {
		t.Fatalf("not string format: %q", line)
	}
}

func TestSetLevelRejectsValuesOutsideTheSix(t *testing.T) {
	t.Cleanup(func() { logus.SetLevel(logus.LevelTrace) })
	logus.SetLevel(logus.LevelWarn)
	for _, bad := range []logus.Level{-1, 3, 5, 999, -9} {
		logus.SetLevel(bad)
		if got := logus.GetLevel(); got != logus.LevelWarn {
			t.Fatalf("SetLevel(%d) changed level to %v", int(bad), got)
		}
	}
}

func TestControlCharactersCannotForgeLinesInAnyFormat(t *testing.T) {
	t.Cleanup(func() { logus.SetOutput(logFile); logus.SetFormat("json") })
	ctx := logus.WithUserID(context.Background(), "u\nFORGED")
	for _, format := range []string{"json", "text", "string"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			logus.SetOutput(&buf)
			logus.SetFormat(format)
			logus.Log(logus.LevelInfo, "first\r\nFORGED line\x1b[31m")
			logus.LogContextf(ctx, logus.LevelInfo, "second\n%s", "FORGED too")
			lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
			if len(lines) != 2 {
				t.Fatalf("lines = %d, want 2: %q", len(lines), buf.String())
			}
			if strings.ContainsAny(buf.String(), "\r\x1b") {
				t.Fatalf("raw control character in %q", buf.String())
			}
		})
	}
}
