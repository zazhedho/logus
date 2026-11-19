package core

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// captureLogs swaps the package logger for newLogger(&buf, format) for the
// duration of the test and restores it via t.Cleanup.
func captureLogs(t *testing.T, format string) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	SetOutput(buf)
	SetFormat(format)
	t.Cleanup(func() { SetOutput(nil); SetFormat("") })
	return buf
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
			buf := captureLogs(t, format)
			var wg sync.WaitGroup
			for g := 0; g < 50; g++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < 20; i++ {
						Log(context.Background(), LevelInfo, "complete-message")
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

func TestControlCharactersAreEscapedInEveryFormat(t *testing.T) {
	for _, format := range []string{"json", "text", "string"} {
		t.Run(format, func(t *testing.T) {
			buf := captureLogs(t, format)
			Log(context.Background(), LevelInfo, "one\ntwo\r\nthree\x1b[0m")
			out := buf.String()
			if strings.Count(out, "\n") != 1 || strings.ContainsAny(strings.TrimSuffix(out, "\n"), "\r\x1b") {
				t.Fatalf("unsafe output %q", out)
			}
		})
	}
}

// resetState forgets the cached logger and level so the next call re-reads the
// environment, and restores the defaults afterwards.
func resetState(t *testing.T) {
	t.Helper()
	reset := func() {
		logMu.Lock()
		appLogger, logOut, logFormat = nil, nil, ""
		logMu.Unlock()
		levelOnce = sync.Once{}
		levelVar.Set(0)
	}
	reset()
	t.Cleanup(reset)
}

func TestSetOutputRedirectsLogger(t *testing.T) {
	resetState(t)
	var buf bytes.Buffer
	SetOutput(&buf)
	Log(context.Background(), LevelInfo, "hello")
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("buf = %q", buf.String())
	}
	SetOutput(nil) // back to stdout, buf must stop receiving
	n := buf.Len()
	Log(context.Background(), LevelInfo, "elsewhere")
	if buf.Len() != n {
		t.Fatal("output still written to the old writer")
	}
}

func TestInvalidLogFormatWarns(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*testing.T)
	}{
		{"env", func(t *testing.T) { t.Setenv("LOG_FORMAT", "yaml") }},
		{"SetFormat", func(t *testing.T) { SetFormat("yaml") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetState(t)
			t.Setenv("LOG_LEVEL", "info")
			var buf bytes.Buffer
			if tc.name == "env" {
				tc.set(t)
				SetOutput(&buf)
			} else {
				SetOutput(&buf)
				tc.set(t)
			}
			Log(context.Background(), LevelInfo, "after")
			out := buf.String()
			if strings.Count(out, "unrecognized") != 1 || !strings.Contains(out, "yaml") || !strings.Contains(out, `"level":"WARN"`) || !strings.Contains(out, `"msg":"after"`) {
				t.Fatalf("unexpected output %q", out)
			}
		})
	}
}

func TestValidLogFormatDoesNotWarn(t *testing.T) {
	for _, f := range []string{"", "json", " TEXT ", "string"} {
		resetState(t)
		t.Setenv("LOG_FORMAT", f)
		var buf bytes.Buffer
		SetOutput(&buf)
		Log(context.Background(), LevelInfo, "x")
		if strings.Contains(buf.String(), "LOG_FORMAT") {
			t.Errorf("format %q warned: %q", f, buf.String())
		}
	}
}

func TestEmptyLogLevelIsUnset(t *testing.T) {
	for _, v := range []string{"", "   "} {
		resetState(t)
		t.Setenv("LOG_LEVEL", v)
		var buf bytes.Buffer
		SetOutput(&buf)
		if got := GetLevel(); got != LevelInfo {
			t.Fatalf("LOG_LEVEL=%q: level %v, want info", v, got)
		}
		if buf.Len() != 0 {
			t.Fatalf("LOG_LEVEL=%q warned: %q", v, buf.String())
		}
	}
}

func TestSetLevelIgnoresInvalidValues(t *testing.T) {
	resetState(t)
	SetLevel(LevelError)
	for _, bad := range []Level{-9, -1, 1, 3, 100} {
		SetLevel(bad)
		if got := GetLevel(); got != LevelError {
			t.Fatalf("SetLevel(%d) changed level to %v", int(bad), got)
		}
	}
}
