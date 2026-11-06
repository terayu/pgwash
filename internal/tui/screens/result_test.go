package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/washover"
)

func TestTableErrors(t *testing.T) {
	tables := []washover.TableResult{
		{TableName: "a"},
		{TableName: "b", Error: "boom"},
		{TableName: "c"},
		{TableName: "d", Error: "kaboom"},
	}
	got := tableErrors(tables)
	if len(got) != 2 || got[0].TableName != "b" || got[1].TableName != "d" {
		t.Errorf("tableErrors = %v, want b and d", got)
	}
}

func TestElapsedDuration(t *testing.T) {
	if got := elapsedDuration(nil); got != 0 {
		t.Errorf("nil state: got %v, want 0", got)
	}

	start := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	st := &washover.RunState{StartedAt: start}
	if got := elapsedDuration(st); got != 0 {
		t.Errorf("no CompletedAt: got %v, want 0", got)
	}

	end := start.Add(90 * time.Second)
	st.CompletedAt = &end
	if got := elapsedDuration(st); got != 90*time.Second {
		t.Errorf("got %v, want 90s", got)
	}
}

func TestResultModel_View_SkipModeShowsBackupCompletion(t *testing.T) {
	st := &washover.RunState{
		RunID:      "20260808_000000",
		TempDBName: "joywatcher_new_20260808_000000",
	}
	st.Config.Database = "joywatcher"
	st.Config.PhaseBMode = config.PhaseBSkip
	m := NewResult(st, nil, ".pgwash")
	view := m.View()
	if !strings.Contains(view, "フェーズA（バックアップ）が完了しました") {
		t.Error("skipモードでは「フェーズA（バックアップ）が完了しました」を表示すべき")
	}
	if !strings.Contains(view, st.TempDBName) {
		t.Error("skipモードではバックアップDB名を表示すべき")
	}
}

func TestResultModel_View_AutoModeShowsWashoverCompletion(t *testing.T) {
	st := &washover.RunState{RunID: "20260808_000000"}
	st.Config.Database = "joywatcher"
	st.Config.PhaseBMode = config.PhaseBAuto
	m := NewResult(st, nil, ".pgwash")
	if !strings.Contains(m.View(), "洗い替えが完了しました") {
		t.Error("autoモードでは「洗い替えが完了しました」を表示すべき")
	}
}
