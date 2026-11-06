package screens

import (
	"testing"
	"time"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/db"
	"github.com/terayu/pgwash/internal/washover"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCycleAction(t *testing.T) {
	cases := []struct {
		name     string
		current  washover.TableAction
		hasPtime bool
		want     washover.TableAction
	}{
		{"copyall->trim (ptime有り)", washover.ActionCopyAll, true, washover.ActionTrim},
		{"copyall->drop (ptime無し。切をスキップ)", washover.ActionCopyAll, false, washover.ActionDrop},
		{"trim->drop", washover.ActionTrim, true, washover.ActionDrop},
		{"drop->copyall (ptime有り)", washover.ActionDrop, true, washover.ActionCopyAll},
		{"drop->copyall (ptime無し)", washover.ActionDrop, false, washover.ActionCopyAll},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CycleAction(c.current, c.hasPtime)
			if got != c.want {
				t.Errorf("CycleAction(%v, %v) = %v, want %v", c.current, c.hasPtime, got, c.want)
			}
		})
	}
}

func TestCycleAction_NeverSkipsToTrimWithoutPtime(t *testing.T) {
	// ptime列がないテーブルはどの経路を辿っても Trim にはならない（設計案9章）。
	a := washover.ActionCopyAll
	for i := 0; i < 10; i++ {
		a = CycleAction(a, false)
		if a == washover.ActionTrim {
			t.Fatalf("ptime列がないテーブルが Trim になった (iteration %d)", i)
		}
	}
}

func TestVisibleIndices(t *testing.T) {
	rows := []TableRow{
		{Info: db.TableInfo{Name: "alarm_log1"}},
		{Info: db.TableInfo{Name: "_5024td20"}},
		{Info: db.TableInfo{Name: "_5204tw12xx"}},
		{Info: db.TableInfo{Name: "jwtrenddef"}},
	}

	if got := visibleIndices(rows, ""); len(got) != 4 {
		t.Errorf("empty query: got %d rows, want 4", len(got))
	}

	got := visibleIndices(rows, "td")
	if len(got) != 1 || rows[got[0]].Info.Name != "_5024td20" {
		t.Errorf("filter %q: got %v, want index of _5024td20", "td", got)
	}

	// 大文字小文字を区別しない
	got = visibleIndices(rows, "ALARM")
	if len(got) != 1 || rows[got[0]].Info.Name != "alarm_log1" {
		t.Errorf("case-insensitive filter failed: got %v", got)
	}

	got = visibleIndices(rows, "存在しない")
	if len(got) != 0 {
		t.Errorf("no-match filter: got %d rows, want 0", len(got))
	}
}

func TestFormatRowCount(t *testing.T) {
	cases := map[int64]string{
		0:        "0",
		312:      "312",
		45201:    "45,201",
		12480113: "12,480,113",
		-1234:    "-1,234",
	}
	for in, want := range cases {
		if got := formatRowCount(in); got != want {
			t.Errorf("formatRowCount(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		500:              "500 B",
		48 * 1024:        "48.0 kB",
		12 * 1024 * 1024: "12.0 MB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatLastPtime(t *testing.T) {
	if got := formatLastPtime(nil); got != "-" {
		t.Errorf("formatLastPtime(nil) = %q, want -", got)
	}
	ti := time.Date(2026, 8, 7, 3, 4, 5, 0, time.UTC)
	got := formatLastPtime(&ti)
	if got == "-" || len(got) != len("2026-08-07") {
		t.Errorf("formatLastPtime(%v) = %q, unexpected format", ti, got)
	}
}

func TestSummarize(t *testing.T) {
	rows := []TableRow{
		{Action: washover.ActionCopyAll},
		{Action: washover.ActionTrim},
		{Action: washover.ActionTrim},
		{Action: washover.ActionDrop},
	}
	copyAll, trim, drop := summarize(rows)
	if copyAll != 1 || trim != 2 || drop != 1 {
		t.Errorf("summarize = (%d,%d,%d), want (1,2,1)", copyAll, trim, drop)
	}
}

func TestNewTables_DefaultsToCopyAllAndSortsByName(t *testing.T) {
	infos := []db.TableInfo{
		{Name: "zzz"},
		{Name: "aaa"},
	}
	m := NewTables("joywatcher", "ptime", infos)
	if len(m.rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(m.rows))
	}
	if m.rows[0].Info.Name != "aaa" || m.rows[1].Info.Name != "zzz" {
		t.Errorf("rows not sorted by name: %v", m.rows)
	}
	for _, r := range m.rows {
		if r.Action != washover.ActionCopyAll {
			t.Errorf("default action = %v, want ActionCopyAll (既定は全件コピーであるべき)", r.Action)
		}
	}
}

func TestTablesModel_ApplyConfig(t *testing.T) {
	infos := []db.TableInfo{
		{Name: "alarm_log1", HasPtime: true},
		{Name: "jwtrenddef", HasPtime: true},
		{Name: "no_ptime_tbl", HasPtime: false},
		{Name: "obsolete_tbl", HasPtime: true},
	}
	m := NewTables("joywatcher", "ptime", infos)
	cfg := config.Config{
		Tables:     []string{"alarm_log1", "no_ptime_tbl"}, // no_ptime_tblはptime列がないためTrimにはならない
		DropTables: []string{"obsolete_tbl"},
	}
	m.ApplyConfig(cfg)

	want := map[string]washover.TableAction{
		"alarm_log1":   washover.ActionTrim,
		"jwtrenddef":   washover.ActionCopyAll,
		"no_ptime_tbl": washover.ActionCopyAll,
		"obsolete_tbl": washover.ActionDrop,
	}
	for _, r := range m.rows {
		if got, ok := want[r.Info.Name]; !ok || r.Action != got {
			t.Errorf("row %s: Action = %v, want %v", r.Info.Name, r.Action, want[r.Info.Name])
		}
	}
}

func TestTablesModel_ApplyConfig_AllTables(t *testing.T) {
	infos := []db.TableInfo{
		{Name: "a", HasPtime: true},
		{Name: "b", HasPtime: false},
	}
	m := NewTables("joywatcher", "ptime", infos)
	m.ApplyConfig(config.Config{AllTables: true})

	for _, r := range m.rows {
		switch r.Info.Name {
		case "a":
			if r.Action != washover.ActionTrim {
				t.Errorf("a: Action = %v, want ActionTrim", r.Action)
			}
		case "b":
			if r.Action != washover.ActionCopyAll {
				t.Errorf("b (no ptime): Action = %v, want ActionCopyAll", r.Action)
			}
		}
	}
}

// フィルタモードでない通常時、escはBackMsgを発行する。
func TestTablesModel_Esc_ProducesBackMsg(t *testing.T) {
	m := NewTables("joywatcher", "ptime", []db.TableInfo{{Name: "a"}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Errorf("expected BackMsg, got %T", cmd())
	}
}

// フィルタモード中のescは絞り込みを抜けるだけで、BackMsgは発行しない。
func TestTablesModel_Esc_InFilterMode_DoesNotProduceBackMsg(t *testing.T) {
	m := NewTables("joywatcher", "ptime", []db.TableInfo{{Name: "a"}})
	m.filterMode = true
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.filterMode {
		t.Error("esc should exit filter mode")
	}
	if cmd != nil {
		if _, ok := cmd().(BackMsg); ok {
			t.Error("esc in filter mode should not produce BackMsg")
		}
	}
}

func TestActionLabel(t *testing.T) {
	if actionLabel(washover.ActionCopyAll) != "[全]" {
		t.Errorf("ActionCopyAll label mismatch")
	}
	if actionLabel(washover.ActionTrim) != "[切]" {
		t.Errorf("ActionTrim label mismatch")
	}
	if actionLabel(washover.ActionDrop) != "[削]" {
		t.Errorf("ActionDrop label mismatch")
	}
}
