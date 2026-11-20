package core

import (
	"bytes"
	"context"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
	record := slog.NewRecord(time.Time{}, slog.LevelInfo, "a\nFORGED line\r\nb\x00c\x1b[31m\u2028d\x7f", 0)
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

// unescape reverses escapeControl; ok is false for a malformed sequence.
func unescape(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			r, size := utf8.DecodeRuneInString(s[i:])
			b.WriteRune(r)
			i += size
			continue
		}
		if i+1 >= len(s) {
			return "", false
		}
		switch c := s[i+1]; c {
		case '\\', 'n', 'r', 't':
			b.WriteByte(map[byte]byte{'\\': '\\', 'n': '\n', 'r': '\r', 't': '\t'}[c])
			i += 2
		case 'x', 'u', 'U':
			width := map[byte]int{'x': 2, 'u': 4, 'U': 8}[c]
			if i+2+width > len(s) {
				return "", false
			}
			n, err := strconv.ParseInt(s[i+2:i+2+width], 16, 32)
			if err != nil {
				return "", false
			}
			b.WriteRune(rune(n))
			i += 2 + width
		default:
			return "", false
		}
	}
	return b.String(), true
}

// asValidRunes is the input as escapeControl sees it: each invalid byte
// becomes U+FFFD.
func asValidRunes(s string) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteRune(r)
	}
	return b.String()
}

func TestEscapeControlIsLossless(t *testing.T) {
	// A literal backslash-n in the input must differ from a real newline.
	if escapeControl(`a\nb`) == escapeControl("a\nb") {
		t.Fatal("literal backslash-n and newline escape identically")
	}
	for in, want := range map[string]string{
		`a\nb`:          `a\\nb`,
		"a\nb":          `a\nb`,
		"tab\there":     `tab\there`,
		"\u202eevil":    `\u202eevil`,
		"zero\u200bw":   `zero\u200bw`,
		"\x00\x1f\x7f":  `\x00\x1f\x7f`,
		"bad\xffbyte":   "bad\ufffdbyte",
		"tag\U000E0001": `tag\U000e0001`,
		"plain ünïcode": "plain ünïcode",
	} {
		if got := escapeControl(in); got != want {
			t.Errorf("escapeControl(%q) = %q, want %q", in, got, want)
		}
	}
}

func FuzzEscapeControl(f *testing.F) {
	for _, seed := range []string{"", "plain", "a\nb", `a\nb`, "\r\n", "\x00\x1b[31m", "\u2028\u202e\u200b", "bad\xff\xfe", `\\`, "tag\U000E0001"} {
		f.Add(seed)
	}
	f.Fuzz(checkEscapeControl)
}

func checkEscapeControl(t *testing.T, in string) {
	out := escapeControl(in)
	if !utf8.ValidString(out) {
		t.Fatalf("invalid UTF-8 output for %q: %q", in, out)
	}
	for _, r := range out {
		if isEscapable(r) && r != '\\' {
			t.Fatalf("output %q for input %q contains escapable rune %U", out, in, r)
		}
	}
	if strings.ContainsAny(out, "\n\r") {
		t.Fatalf("raw newline in %q", out)
	}
	// Lossless: the escaping can be undone exactly.
	if back, ok := unescape(out); !ok || back != asValidRunes(in) {
		t.Fatalf("not lossless: %q -> %q -> %q", in, out, back)
	}
	// Input with nothing to escape passes through unchanged.
	if !strings.ContainsFunc(in, isEscapable) && utf8.ValidString(in) && out != in {
		t.Fatalf("clean input %q changed to %q", in, out)
	}
}
