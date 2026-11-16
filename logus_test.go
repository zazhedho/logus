package logus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestWriteLogDropsDebugWhenLogLevelUnset(t *testing.T) {
	buf := captureLogs(t, "json")
	unsetEnv(t, "LOG_LEVEL")
	WriteLog(LogLevelDebug, "d")
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
	WriteLog(LogLevelWarn, "w")
	if n := strings.Count(buf.String(), "\n"); n != 1 {
		t.Fatalf("expected 1 line, got %d", n)
	}
	if m := lastJSONLine(t, buf); m["msg"] != "w" {
		t.Fatalf("msg = %v", m["msg"])
	}
}

func TestWriteLogWithContextLogsDebugWhenLogLevelUnset(t *testing.T) {
	buf := captureLogs(t, "json")
	unsetEnv(t, "LOG_LEVEL")
	WriteLogWithContext(context.Background(), LogLevelDebug, "d")
	if m := lastJSONLine(t, buf); m["msg"] != "d" {
		t.Fatalf("msg = %v", m["msg"])
	}
}

func TestLogLevelEnvFilters(t *testing.T) {
	buf := captureLogs(t, "json")
	t.Setenv("LOG_LEVEL", "3")
	for _, lvl := range []int{LogLevelWarn, LogLevelData} {
		WriteLog(lvl, "x")
		if buf.Len() != 0 {
			t.Fatalf("level %d should be dropped, got %q", lvl, buf.String())
		}
	}
	for _, lvl := range []int{LogLevelInfo, LogLevelFail, LogLevelError, LogLevelPanic} {
		buf.Reset()
		WriteLog(lvl, "x")
		if buf.Len() == 0 {
			t.Fatalf("level %d should be logged", lvl)
		}
	}
}

func TestLogLevelWhitespaceIsTrimmed(t *testing.T) {
	buf := captureLogs(t, "json")
	t.Setenv("LOG_LEVEL", " 6 ")
	WriteLog(LogLevelDebug, "d")
	if buf.Len() == 0 {
		t.Fatal("debug should be logged")
	}
}

func TestLogLevelNotANumberLogsOnlyPanic(t *testing.T) {
	for _, v := range []string{"abc", ""} {
		t.Run(fmt.Sprintf("%q", v), func(t *testing.T) {
			buf := captureLogs(t, "json")
			t.Setenv("LOG_LEVEL", v)
			WriteLog(LogLevelError, "e")
			if buf.Len() != 0 {
				t.Fatalf("error should be dropped, got %q", buf.String())
			}
			WriteLog(LogLevelPanic, "p")
			if m := lastJSONLine(t, buf); m["msg"] != "p" {
				t.Fatalf("msg = %v", m["msg"])
			}
		})
	}
}

func TestUnknownLevelIsIgnored(t *testing.T) {
	buf := captureLogs(t, "json")
	t.Setenv("LOG_LEVEL", "6")
	WriteLog(999, "x")
	WriteLog(-1, "x")
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestMessageArgsAreJoinedWithSprint(t *testing.T) {
	buf := captureLogs(t, "json")
	t.Setenv("LOG_LEVEL", "6")
	err := errors.New("boom")
	WriteLog(LogLevelInfo, "a", 1, err, nil)
	if got, want := lastJSONLine(t, buf)["msg"], fmt.Sprint("a", 1, err, nil); got != want {
		t.Fatalf("msg = %q, want %q", got, want)
	}
}

func TestSlogLevelMapping(t *testing.T) {
	t.Setenv("LOG_LEVEL", "6")
	want := map[int]string{
		LogLevelPanic: "ERROR", LogLevelError: "ERROR", LogLevelWarn: "WARN",
		LogLevelDebug: "DEBUG", LogLevelFail: "INFO", LogLevelInfo: "INFO", LogLevelData: "INFO",
	}
	for lvl, name := range want {
		buf := captureLogs(t, "json")
		WriteLog(lvl, "x")
		if got := lastJSONLine(t, buf)["level"]; got != name {
			t.Errorf("level %d -> %v, want %s", lvl, got, name)
		}
	}
}

func TestServerIPAndNodeFields(t *testing.T) {
	t.Setenv("LOG_LEVEL", "6")
	t.Run("set", func(t *testing.T) {
		buf := captureLogs(t, "json")
		t.Setenv("ServerIP", "10.0.0.1")
		t.Setenv("NODE", "api-1")
		WriteLog(LogLevelInfo, "x")
		m := lastJSONLine(t, buf)
		if m["server_ip"] != "10.0.0.1" || m["node"] != "api-1" {
			t.Fatalf("got server_ip=%v node=%v", m["server_ip"], m["node"])
		}
	})
	t.Run("unset", func(t *testing.T) {
		buf := captureLogs(t, "json")
		unsetEnv(t, "ServerIP")
		unsetEnv(t, "NODE")
		WriteLog(LogLevelInfo, "x")
		m := lastJSONLine(t, buf)
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
	calls := map[string]func(){
		"WriteLog":            func() { WriteLog(LogLevelInfo, "x") },
		"WriteLogWithContext": func() { WriteLogWithContext(context.Background(), LogLevelInfo, "x") },
	}
	for name, call := range calls {
		buf := captureLogs(t, "json")
		call()
		m := lastJSONLine(t, buf)
		if f, _ := m["source_file"].(string); !strings.HasSuffix(f, "logus_test.go") {
			t.Errorf("%s: source_file = %v", name, m["source_file"])
		}
		if fn, _ := m["source_function"].(string); !strings.Contains(fn, "TestSourceAttributionPointsAtCaller") {
			t.Errorf("%s: source_function = %v", name, m["source_function"])
		}
		if l, _ := m["source_line"].(float64); l <= 0 {
			t.Errorf("%s: source_line = %v", name, m["source_line"])
		}
	}
}

func TestLogIDFromMetadata(t *testing.T) {
	buf := captureLogs(t, "json")
	t.Setenv("LOG_LEVEL", "6")
	ctx := WithLogMetadata(WithRequestID(context.Background(), uuid.New()), "log-1", "user-1")
	WriteLogWithContext(ctx, LogLevelInfo, "x")
	m := lastJSONLine(t, buf)
	if m["log_id"] != "log-1" || m["user_id"] != "user-1" {
		t.Fatalf("got log_id=%v user_id=%v", m["log_id"], m["user_id"])
	}
}

func TestLogIDFallsBackToRequestID(t *testing.T) {
	buf := captureLogs(t, "json")
	t.Setenv("LOG_LEVEL", "6")
	id := uuid.New()
	WriteLogWithContext(WithRequestID(context.Background(), id), LogLevelInfo, "x")
	if got := lastJSONLine(t, buf)["log_id"]; got != id.String() {
		t.Fatalf("log_id = %v, want %v", got, id)
	}
}

func TestUserIDFromGetter(t *testing.T) {
	buf := captureLogs(t, "json")
	t.Setenv("LOG_LEVEL", "6")
	f := newFakeGinContext()
	f.Set(CtxKeyUserID, "user-9")
	WriteLogWithContext(f, LogLevelInfo, "x")
	m := lastJSONLine(t, buf)
	if m["user_id"] != "user-9" {
		t.Fatalf("user_id = %v", m["user_id"])
	}
	stored, _ := f.Get(CtxKeyID)
	if m["log_id"] != fmt.Sprint(stored) || m["log_id"] == "" {
		t.Fatalf("log_id = %v, stored = %v", m["log_id"], stored)
	}
}

func TestUserIDFromAnyKeyContext(t *testing.T) {
	buf := captureLogs(t, "json")
	t.Setenv("LOG_LEVEL", "6")
	f := newFakeAnyKeyContext()
	f.Set(CtxKeyUserID, "user-9")
	WriteLogWithContext(f, LogLevelInfo, "x")
	m := lastJSONLine(t, buf)
	if m["user_id"] != "user-9" {
		t.Fatalf("user_id = %v", m["user_id"])
	}
	stored, _ := f.Get(CtxKeyID)
	if m["log_id"] != fmt.Sprint(stored) || m["log_id"] == "" {
		t.Fatalf("log_id = %v, stored = %v", m["log_id"], stored)
	}
}

func TestNoUserIDFieldWhenAbsent(t *testing.T) {
	buf := captureLogs(t, "json")
	t.Setenv("LOG_LEVEL", "6")
	WriteLogWithContext(context.Background(), LogLevelInfo, "x")
	m := lastJSONLine(t, buf)
	if _, ok := m["user_id"]; ok {
		t.Fatal("user_id should be absent")
	}
	if s, _ := m["log_id"].(string); s == "" {
		t.Fatalf("log_id = %v, want non-empty", m["log_id"])
	}
}

func TestNilContextDoesNotPanic(t *testing.T) {
	buf := captureLogs(t, "json")
	t.Setenv("LOG_LEVEL", "6")
	//nolint:staticcheck // nil context is deliberately tolerated
	WriteLogWithContext(nil, LogLevelInfo, "x")
	m := lastJSONLine(t, buf)
	if m["msg"] != "x" {
		t.Fatalf("msg = %v", m["msg"])
	}
	if _, ok := m["log_id"]; ok {
		t.Fatal("log_id should be absent for nil context")
	}
}

func TestWithLogMetadata(t *testing.T) {
	buf := captureLogs(t, "json")
	t.Setenv("LOG_LEVEL", "6")
	//nolint:staticcheck // nil context is deliberately tolerated
	if ctx := WithLogMetadata(nil, "l", "u"); ctx == nil {
		t.Fatal("nil ctx result")
	}
	ctx := WithLogMetadata(WithLogMetadata(context.Background(), "l", "u"), "", "")
	WriteLogWithContext(ctx, LogLevelInfo, "x")
	m := lastJSONLine(t, buf)
	if m["log_id"] != "l" || m["user_id"] != "u" {
		t.Fatalf("got log_id=%v user_id=%v", m["log_id"], m["user_id"])
	}
}

func TestNewLoggerSelectsFormat(t *testing.T) {
	emit := func(format string) string {
		var buf bytes.Buffer
		newLogger(&buf, format).Info("hello")
		return strings.TrimSpace(buf.String())
	}
	datePrefix := regexp.MustCompile(`^\d{4}/\d{2}/\d{2} `)
	for _, f := range []string{"string", " STRING "} {
		if line := emit(f); line == "" || line[0] == '{' || !datePrefix.MatchString(line) {
			t.Errorf("format %q: unexpected line %q", f, line)
		}
	}
	if line := emit("text"); !strings.Contains(line, "msg=") || !strings.Contains(line, "level=INFO") {
		t.Errorf("text format: unexpected line %q", line)
	}
	for _, f := range []string{"json", "", "apa-saja"} {
		var m map[string]any
		if err := json.Unmarshal([]byte(emit(f)), &m); err != nil {
			t.Errorf("format %q: not valid JSON: %v", f, err)
		}
	}
}

func TestConcurrentWritesProduceWholeLines(t *testing.T) {
	for _, format := range []string{"json", "string"} {
		t.Run(format, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", "6")
			buf := captureLogs(t, format)
			var wg sync.WaitGroup
			for g := 0; g < 50; g++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < 20; i++ {
						WriteLogWithContext(context.Background(), LogLevelInfo, "complete-message")
					}
				}()
			}
			wg.Wait()
			lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
			if len(lines) != 1000 {
				t.Fatalf("lines = %d, want 1000", len(lines))
			}
			for _, line := range lines {
				if format == "json" {
					var m map[string]any
					if err := json.Unmarshal([]byte(line), &m); err != nil {
						t.Fatalf("torn line %q: %v", line, err)
					}
				} else if !strings.HasSuffix(line, " complete-message") {
					t.Fatalf("torn line %q", line)
				}
			}
		})
	}
}
