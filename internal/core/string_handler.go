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
)

type stringHandler struct {
	mu     *sync.Mutex // shared by every handler derived from the same root
	writer io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

func newStringHandler(writer io.Writer, level slog.Leveler) slog.Handler {
	if level == nil {
		level = slog.LevelInfo
	}
	return &stringHandler{
		mu:     &sync.Mutex{},
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

// escapeControl makes s safe to embed in a single log line: \r, \n, \t and
// every other control character (including DEL, NEL and the Unicode line and
// paragraph separators) are replaced by a visible escape sequence.
func escapeControl(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x100 && isControl(r):
			fmt.Fprintf(&b, `\x%02x`, r)
		case isControl(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isControl(r rune) bool {
	return unicode.IsControl(r) || r == 0x2028 || r == 0x2029
}
