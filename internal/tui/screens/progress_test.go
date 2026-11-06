package screens

import (
	"errors"
	"testing"
	"time"

	"github.com/terayu/pgwash/internal/washover"
)

func TestProgressBar_Bounds(t *testing.T) {
	if got := progressBar(0, 10, 12); got != "[░░░░░░░░░░]" {
		t.Errorf("0/10 = %q", got)
	}
	if got := progressBar(10, 10, 12); got != "[██████████]" {
		t.Errorf("10/10 = %q", got)
	}
	if got := progressBar(5, 10, 12); got != "[█████░░░░░]" {
		t.Errorf("5/10 = %q", got)
	}
}

func TestProgressBar_ZeroTotal(t *testing.T) {
	got := progressBar(0, 0, 10)
	if got != "[--------]" {
		t.Errorf("zero-total bar = %q", got)
	}
}

func TestProgressBar_OverDone(t *testing.T) {
	// done > total は起こらない想定だが、防御的に100%扱いにする。
	got := progressBar(15, 10, 12)
	if got != "[██████████]" {
		t.Errorf("over-done bar = %q, want fully filled", got)
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5秒"},
		{65 * time.Second, "1分5秒"},
		{3661 * time.Second, "1時間1分1秒"},
		{-1 * time.Second, "0秒"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.d); got != c.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestProgressModel_ApplyEvent(t *testing.T) {
	m := NewProgress("フェーズA")

	m.ApplyEvent(washover.Event{Kind: washover.EventPhaseStart, Total: 3, Message: "開始"})
	if m.total != 3 || m.done != 0 {
		t.Errorf("after phase_start: total=%d done=%d, want 3,0", m.total, m.done)
	}

	m.ApplyEvent(washover.Event{Kind: washover.EventTableStart, Table: "t1", Index: 1, Total: 3, Action: washover.ActionCopyAll})
	if m.currentTable != "t1" {
		t.Errorf("currentTable = %q, want t1", m.currentTable)
	}

	m.ApplyEvent(washover.Event{Kind: washover.EventTableDone, Table: "t1", Index: 1, Total: 3, Rows: 100})
	if m.done != 1 {
		t.Errorf("done = %d, want 1", m.done)
	}
	if m.currentTable != "" {
		t.Errorf("currentTable should be cleared after table_done, got %q", m.currentTable)
	}

	m.ApplyEvent(washover.Event{Kind: washover.EventError, Table: "t2", Err: errors.New("boom"), Message: "失敗"})
	if m.Err() == nil || m.Err().Error() != "boom" {
		t.Errorf("Err() = %v, want boom", m.Err())
	}

	if m.Finished() {
		t.Error("should not be finished yet")
	}
	m.ApplyEvent(washover.Event{Kind: washover.EventDone, Message: "完了"})
	if !m.Finished() {
		t.Error("should be finished after EventDone")
	}
}

func TestProgressModel_AppendLog_TrimsToMax(t *testing.T) {
	m := NewProgress("フェーズA")
	for i := 0; i < maxLogLines+5; i++ {
		m.appendLog("line")
	}
	if len(m.logs) != maxLogLines {
		t.Errorf("logs len = %d, want %d", len(m.logs), maxLogLines)
	}
}
