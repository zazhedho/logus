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
	// Consume the once so getLogger never rebuilds over the captured logger.
	loggerOnce.Do(func() {})
	prev := appLogger
	appLogger = newLogger(buf, format)
	t.Cleanup(func() { appLogger = prev })
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
