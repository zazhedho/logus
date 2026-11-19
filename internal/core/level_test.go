package core

import (
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in    string
		want  Level
		valid bool
	}{
		{"trace", LevelTrace, true}, {" DeBuG ", LevelDebug, true}, {"info", LevelInfo, true},
		{"warn", LevelWarn, true}, {"warning", LevelWarn, true}, {"error", LevelError, true}, {"FATAL", LevelFatal, true},
		{"0", LevelError, true}, {"1", LevelError, true}, {"2", LevelInfo, true}, {"5", LevelInfo, true}, {"6", LevelDebug, true},
		{"7", LevelInfo, false}, {"-1", LevelInfo, false}, {"", LevelInfo, false}, {"nope", LevelInfo, false},
	}
	for _, c := range cases {
		got, ok := parseLevel(c.in)
		if got != c.want || ok != c.valid {
			t.Errorf("parseLevel(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.valid)
		}
	}
}

func TestLevelString(t *testing.T) {
	for l, want := range map[Level]string{
		LevelTrace: "TRACE", LevelDebug: "DEBUG", LevelInfo: "INFO",
		LevelWarn: "WARN", LevelError: "ERROR", LevelFatal: "FATAL",
	} {
		if l.String() != want {
			t.Errorf("%d.String() = %q, want %q", int(l), l.String(), want)
		}
	}
}

func FuzzParseLevel(f *testing.F) {
	for _, seed := range []string{"", "trace", "DEBUG", " warn ", "warning", "0", "6", "7", "-1", "+3", "1e1", "９", "\x00", "info\n", "99999999999999999999"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		level, ok := parseLevel(in)
		if ok && !level.valid() {
			t.Fatalf("parseLevel(%q) returned invalid level %d", in, int(level))
		}
		if !ok && level != LevelInfo {
			t.Fatalf("parseLevel(%q) failed but returned %v, want the info fallback", in, level)
		}
		// Surrounding spaces never change the outcome.
		if l2, ok2 := parseLevel("  " + in + "\t"); ok2 != ok || l2 != level {
			t.Fatalf("parseLevel(%q) = (%v,%v) but padded gives (%v,%v)", in, level, ok, l2, ok2)
		}
	})
}
