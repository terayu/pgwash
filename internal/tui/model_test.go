package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/db"
	"github.com/terayu/pgwash/internal/tui/screens"
	"github.com/terayu/pgwash/internal/washover"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewModel_StartsAtConnect(t *testing.T) {
	m := newModel(config.Config{Host: "localhost", Database: "joywatcher"})
	if m.stage != stageConnect {
		t.Errorf("initial stage = %v, want stageConnect", m.stage)
	}
	if m.Init() == nil {
		t.Error("Init() should return a non-nil Cmd (cursor blink) for the Connect screen")
	}
}

func TestDeriveTableConfig(t *testing.T) {
	rows := []screens.TableRow{
		{Info: db.TableInfo{Name: "a"}, Action: washover.ActionCopyAll},
		{Info: db.TableInfo{Name: "b"}, Action: washover.ActionTrim},
		{Info: db.TableInfo{Name: "c"}, Action: washover.ActionDrop},
		{Info: db.TableInfo{Name: "d"}, Action: washover.ActionTrim},
	}
	trim, drop := deriveTableConfig(rows)
	if len(trim) != 2 || trim[0] != "b" || trim[1] != "d" {
		t.Errorf("trim = %v, want [b d]", trim)
	}
	if len(drop) != 1 || drop[0] != "c" {
		t.Errorf("drop = %v, want [c]", drop)
	}
}

func TestDeriveTableConfig_NoTrimOrDrop(t *testing.T) {
	rows := []screens.TableRow{
		{Info: db.TableInfo{Name: "a"}, Action: washover.ActionCopyAll},
	}
	trim, drop := deriveTableConfig(rows)
	if trim != nil || drop != nil {
		t.Errorf("trim=%v drop=%v, want both nil", trim, drop)
	}
}

// handleConnectSubmitted はConnect画面の入力をcfgへ反映し、接続・テーブル一覧取得の
// IOをCmdとして起動する（IO自体は実行せず、遷移とCmdの発行だけを検証する）。
func TestHandleConnectSubmitted_TransitionsToConnecting(t *testing.T) {
	m := newModel(config.Config{})
	msg := screens.ConnectSubmittedMsg{
		ConnInfo: db.ConnInfo{Host: "localhost", Port: 5432, User: "postgres", Database: "joywatcher"},
		Schema:   "public",
	}
	next, cmd := m.handleConnectSubmitted(msg)
	if next.stage != stageConnecting {
		t.Errorf("stage = %v, want stageConnecting", next.stage)
	}
	if cmd == nil {
		t.Error("expected a non-nil Cmd to perform the connect+list-tables IO")
	}
	if next.cfg.Host != "localhost" || next.cfg.Database != "joywatcher" || next.cfg.Schema != "public" {
		t.Errorf("cfg not updated from ConnInfo: %+v", next.cfg)
	}
}

// Tables画面でTrim対象が1つもなければCutoff画面をスキップしてPhaseBModeへ進む。
func TestHandleTablesSubmitted_SkipsCutoffWhenNoTrim(t *testing.T) {
	m := newModel(config.Config{})
	msg := screens.TablesSubmittedMsg{Rows: []screens.TableRow{
		{Info: db.TableInfo{Name: "a"}, Action: washover.ActionCopyAll},
		{Info: db.TableInfo{Name: "b"}, Action: washover.ActionDrop},
	}}
	next, _ := m.handleTablesSubmitted(msg)
	if next.stage != stagePhaseBMode {
		t.Errorf("stage = %v, want stagePhaseBMode (Cutoffをスキップすべき)", next.stage)
	}
	if len(next.cfg.Tables) != 0 {
		t.Errorf("cfg.Tables = %v, want empty (Trim対象なし)", next.cfg.Tables)
	}
	if len(next.cfg.DropTables) != 1 || next.cfg.DropTables[0] != "b" {
		t.Errorf("cfg.DropTables = %v, want [b]", next.cfg.DropTables)
	}
}

// Trim対象が1つ以上あればCutoff画面へ進む。
func TestHandleTablesSubmitted_GoesToCutoffWhenTrimPresent(t *testing.T) {
	m := newModel(config.Config{})
	msg := screens.TablesSubmittedMsg{Rows: []screens.TableRow{
		{Info: db.TableInfo{Name: "a"}, Action: washover.ActionTrim},
	}}
	next, cmd := m.handleTablesSubmitted(msg)
	if next.stage != stageCutoff {
		t.Errorf("stage = %v, want stageCutoff", next.stage)
	}
	if cmd == nil {
		t.Error("expected a non-nil Cmd (cursor blink) when entering Cutoff screen")
	}
	if len(next.cfg.Tables) != 1 || next.cfg.Tables[0] != "a" {
		t.Errorf("cfg.Tables = %v, want [a]", next.cfg.Tables)
	}
}

func TestHandleCutoffSubmitted_GoesToPhaseBMode(t *testing.T) {
	m := newModel(config.Config{})
	m.stage = stageCutoff
	next, _ := m.handleCutoffSubmitted(screens.CutoffSubmittedMsg{})
	if next.stage != stagePhaseBMode {
		t.Errorf("stage = %v, want stagePhaseBMode", next.stage)
	}
}

func TestHandlePhaseBModeSubmitted_GoesToConfirm(t *testing.T) {
	m := newModel(config.Config{})
	m.stage = stagePhaseBMode
	next, _ := m.handlePhaseBModeSubmitted(screens.PhaseBModeSubmittedMsg{Mode: config.PhaseBManual})
	if next.stage != stageConfirm {
		t.Errorf("stage = %v, want stageConfirm", next.stage)
	}
	if next.cfg.PhaseBMode != config.PhaseBManual {
		t.Errorf("cfg.PhaseBMode = %v, want PhaseBManual", next.cfg.PhaseBMode)
	}
}

// フェーズA完了後、手動モードならPhaseBGateへ、自動モードならProgressBへ進む。
func TestHandlePhaseAResult_ManualGoesToGate(t *testing.T) {
	m := newModel(config.Config{PhaseBMode: config.PhaseBManual})
	m.stage = stageProgressA
	st := &washover.RunState{RunID: "20260808_000000", TempDBName: "joywatcher_new"}
	next, _ := m.handlePhaseAResult(phaseAResultMsg{st: st, err: nil})
	if next.stage != stagePhaseBGate {
		t.Errorf("stage = %v, want stagePhaseBGate", next.stage)
	}
}

// フェーズA完了後、skipモードならPhaseBGate/ProgressBを経由せず直接Resultへ進む。
func TestHandlePhaseAResult_SkipGoesToResult(t *testing.T) {
	m := newModel(config.Config{PhaseBMode: config.PhaseBSkip})
	m.stage = stageProgressA
	st := &washover.RunState{RunID: "20260808_000000", TempDBName: "joywatcher_new"}
	next, cmd := m.handlePhaseAResult(phaseAResultMsg{st: st, err: nil})
	if next.stage != stageResult {
		t.Errorf("stage = %v, want stageResult", next.stage)
	}
	if next.stage == stagePhaseBGate || next.stage == stageProgressB {
		t.Errorf("stage = %v, should not go to PhaseBGate/ProgressB in skip mode", next.stage)
	}
	if cmd != nil {
		t.Error("skip mode transition to Result should not spawn further IO")
	}
}

func TestHandlePhaseAResult_ErrorGoesToResult(t *testing.T) {
	m := newModel(config.Config{PhaseBMode: config.PhaseBAuto})
	m.stage = stageProgressA
	next, cmd := m.handlePhaseAResult(phaseAResultMsg{st: &washover.RunState{}, err: errBoom})
	if next.stage != stageResult {
		t.Errorf("stage = %v, want stageResult", next.stage)
	}
	if cmd != nil {
		t.Error("failure transition to Result should not spawn further IO")
	}
}

var errBoom = &testError{"boom"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

// フェーズB実行中のCtrl+Cは1回目では終了せず警告を立てるだけ。
func TestHandlePhaseBCtrlC_FirstPressWarnsWithoutQuitting(t *testing.T) {
	m := newModel(config.Config{})
	m.stage = stageProgressB
	next, cmd := m.handlePhaseBCtrlC()
	if !next.phaseBQuitArmed {
		t.Error("phaseBQuitArmed should be true after the first Ctrl+C")
	}
	if next.phaseBForceQuit {
		t.Error("phaseBForceQuit should still be false after only one Ctrl+C")
	}
	if cmd != nil {
		t.Error("first Ctrl+C during phase B must not quit")
	}
}

// 2回目のCtrl+Cで初めて終了する（tea.Quitに相当するCmdを返す）。
func TestHandlePhaseBCtrlC_SecondPressQuits(t *testing.T) {
	m := newModel(config.Config{})
	m.stage = stageProgressB
	m.phaseBQuitArmed = true
	next, cmd := m.handlePhaseBCtrlC()
	if !next.phaseBForceQuit {
		t.Error("phaseBForceQuit should be true after the second Ctrl+C")
	}
	if cmd == nil {
		t.Fatal("second Ctrl+C during phase B must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("second Ctrl+C should produce a tea.QuitMsg")
	}
}

// Update経由でも、ProgressB中はCtrl+Cを1回受けただけでは終了しない。
func TestUpdate_CtrlCDuringProgressB_RequiresSecondPress(t *testing.T) {
	m := newModel(config.Config{})
	m.stage = stageProgressB

	next1, cmd1 := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m1 := next1.(Model)
	if m1.stage != stageProgressB {
		t.Fatalf("stage changed after first Ctrl+C: %v", m1.stage)
	}
	if cmd1 != nil {
		t.Error("first Ctrl+C should not produce a quit Cmd")
	}

	next2, cmd2 := m1.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m2 := next2.(Model)
	if !m2.phaseBForceQuit {
		t.Error("second Ctrl+C should set phaseBForceQuit")
	}
	if cmd2 == nil {
		t.Fatal("second Ctrl+C should produce a quit Cmd")
	}
	if _, ok := cmd2().(tea.QuitMsg); !ok {
		t.Error("second Ctrl+C should produce a tea.QuitMsg")
	}
}

// Ctrl+Cは他のウィザード画面ではこれまでどおり1回で終了する（sourceを変更しないため）。
func TestUpdate_CtrlCOutsideProgressB_QuitsImmediately(t *testing.T) {
	for _, s := range []stage{stageConnect, stageTables, stageConfirm, stageProgressA, stagePhaseBGate} {
		m := newModel(config.Config{})
		m.stage = s
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if cmd == nil {
			t.Fatalf("stage %v: expected immediate quit Cmd", s)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("stage %v: expected tea.QuitMsg", s)
		}
	}
}

// esc（screens.BackMsg）はTables→Connectへ戻す。
func TestHandleBack_TablesToConnect(t *testing.T) {
	m := newModel(config.Config{})
	m.stage = stageTables
	next, cmd := m.handleBack()
	if next.stage != stageConnect {
		t.Errorf("stage = %v, want stageConnect", next.stage)
	}
	if cmd == nil {
		t.Error("expected a non-nil Cmd (cursor blink) when returning to Connect")
	}
}

// esc はCutoff→Tablesへ戻す。
func TestHandleBack_CutoffToTables(t *testing.T) {
	m := newModel(config.Config{})
	m.stage = stageCutoff
	next, _ := m.handleBack()
	if next.stage != stageTables {
		t.Errorf("stage = %v, want stageTables", next.stage)
	}
}

// Trim対象があるときはPhaseBMode→Cutoffへ戻す（往路でCutoffを経由しているため）。
func TestHandleBack_PhaseBModeToCutoff_WhenTrimPresent(t *testing.T) {
	m := newModel(config.Config{})
	m.cfg.Tables = []string{"a"}
	m.stage = stagePhaseBMode
	next, cmd := m.handleBack()
	if next.stage != stageCutoff {
		t.Errorf("stage = %v, want stageCutoff", next.stage)
	}
	if cmd == nil {
		t.Error("expected a non-nil Cmd (cursor blink) when returning to Cutoff")
	}
}

// Trim対象がないときは往路でCutoffをスキップしているため、PhaseBMode→Tablesへ戻す。
func TestHandleBack_PhaseBModeToTables_WhenNoTrim(t *testing.T) {
	m := newModel(config.Config{})
	m.stage = stagePhaseBMode
	next, _ := m.handleBack()
	if next.stage != stageTables {
		t.Errorf("stage = %v, want stageTables (Trim対象なしのためCutoffは経由しない)", next.stage)
	}
}

// esc はConfirm→PhaseBModeへ戻す。
func TestHandleBack_ConfirmToPhaseBMode(t *testing.T) {
	m := newModel(config.Config{})
	m.stage = stageConfirm
	next, _ := m.handleBack()
	if next.stage != stagePhaseBMode {
		t.Errorf("stage = %v, want stagePhaseBMode", next.stage)
	}
}

// 実行開始後の画面（ProgressA/Result等）はescを受けても戻らない。
func TestHandleBack_NonReturnableStagesStayPut(t *testing.T) {
	for _, s := range []stage{stageConnect, stageConnecting, stageProgressA, stagePhaseBGate, stageProgressB, stageResult} {
		m := newModel(config.Config{})
		m.stage = s
		next, cmd := m.handleBack()
		if next.stage != s {
			t.Errorf("stage %v: changed to %v, want unchanged", s, next.stage)
		}
		if cmd != nil {
			t.Errorf("stage %v: expected nil Cmd, got non-nil", s)
		}
	}
}

// Tables→Cutoff→戻る、という経路でもTables画面での選択（cfg.Tables/DropTables由来のrows）は
// 作り直されない。handleBackはm.rows/m.cfgをリセットしないことで確認する。
func TestHandleBack_PreservesTablesSelection(t *testing.T) {
	m := newModel(config.Config{})
	m.rows = []screens.TableRow{{Info: db.TableInfo{Name: "a"}, Action: washover.ActionTrim}}
	m.cfg.Tables = []string{"a"}
	m.stage = stageCutoff

	next, _ := m.handleBack()
	if next.stage != stageTables {
		t.Fatalf("stage = %v, want stageTables", next.stage)
	}
	if len(next.rows) != 1 || next.rows[0].Info.Name != "a" {
		t.Errorf("rows = %v, want preserved [a]", next.rows)
	}
	if len(next.cfg.Tables) != 1 || next.cfg.Tables[0] != "a" {
		t.Errorf("cfg.Tables = %v, want preserved [a]", next.cfg.Tables)
	}
}

func TestPhaseBForceQuitMessage_RevokeShowsGrantInstructions(t *testing.T) {
	msg := phaseBForceQuitMessage(config.Config{BlockMode: config.BlockRevoke, Database: "joywatcher"})
	if !strings.Contains(msg, "GRANT CONNECT ON DATABASE joywatcher TO PUBLIC;") {
		t.Errorf("message missing GRANT instructions: %q", msg)
	}
}

func TestPhaseBForceQuitMessage_BlockNoneOmitsGrantInstructions(t *testing.T) {
	msg := phaseBForceQuitMessage(config.Config{BlockMode: config.BlockNone, Database: "joywatcher"})
	if strings.Contains(msg, "GRANT CONNECT") {
		t.Errorf("BlockNone should not mention GRANT CONNECT recovery: %q", msg)
	}
}

// Tables画面で足切り対象から外したテーブルの個別日時は破棄し、残った対象の分だけ保持する。
func TestHandleTablesSubmitted_PrunesTableCutoffs(t *testing.T) {
	now := time.Now()
	m := newModel(config.Config{TableCutoffs: map[string]time.Time{"a": now, "b": now}})
	msg := screens.TablesSubmittedMsg{Rows: []screens.TableRow{
		{Info: db.TableInfo{Name: "a"}, Action: washover.ActionTrim},
		{Info: db.TableInfo{Name: "b"}, Action: washover.ActionCopyAll},
	}}
	next, _ := m.handleTablesSubmitted(msg)
	if len(next.cfg.TableCutoffs) != 1 || !next.cfg.TableCutoffs["a"].Equal(now) {
		t.Errorf("cfg.TableCutoffs = %v, want only a", next.cfg.TableCutoffs)
	}
}

func TestHandleCutoffSubmitted_SetsTableCutoffs(t *testing.T) {
	common := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	own := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	m := newModel(config.Config{})
	m.stage = stageCutoff
	next, _ := m.handleCutoffSubmitted(screens.CutoffSubmittedMsg{Cutoff: common, TableCutoffs: map[string]time.Time{"a": own}})
	if !next.cfg.Cutoff.Equal(common) || !next.cfg.CutoffFor("a").Equal(own) {
		t.Errorf("cfg = Cutoff %v / TableCutoffs %v", next.cfg.Cutoff, next.cfg.TableCutoffs)
	}
}

// 自動を選んだら除外時間の入力画面へ進む。
func TestHandlePhaseBModeSubmitted_AutoGoesToExclude(t *testing.T) {
	m := newModel(config.Config{})
	m.stage = stagePhaseBMode
	next, cmd := m.handlePhaseBModeSubmitted(screens.PhaseBModeSubmittedMsg{Mode: config.PhaseBAuto})
	if next.stage != stagePhaseBExclude {
		t.Errorf("stage = %v, want stagePhaseBExclude", next.stage)
	}
	if cmd == nil {
		t.Error("expected a non-nil Cmd (cursor blink) when entering PhaseBExclude screen")
	}
}

// 自動以外を選んだら除外時間は破棄する（効かない設定を持ち越さない）。
func TestHandlePhaseBModeSubmitted_NonAutoClearsExclude(t *testing.T) {
	ws, _ := config.ParseTimeWindows("23:00-01:00")
	m := newModel(config.Config{PhaseBExclude: ws})
	next, _ := m.handlePhaseBModeSubmitted(screens.PhaseBModeSubmittedMsg{Mode: config.PhaseBManual})
	if next.cfg.PhaseBExclude != nil {
		t.Errorf("cfg.PhaseBExclude = %v, want nil", next.cfg.PhaseBExclude)
	}
}

func TestHandlePhaseBExcludeSubmitted_GoesToConfirm(t *testing.T) {
	ws, _ := config.ParseTimeWindows("23:00-01:00")
	m := newModel(config.Config{PhaseBMode: config.PhaseBAuto})
	m.stage = stagePhaseBExclude
	next, _ := m.handlePhaseBExcludeSubmitted(screens.PhaseBExcludeSubmittedMsg{Windows: ws})
	if next.stage != stageConfirm {
		t.Errorf("stage = %v, want stageConfirm", next.stage)
	}
	if config.FormatTimeWindows(next.cfg.PhaseBExclude) != "23:00-01:00" {
		t.Errorf("cfg.PhaseBExclude = %v", next.cfg.PhaseBExclude)
	}
}

func TestHandleBack_PhaseBExcludeToPhaseBMode(t *testing.T) {
	m := newModel(config.Config{PhaseBMode: config.PhaseBAuto})
	m.stage = stagePhaseBExclude
	next, _ := m.handleBack()
	if next.stage != stagePhaseBMode {
		t.Errorf("stage = %v, want stagePhaseBMode", next.stage)
	}
}

// 自動のときは往路で PhaseBExclude を経由しているため、Confirm からの戻り先もそこになる。
func TestHandleBack_ConfirmToPhaseBExclude_WhenAuto(t *testing.T) {
	m := newModel(config.Config{PhaseBMode: config.PhaseBAuto})
	m.stage = stageConfirm
	next, _ := m.handleBack()
	if next.stage != stagePhaseBExclude {
		t.Errorf("stage = %v, want stagePhaseBExclude", next.stage)
	}
}

// 自動でフェーズA完了時刻が除外時間に入っていれば、フェーズBを始めず待機画面へ進む。
func TestHandlePhaseAResult_AutoInExcludeWindowWaits(t *testing.T) {
	ws, _ := config.ParseTimeWindows("23:00-01:00")
	m := newModel(config.Config{PhaseBMode: config.PhaseBAuto, PhaseBExclude: ws})
	m.stage = stageProgressA
	now := time.Date(2026, 10, 29, 23, 30, 0, 0, time.Local)
	m.now = func() time.Time { return now }
	st := &washover.RunState{RunID: "20261029_220000", TempDBName: "joywatcher_new"}

	next, cmd := m.handlePhaseAResult(phaseAResultMsg{st: st, err: nil})
	if next.stage != stagePhaseBWait {
		t.Fatalf("stage = %v, want stagePhaseBWait", next.stage)
	}
	if cmd == nil {
		t.Error("expected a tick Cmd while waiting")
	}
	want := time.Date(2026, 10, 30, 1, 0, 0, 0, time.Local)
	if !next.phaseBStartAt.Equal(want) {
		t.Errorf("phaseBStartAt = %v, want %v", next.phaseBStartAt, want)
	}

	// 開始予定時刻の前の Tick では待機を続ける。
	now = time.Date(2026, 10, 30, 0, 59, 59, 0, time.Local)
	after, cmd := next.handlePhaseBWaitTick()
	if after.stage != stagePhaseBWait || cmd == nil {
		t.Errorf("stage = %v cmd = %v, want to keep waiting", after.stage, cmd)
	}
}

// 開始予定時刻に達した Tick でフェーズBを開始する。
// RunState は Phase 未設定のため RunPhaseB は CanRunPhaseB で即座に失敗し、DBには接続しない。
func TestHandlePhaseBWaitTick_StartsPhaseBAtStartTime(t *testing.T) {
	ws, _ := config.ParseTimeWindows("23:00-01:00")
	m := newModel(config.Config{PhaseBMode: config.PhaseBAuto, PhaseBExclude: ws})
	m.stage = stagePhaseBWait
	m.runState = &washover.RunState{RunID: "20261029_220000"}
	m.phaseBStartAt = time.Date(2026, 10, 30, 1, 0, 0, 0, time.Local)
	m.now = func() time.Time { return m.phaseBStartAt }

	next, cmd := m.handlePhaseBWaitTick()
	if next.stage != stageProgressB {
		t.Errorf("stage = %v, want stageProgressB", next.stage)
	}
	if cmd == nil {
		t.Error("expected a non-nil Cmd after starting phase B")
	}
}

// 自動でフェーズA完了時刻が除外時間の外なら、待機せずにフェーズBを開始する。
func TestHandlePhaseAResult_AutoOutsideExcludeWindowStartsPhaseB(t *testing.T) {
	ws, _ := config.ParseTimeWindows("23:00-01:00")
	m := newModel(config.Config{PhaseBMode: config.PhaseBAuto, PhaseBExclude: ws})
	m.stage = stageProgressA
	m.now = func() time.Time { return time.Date(2026, 10, 29, 22, 59, 0, 0, time.Local) }
	st := &washover.RunState{RunID: "20261029_220000"}

	next, _ := m.handlePhaseAResult(phaseAResultMsg{st: st, err: nil})
	if next.stage != stageProgressB {
		t.Errorf("stage = %v, want stageProgressB", next.stage)
	}
}

// 待機画面以外に届いた Tick（待機中に画面が変わった後の残り）は無視する。
func TestHandlePhaseBWaitTick_IgnoredOutsideWaitStage(t *testing.T) {
	m := newModel(config.Config{})
	m.stage = stageResult
	next, cmd := m.handlePhaseBWaitTick()
	if next.stage != stageResult || cmd != nil {
		t.Errorf("stage = %v cmd = %v, want unchanged", next.stage, cmd)
	}
}
