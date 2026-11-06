package cli

import (
	"testing"
	"time"
)

func TestFormatTimeNil(t *testing.T) {
	if got := formatTime(nil); got != "-" {
		t.Errorf("formatTime(nil) = %q, want -", got)
	}
}

func TestFormatTimeZero(t *testing.T) {
	var zero time.Time
	if got := formatTime(&zero); got != "-" {
		t.Errorf("formatTime(&zero) = %q, want -", got)
	}
}

func TestFormatTimeSet(t *testing.T) {
	tm := time.Date(2026, 8, 7, 12, 30, 0, 0, time.UTC)
	got := formatTime(&tm)
	want := tm.Local().Format("2006-01-02 15:04:05")
	if got != want {
		t.Errorf("formatTime(tm) = %q, want %q", got, want)
	}
}
