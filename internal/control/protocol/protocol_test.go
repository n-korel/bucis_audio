package protocol

import (
	"math"
	"strings"
	"testing"
)

func TestParseSoundStartVariedSpacingAndSemicolons(t *testing.T) {
	cases := []struct {
		line string
		want SoundStart
	}{
		{"sound_start 1;100", SoundStart{Type: 1, T0: 100}},
		{"sound_start\t2;200", SoundStart{Type: 2, T0: 200}},
		{"sound_start 1 ; 300 ", SoundStart{Type: 1, T0: 300}},
		{"sound_start 2;;400", SoundStart{Type: 2, T0: 400}},
		{"  sound_start 1;500  ", SoundStart{Type: 1, T0: 500}},
		{"sound_start 1;600;", SoundStart{Type: 1, T0: 600}},
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(tc.line, " ", "_"), func(t *testing.T) {
			start, stop, ok := Parse([]byte(tc.line))
			if !ok || stop != nil {
				t.Fatalf("Parse() ok=%v stop=%v", ok, stop)
			}
			if *start != tc.want {
				t.Fatalf("got %+v want %+v", *start, tc.want)
			}
		})
	}
}

func TestParseSoundStartWithSessionID(t *testing.T) {
	line := "sound_start 1;1000;deadbeef"
	start, stop, ok := Parse([]byte(line))
	if !ok || stop != nil {
		t.Fatalf("Parse() ok=%v stop=%v", ok, stop)
	}
	if start.Type != 1 || start.T0 != 1000 || start.SessionID != "deadbeef" {
		t.Fatalf("got %+v", *start)
	}
}

func TestParseEmptyAndMalformed(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"sound_start",
		"sound_start 1",
		"sound_start 1;",
		"sound_start ;100",
		"sound_start 0;100",
		"sound_start 3;100",
		"sound_start x;100",
		"sound_start 1;100;abc",
		"sound_start 1;100;nothex!!",
		"sound_start 1;100;deadbeef;extra",
		"sound_start 1;100;deadbeefa",
		"unknown_cmd",
	}
	for _, line := range bad {
		t.Run(strings.ReplaceAll(line, " ", "_"), func(t *testing.T) {
			start, stop, ok := Parse([]byte(line))
			if ok {
				t.Fatalf("expected failure, got start=%v stop=%v", start, stop)
			}
		})
	}
}

func TestParseInt64OverflowRejected(t *testing.T) {
	maxInt64 := "9223372036854775807"
	line := "sound_start 1;" + maxInt64
	start, stop, ok := Parse([]byte(line))
	if !ok || start.T0 != math.MaxInt64 || stop != nil {
		t.Fatalf("max int64: ok=%v start=%v stop=%v", ok, start, stop)
	}

	overflow := "sound_start 1;9223372036854775808"
	start, stop, ok = Parse([]byte(overflow))
	if ok {
		t.Fatalf("expected overflow to fail, got start=%v stop=%v", start, stop)
	}

	typeOverflow := "sound_start 9223372036854775808;100"
	start, stop, ok = Parse([]byte(typeOverflow))
	if ok {
		t.Fatalf("expected type overflow to fail, got start=%v stop=%v", start, stop)
	}
}

func TestParseSoundStop(t *testing.T) {
	start, stop, ok := Parse([]byte("sound_stop extra args"))
	if !ok || start != nil {
		t.Fatalf("Parse: start=%v ok=%v", start, ok)
	}
	if stop.Args != "extra args" {
		t.Fatalf("Args %q", stop.Args)
	}
}
