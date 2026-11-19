package core

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

func TestStringHandlerPrintsLevelNames(t *testing.T) {
	for level, name := range map[Level]string{
		LevelTrace: "TRACE", LevelDebug: "DEBUG", LevelInfo: "INFO",
		LevelWarn: "WARN", LevelError: "ERROR", LevelFatal: "FATAL",
	} {
		var buf bytes.Buffer
		handler := newStringHandler(&buf, slog.Level(LevelTrace))
		record := slog.NewRecord(time.Time{}, slog.Level(level), "m", 0)
		if err := handler.Handle(context.Background(), record); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "]["+name+"]") {
			t.Errorf("%s: line %q", name, buf.String())
		}
	}
}

func TestStringHandlerEscapesControlCharacters(t *testing.T) {
	var buf bytes.Buffer
	handler := newStringHandler(&buf, slog.LevelDebug)
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "a\nFORGED line\r\nb\x00c\x1b[31m d\x7f", 0)
	record.AddAttrs(
		slog.String("server_ip", "1\n.1"),
		slog.String("node", "n\rode"),
		slog.String("log_id", "id\nforged"),
		slog.String("user_id", "u\x00ser"),
		slog.String("source_file", "f\nile.go"),
		slog.Int("source_line", 3),
	)
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("output must be exactly one line, got %q", out)
	}
	for _, r := range strings.TrimSuffix(out, "\n") {
		if r < 0x20 || r == 0x7f || r == 0x2028 || r == 0x2029 || r == 0x85 {
			t.Fatalf("raw control character %U in %q", r, out)
		}
	}
	for _, want := range []string{`a\nFORGED line\r\nb`, `[1\n.1]`, `[n\rode]`, `[id\nforged]`, `[u\x00ser]`, `[f\nile.go:3]`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestStringHandlerDerivedHandlersShareMutex(t *testing.T) {
	root := newStringHandler(&bytes.Buffer{}, slog.LevelDebug).(*stringHandler)
	withAttrs := root.WithAttrs([]slog.Attr{slog.String("a", "b")}).(*stringHandler)
	withGroup := root.WithGroup("g").(*stringHandler)
	deeper := withGroup.WithAttrs([]slog.Attr{slog.String("c", "d")}).(*stringHandler)
	for name, h := range map[string]*stringHandler{"WithAttrs": withAttrs, "WithGroup": withGroup, "nested": deeper} {
		if h.mu != root.mu {
			t.Errorf("%s: derived handler has its own mutex", name)
		}
	}
}
