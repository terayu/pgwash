package config

import (
	"fmt"
	"strings"
	"time"
)

// TimeWindow は1日のうちの時間帯 [Start, End)。ローカル時刻の 0:00 からの分で表す。
// Start > End の場合は日付をまたぐ時間帯（例 23:00-01:00）を表す。
type TimeWindow struct {
	Start int
	End   int
}

// ParseTimeWindow は "HH:MM-HH:MM" 形式の時間帯を解析する。
func ParseTimeWindow(s string) (TimeWindow, error) {
	startStr, endStr, ok := strings.Cut(strings.TrimSpace(s), "-")
	if !ok {
		return TimeWindow{}, fmt.Errorf("時間帯の形式が不正です（HH:MM-HH:MM で指定してください）: %q", s)
	}
	start, err := parseClock(startStr)
	if err != nil {
		return TimeWindow{}, fmt.Errorf("時間帯の開始時刻が不正です: %q", s)
	}
	end, err := parseClock(endStr)
	if err != nil {
		return TimeWindow{}, fmt.Errorf("時間帯の終了時刻が不正です: %q", s)
	}
	if start == end {
		return TimeWindow{}, fmt.Errorf("時間帯の開始と終了が同じです: %q", s)
	}
	return TimeWindow{Start: start, End: end}, nil
}

// ParseTimeWindows はカンマ区切りの時間帯を解析する。空文字なら nil を返す。
// 時間帯が1日全体を覆う場合は、フェーズBを開始できる時刻が存在しないためエラーとする。
func ParseTimeWindows(s string) ([]TimeWindow, error) {
	var out []TimeWindow
	for _, part := range strings.Split(s, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		w, err := ParseTimeWindow(part)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	if coversWholeDay(out) {
		return nil, fmt.Errorf("除外時間が1日全体を覆っています: %q", s)
	}
	return out, nil
}

func parseClock(s string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

func (w TimeWindow) String() string {
	return fmt.Sprintf("%02d:%02d-%02d:%02d", w.Start/60, w.Start%60, w.End/60, w.End%60)
}

// FormatTimeWindows は時間帯の一覧を ParseTimeWindows と同じカンマ区切り形式で返す。
func FormatTimeWindows(ws []TimeWindow) string {
	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = w.String()
	}
	return strings.Join(parts, ",")
}

// MarshalText は状態ファイルへ "HH:MM-HH:MM" 形式で書き出すために実装する。
func (w TimeWindow) MarshalText() ([]byte, error) {
	return []byte(w.String()), nil
}

func (w *TimeWindow) UnmarshalText(b []byte) error {
	parsed, err := ParseTimeWindow(string(b))
	if err != nil {
		return err
	}
	*w = parsed
	return nil
}

// containsMinute は 0:00 からの分 m が時間帯に含まれるかを返す。
func (w TimeWindow) containsMinute(m int) bool {
	if w.Start < w.End {
		return w.Start <= m && m < w.End
	}
	return m >= w.Start || m < w.End
}

// Contains は時刻 t（秒以下は切り捨て）が時間帯に含まれるかを返す。
func (w TimeWindow) Contains(t time.Time) bool {
	return w.containsMinute(t.Hour()*60 + t.Minute())
}

// endAfter は t より後で最初に来る時間帯の終了時刻を返す。
func (w TimeWindow) endAfter(t time.Time) time.Time {
	end := time.Date(t.Year(), t.Month(), t.Day(), w.End/60, w.End%60, 0, 0, t.Location())
	if !end.After(t) {
		end = end.AddDate(0, 0, 1)
	}
	return end
}

func coversWholeDay(ws []TimeWindow) bool {
	if len(ws) == 0 {
		return false
	}
	for m := 0; m < 24*60; m++ {
		covered := false
		for _, w := range ws {
			if w.containsMinute(m) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

// NextOutsideWindows は now 以降で、どの時間帯にも含まれない最初の時刻を返す。
// now がどの時間帯にも含まれなければ now をそのまま返す。
// 時間帯が重なる・連続する場合は、まとめて抜けた時刻を返す。
func NextOutsideWindows(now time.Time, ws []TimeWindow) time.Time {
	t := now
	// 各時間帯を高々1回ずつ抜ければ外に出られるため、len(ws)+1 回で打ち切る
	// （1日全体を覆う指定は ParseTimeWindows で弾いている）。
	for range len(ws) + 1 {
		moved := false
		for _, w := range ws {
			if w.Contains(t) {
				t = w.endAfter(t)
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	return t
}
