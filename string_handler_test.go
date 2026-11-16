package logus

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestStringHandlerWritesCompactLine(t *testing.T) {
	var buf bytes.Buffer
	handler := newStringHandler(&buf, slog.LevelDebug)
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "hello", 0)
	record.AddAttrs(
		slog.String("server_ip", "127.0.0.1"),
		slog.String("node", "api"),
		slog.String("source_file", "logger_test.go"),
		slog.Int("source_line", 10),
	)

	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[127.0.0.1][api][INFO]") || !strings.Contains(out, "hello") {
		t.Fatalf("unexpected log line: %q", out)
	}
}

func TestStringHandlerWithGroupAndAttrs(t *testing.T) {
	var buf bytes.Buffer
	handler := newStringHandler(&buf, slog.LevelDebug).WithGroup("request").WithAttrs([]slog.Attr{slog.String("log_id", "log-1")})
	if !handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("expected info level enabled")
	}

	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "hello", 0)
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestStringHandlerLineLayout(t *testing.T) {
	var buf bytes.Buffer
	handler := newStringHandler(&buf, slog.LevelDebug)
	ts := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.Local)
	record := slog.NewRecord(ts, slog.LevelWarn, "hello", 0)
	record.AddAttrs(
		slog.String("server_ip", "10.0.0.1"),
		slog.String("node", "api"),
		slog.String("log_id", "L"),
		slog.String("user_id", "U"),
		slog.String("source_file", "a/b.go"),
		slog.Int("source_line", 12),
	)
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle: %v", err)
	}
	want := "2026/01/02 03:04:05 .123456 [10.0.0.1][api][WARN][L][U][a/b.go:12] hello\n"
	if buf.String() != want {
		t.Fatalf("got  %q\nwant %q", buf.String(), want)
	}
}

func TestStringHandlerOmitsOptionalSegments(t *testing.T) {
	var buf bytes.Buffer
	handler := newStringHandler(&buf, slog.LevelDebug)
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "hello", 0)
	record.AddAttrs(slog.String("server_ip", "ip"), slog.String("node", "node"))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle: %v", err)
	}
	line := strings.TrimSuffix(buf.String(), "\n")
	if !strings.HasSuffix(line, " [ip][node][INFO] hello") {
		t.Fatalf("unexpected line %q", line)
	}
}

func TestStringHandlerLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	ctx := context.Background()
	if newStringHandler(&buf, slog.LevelWarn).Enabled(ctx, slog.LevelInfo) {
		t.Fatal("info must be disabled at warn threshold")
	}
	if !newStringHandler(&buf, nil).Enabled(ctx, slog.LevelInfo) {
		t.Fatal("nil level must default to info")
	}
}
