package logus

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

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

// lastJSONLine parses the last JSON line in buf; it fails the test when buf is empty.
func lastJSONLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		t.Fatal("no log output")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("parse %q: %v", lines[len(lines)-1], err)
	}
	return m
}
