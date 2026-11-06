package config

import (
	"encoding/json"
	"testing"
	"time"
)

func mustWindows(t *testing.T, s string) []TimeWindow {
	t.Helper()
	ws, err := ParseTimeWindows(s)
	if err != nil {
		t.Fatalf("ParseTimeWindows(%q): %v", s, err)
	}
	return ws
}

func TestParseTimeWindow(t *testing.T) {
	w, err := ParseTimeWindow(" 23:00 - 01:30 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Start != 23*60 || w.End != 90 {
		t.Errorf("got %+v, want {1380 90}", w)
	}
	if w.String() != "23:00-01:30" {
		t.Errorf("String() = %q", w.String())
	}
}

func TestParseTimeWindow_Invalid(t *testing.T) {
	for _, s := range []string{"", "23:00", "23:00-", "25:00-01:00", "23:00-24:00", "aa:bb-01:00", "12:00-12:00"} {
		if _, err := ParseTimeWindow(s); err == nil {
			t.Errorf("ParseTimeWindow(%q): expected error, got nil", s)
		}
	}
}

func TestParseTimeWindows(t *testing.T) {
	if ws := mustWindows(t, ""); ws != nil {
		t.Errorf("empty input: got %v, want nil", ws)
	}
	ws := mustWindows(t, "23:00-01:00, 12:00-13:00,")
	if len(ws) != 2 || FormatTimeWindows(ws) != "23:00-01:00,12:00-13:00" {
		t.Errorf("got %v", ws)
	}
}

func TestParseTimeWindows_WholeDayIsError(t *testing.T) {
	if _, err := ParseTimeWindows("00:00-12:00,12:00-00:00"); err == nil {
		t.Error("expected error for windows covering the whole day")
	}
}

func TestTimeWindow_JSONRoundTrip(t *testing.T) {
	in := mustWindows(t, "23:00-01:00")
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `["23:00-01:00"]` {
		t.Errorf("json = %s", b)
	}
	var out []TimeWindow
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0] != in[0] {
		t.Errorf("round trip = %v, want %v", out, in)
	}
}

func TestNextOutsideWindows(t *testing.T) {
	at := func(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, time.Local) }
	tests := []struct {
		name    string
		windows string
		now     time.Time
		want    time.Time
	}{
		{name: "除外時間なし", windows: "", now: at(29, 23, 30), want: at(29, 23, 30)},
		{name: "時間帯の外", windows: "23:00-01:00", now: at(29, 22, 59), want: at(29, 22, 59)},
		{name: "日付をまたぐ時間帯の前半", windows: "23:00-01:00", now: at(29, 23, 0), want: at(30, 1, 0)},
		{name: "日付をまたぐ時間帯の後半", windows: "23:00-01:00", now: at(30, 0, 59), want: at(30, 1, 0)},
		{name: "終了時刻ちょうどは外", windows: "23:00-01:00", now: at(30, 1, 0), want: at(30, 1, 0)},
		{name: "日中の時間帯", windows: "12:00-13:00", now: at(29, 12, 30), want: at(29, 13, 0)},
		{name: "連続する時間帯はまとめて抜ける", windows: "23:00-01:00,01:00-02:00", now: at(29, 23, 30), want: at(30, 2, 0)},
		{name: "重なる時間帯はまとめて抜ける", windows: "00:30-03:00,23:00-01:00", now: at(29, 23, 30), want: at(30, 3, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextOutsideWindows(tt.now, mustWindows(t, tt.windows))
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
