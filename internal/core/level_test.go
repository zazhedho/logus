package core

import "testing"

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
