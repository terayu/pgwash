package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/db"
	"github.com/terayu/pgwash/internal/washover"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDropRows(t *testing.T) {
	rows := []TableRow{
		{Info: db.TableInfo{Name: "a"}, Action: washover.ActionCopyAll},
		{Info: db.TableInfo{Name: "b"}, Action: washover.ActionDrop},
		{Info: db.TableInfo{Name: "c"}, Action: washover.ActionTrim},
		{Info: db.TableInfo{Name: "d"}, Action: washover.ActionDrop},
	}
	got := dropRows(rows)
	if len(got) != 2 || got[0].Info.Name != "b" || got[1].Info.Name != "d" {
		t.Errorf("dropRows = %v, want rows b and d", got)
	}
}

func TestCanSubmitConfirm_NoDropTables(t *testing.T) {
	rows := []TableRow{{Action: washover.ActionCopyAll}, {Action: washover.ActionTrim}}
	if !canSubmitConfirm(rows, false) {
		t.Error("削除対象がない場合、未acknowledgeでも確定できるべき")
	}
}

func TestCanSubmitConfirm_WithDropTables(t *testing.T) {
	rows := []TableRow{{Action: washover.ActionDrop}}
	if canSubmitConfirm(rows, false) {
		t.Error("削除対象があり未acknowledgeなら確定できないべき")
	}
	if !canSubmitConfirm(rows, true) {
		t.Error("削除対象があってもacknowledge済みなら確定できるべき")
	}
}

// escはBackMsgを発行する。
func TestConfirmModel_Esc_ProducesBackMsg(t *testing.T) {
	m := NewConfirm(config.Config{}, nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Errorf("expected BackMsg, got %T", cmd())
	}
}

// skipモードではダウンタイム警告（退避DBへリネーム）を表示せず、代わりにバックアップの案内を表示する。
func TestConfirmModel_View_SkipModeOmitsDowntimeWarning(t *testing.T) {
	m := NewConfirm(config.Config{PhaseBMode: config.PhaseBSkip, Database: "joywatcher"}, nil)
	view := m.View()
	if strings.Contains(view, "退避DBへリネーム") {
		t.Error("skipモードでは退避DBへのリネーム警告を表示すべきではない")
	}
	if !strings.Contains(view, "コピー先の一時DB") {
		t.Error("skipモードではバックアップの案内を表示すべき")
	}
}

// autoモードでは引き続きダウンタイム警告を表示する。
func TestConfirmModel_View_AutoModeShowsDowntimeWarning(t *testing.T) {
	m := NewConfirm(config.Config{PhaseBMode: config.PhaseBAuto, Database: "joywatcher"}, nil)
	view := m.View()
	if !strings.Contains(view, "退避DBへリネーム") {
		t.Error("autoモードでは退避DBへのリネーム警告を表示すべき")
	}
}

// 足切り対象ごとに適用される日時と、共通/個別の別を表示する。
func TestConfirmModel_View_ShowsPerTableCutoff(t *testing.T) {
	common := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	own := time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)
	cfg := config.Config{Cutoff: common, TableCutoffs: map[string]time.Time{"orders": own}}
	rows := []TableRow{
		{Info: db.TableInfo{Name: "orders"}, Action: washover.ActionTrim},
		{Info: db.TableInfo{Name: "events"}, Action: washover.ActionTrim},
		{Info: db.TableInfo{Name: "users"}, Action: washover.ActionCopyAll},
	}
	view := NewConfirm(cfg, rows).View()
	for _, line := range strings.Split(view, "\n") {
		switch {
		case strings.Contains(line, "orders"):
			if !strings.Contains(line, "2026-05-01 00:00:00") || !strings.Contains(line, "(個別)") {
				t.Errorf("orders line = %q, want own cutoff marked (個別)", line)
			}
		case strings.Contains(line, "events"):
			if !strings.Contains(line, "2026-08-01 00:00:00") || !strings.Contains(line, "(共通)") {
				t.Errorf("events line = %q, want common cutoff marked (共通)", line)
			}
		case strings.Contains(line, "users"):
			t.Errorf("non-trim table should not be listed: %q", line)
		}
	}
}
