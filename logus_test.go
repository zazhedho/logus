package logus_test

import (
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

// logFile receives the process-wide logger's output. The logger binds to
// os.Stdout and reads LOG_FORMAT only once, so TestMain redirects stdout to
// this file and forces JSON before the logger is first created.
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

	origStdout := os.Stdout
	os.Stdout = f
	_ = os.Setenv("LOG_FORMAT", "json")
	_ = os.Setenv("LOG_LEVEL", "trace")
	logus.WriteLog(logus.LogLevelInfo, "logger warm-up")
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

func TestMessageArgsAreJoinedWithSprint(t *testing.T) {
	buf := captureLogs(t)
	err := errors.New("boom")
	logus.WriteLog(logus.LogLevelInfo, "a", 1, err, nil)
	if got, want := buf.last(t)["msg"], fmt.Sprint("a", 1, err, nil); got != want {
		t.Fatalf("msg = %q, want %q", got, want)
	}
}

func TestServerIPAndNodeFields(t *testing.T) {
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
	ctx := logus.WithLogMetadata(logus.WithRequestID(context.Background(), uuid.New()), "log-1", "user-1")
	logus.WriteLogWithContext(ctx, logus.LogLevelInfo, "x")
	m := buf.last(t)
	if m["log_id"] != "log-1" || m["user_id"] != "user-1" {
		t.Fatalf("got log_id=%v user_id=%v", m["log_id"], m["user_id"])
	}
}

func TestLogIDFallsBackToRequestID(t *testing.T) {
	buf := captureLogs(t)
	id := uuid.New()
	logus.WriteLogWithContext(logus.WithRequestID(context.Background(), id), logus.LogLevelInfo, "x")
	if got := buf.last(t)["log_id"]; got != id.String() {
		t.Fatalf("log_id = %v, want %v", got, id)
	}
}

func TestUserIDFromGetter(t *testing.T) {
	buf := captureLogs(t)
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
	//nolint:staticcheck // plain string key is a supported lookup under test
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

// helperEnv selects a child-process mode. LOG_LEVEL and LOG_FORMAT are read
// once per process, so tests that depend on their initial value re-run the
// test binary as a child and inspect what it writes to stdout.
const helperEnv = "LOGUS_TEST_HELPER"

type levelCase struct {
	level logus.Level
	name  string
}

var allLevels = []levelCase{
	{logus.LogLevelTrace, "TRACE"}, {logus.LogLevelDebug, "DEBUG"}, {logus.LogLevelInfo, "INFO"},
	{logus.LogLevelWarn, "WARN"}, {logus.LogLevelError, "ERROR"}, {logus.LogLevelFatal, "FATAL"},
}

// runHelper is the child side: it writes one probe line per level.
func runHelper(mode string) {
	probe := func(write func(logus.Level, string)) {
		for _, lc := range allLevels {
			write(lc.level, "probe-"+lc.name)
		}
	}
	plain := func(l logus.Level, msg string) { logus.WriteLog(l, msg) }
	withCtx := func(l logus.Level, msg string) { logus.WriteLogWithContext(context.Background(), l, msg) }
	switch mode {
	case "levels":
		probe(plain)
	case "levels-ctx":
		probe(withCtx)
	case "twice":
		probe(plain)
		probe(withCtx)
	case "setlevel-first":
		logus.SetLevel(logus.LogLevelDebug)
		probe(plain)
	case "getlevel":
		fmt.Println(logus.GetLevel())
	case "readonce":
		logus.WriteLog(logus.LogLevelDebug, "probe-first")
		_ = os.Setenv("LOG_LEVEL", "trace")
		logus.WriteLog(logus.LogLevelDebug, "probe-second")
	}
}

// runChild runs the helper mode with LOG_FORMAT/LOG_LEVEL controlled by the
// caller. A nil level leaves LOG_LEVEL unset.
func runChild(t *testing.T, mode, format string, level *string) []string {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "LOG_LEVEL=") || strings.HasPrefix(kv, "LOG_FORMAT=") || strings.HasPrefix(kv, helperEnv+"=") {
			continue
		}
		env = append(env, kv)
	}
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
		{logus.LogLevelTrace, -8, core.LevelTrace, -8}, {logus.LogLevelDebug, slog.LevelDebug, core.LevelDebug, -4},
		{logus.LogLevelInfo, slog.LevelInfo, core.LevelInfo, 0}, {logus.LogLevelWarn, slog.LevelWarn, core.LevelWarn, 4},
		{logus.LogLevelError, slog.LevelError, core.LevelError, 8}, {logus.LogLevelFatal, 12, core.LevelFatal, 12},
	}
	for _, w := range want {
		if int(w.got) != w.value || slog.Level(w.got) != w.slog || int(w.core) != w.value {
			t.Errorf("%v: public=%d slog=%d core=%d, want %d", w.got, int(w.got), int(w.slog), int(w.core), w.value)
		}
	}
	for _, p := range [][2]any{{logus.CtxKeyID, core.CtxKeyID}, {logus.CtxKeyUserID, core.CtxKeyUserID}} {
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
		{nil, "INFO"},
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
	for _, env := range []string{"verbose", "7", "-1", "", "   ", "3.5"} {
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
	t.Cleanup(func() { logus.SetLevel(logus.LogLevelTrace) })
	buf := captureLogs(t)
	for _, from := range allLevels {
		logus.SetLevel(from.level)
		if got := logus.GetLevel(); got != from.level {
			t.Fatalf("GetLevel = %v, want %v", got, from.level)
		}
		buf.reset()
		for _, lc := range allLevels {
			logus.WriteLog(lc.level, "probe-"+lc.name)
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
	t.Cleanup(func() { logus.SetLevel(logus.LogLevelTrace) })
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
				logus.WriteLog(allLevels[i%len(allLevels)].level, "race")
				logus.WriteLogWithContext(context.Background(), logus.LogLevelFatal, "race")
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
		logus.WriteLog(logus.LogLevelFatal, "unrecoverable")
		logus.WriteLogWithContext(context.Background(), logus.LogLevelFatal, "unrecoverable too")
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
	logus.WriteLog(logus.Level(999), "x")
	logus.WriteLog(logus.Level(-1), "x")
	logus.WriteLog(logus.Level(5), "x")
	if len(buf.lines()) != 0 {
		t.Fatalf("expected no output, got %q", strings.Join(buf.lines(), "\n"))
	}
}
