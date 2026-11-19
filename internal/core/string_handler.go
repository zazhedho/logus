package core

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type stringHandler struct {
	mu     *sync.Mutex // shared by every handler derived from the same root
	writer io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

func newStringHandler(writer io.Writer, level slog.Leveler) slog.Handler {
	return newStringHandlerLocked(writer, level, &sync.Mutex{})
}

// newStringHandlerLocked serializes writes with mu, which every derived
// handler shares.
func newStringHandlerLocked(writer io.Writer, level slog.Leveler, mu *sync.Mutex) slog.Handler {
	if level == nil {
		level = slog.LevelInfo
	}
	return &stringHandler{
		mu:     mu,
		writer: writer,
		level:  level,
	}
}

func (h *stringHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *stringHandler) Handle(_ context.Context, record slog.Record) error {
	fields := map[string]string{}
	for _, attr := range h.attrs {
		h.addStringAttr(fields, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		h.addStringAttr(fields, attr)
		return true
	})

	level := Level(record.Level).String()
	prefix := fmt.Sprintf("[%s][%s][%s]", escapeControl(fields["server_ip"]), escapeControl(fields["node"]), level)
	if logID := fields["log_id"]; logID != "" {
		prefix += fmt.Sprintf("[%s]", escapeControl(logID))
	}
	if userID := fields["user_id"]; userID != "" {
		prefix += fmt.Sprintf("[%s]", escapeControl(userID))
	}
	if sourceFile := fields["source_file"]; sourceFile != "" {
		prefix += fmt.Sprintf("[%s:%s]", escapeControl(sourceFile), escapeControl(fields["source_line"]))
	}

	ts := record.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	line := fmt.Sprintf("%s %s %s\n", ts.Format("2006/01/02 15:04:05 .000000"), prefix, escapeControl(record.Message))

	h.mu.Lock()
	_, err := io.WriteString(h.writer, line)
	h.mu.Unlock()
	return err
}

func (h *stringHandler) addStringAttr(fields map[string]string, attr slog.Attr) {
	if attr.Key == "" {
		return
	}
	key := attr.Key
	value := attr.Value
	if value.Kind() == slog.KindAny {
		if logValue, ok := value.Any().(slog.LogValuer); ok {
			value = logValue.LogValue()
		}
	}
	if len(h.groups) > 0 {
		key = strings.Join(h.groups, ".") + "." + key
	}
	if value.Kind() == slog.KindString {
		fields[key] = value.String()
		return
	}
	fields[key] = fmt.Sprint(value.Any())
}

func (h *stringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &stringHandler{
		mu:     h.mu,
		writer: h.writer,
		level:  h.level,
		attrs:  append(append([]slog.Attr{}, h.attrs...), attrs...),
		groups: append([]string{}, h.groups...),
	}
}

func (h *stringHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &stringHandler{
		mu:     h.mu,
		writer: h.writer,
		level:  h.level,
		attrs:  append([]slog.Attr{}, h.attrs...),
		groups: append(append([]string{}, h.groups...), name),
	}
}

// escapeControl makes s safe to embed in one log line, and reversibly so.
// A backslash becomes `\\`; \n, \r and \t become their two-character forms;
// other control characters become \xNN; bidi overrides, zero-width and other
// format characters (Unicode category Cf) and the Unicode line and paragraph
// separators become \uNNNN (or \UNNNNNNNN beyond the BMP). Invalid UTF-8 bytes
// become U+FFFD.
func escapeControl(s string) string {
	if utf8.ValidString(s) && !strings.ContainsFunc(s, isEscapable) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s { // an invalid byte is yielded as utf8.RuneError
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case !isEscapable(r):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	return b.String()
}

func isEscapable(r rune) bool {
	return r == '\\' || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029
}
