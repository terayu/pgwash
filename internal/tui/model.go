package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/db"
	"github.com/terayu/pgwash/internal/tui/screens"
	"github.com/terayu/pgwash/internal/washover"
	tea "github.com/charmbracelet/bubbletea"
)

// stage はウィザードの現在の画面を表す。
//
// 画面遷移（設計案9章）:
//
//	Connect → (connecting) → Tables → (Trim指定があれば)Cutoff → PhaseBMode → (自動なら)PhaseBExclude → Confirm
//	→ ProgressA → (手動なら)PhaseBGate → ProgressB → Result
//	（自動なら ProgressA から直接 ProgressB へ進む。ただしフェーズA完了時刻が除外時間に
//	入っていれば PhaseBWait で時間帯を抜けるまで待つ。
//	--phase-b=skip なら ProgressA の直後に Result で終わる）
//
// Tables〜Confirm の各画面では esc キーで1つ前の画面へ戻れる（screens.BackMsg、
// 詳細は handleBack を参照）。Connect（最初の画面）と、実行開始後の
// ProgressA/PhaseBGate/ProgressB/Result では戻れない。
type stage int

const (
	stageConnect stage = iota
	stageConnecting
	stageTables
	stageCutoff
	stagePhaseBMode
	stagePhaseBExclude
	stageConfirm
	stageProgressA
	stagePhaseBGate
	stagePhaseBWait
	stageProgressB
	stageResult
)

// phaseAResult は RunPhaseA の完了結果。goroutineからチャネル経由でルートModelへ届く。
type phaseAResult struct {
	st  *washover.RunState
	err error
}

// phaseBResult は RunPhaseB の完了結果。
type phaseBResult struct {
	err error
}

// 以下はいずれも goroutine → tea.Program への通知用 tea.Msg。
type (
	connectErrMsg   struct{ err error }
	tablesLoadedMsg struct{ tables []db.TableInfo }
	engineEventMsg  washover.Event
	eventsClosedMsg struct{}
	phaseAResultMsg phaseAResult
	phaseBResultMsg phaseBResult
	phaseBWaitTick  struct{}
)

// Model はウィザード全体を統括するルートModel。
// 各画面（screens パッケージ）は自分の入力状態のみを持ち、画面間の遷移判定・
// washover.Engine の起動と Event の中継はこの Model が担う。
type Model struct {
	cfg   config.Config
	stage stage
	ctx   context.Context

	width, height int

	connect       screens.ConnectModel
	tables        screens.TablesModel
	cutoff        screens.CutoffModel
	phaseBMode    screens.PhaseBModeModel
	phaseBExclude screens.PhaseBExcludeModel
	confirm       screens.ConfirmModel
	progressA     screens.ProgressModel
	phaseBGate    screens.PhaseBGateModel
	phaseBWait    screens.PhaseBWaitModel
	progressB     screens.ProgressModel
	result        screens.ResultModel

	connInfo db.ConnInfo
	schema   string
	rows     []screens.TableRow

	runState *washover.RunState

	// phaseBStartAt は PhaseBWait 画面で待機中のフェーズB開始予定時刻。
	phaseBStartAt time.Time
	// now は現在時刻の取得用フック（テストで差し替えるため）。nil なら time.Now を使う。
	now func() time.Time

	eventsCh       chan washover.Event
	phaseAResultCh chan phaseAResult
	phaseBResultCh chan phaseBResult

	// phaseBQuitArmed はフェーズB実行中に一度 Ctrl+C が押されたことを示す。
	// フェーズBは source のリネーム・接続遮断を伴う実ダウンタイムであり、
	// --block-mode=revoke の場合は中断すると source の CONNECT 権限が剥奪されたまま
	// 残ってしまう（Engine の restoreAndFail による復旧パスを経由しないため）。
	// そのため ProgressB 中の Ctrl+C は即終了とせず、2回押されて初めて終了する。
	phaseBQuitArmed bool
	phaseBForceQuit bool // 2回目のCtrl+Cで終了する場合に立つ。Run() が終了後に復旧手順を表示する。
}

// newModel は cfg を初期値としてルートModelを構築する。最初の画面は常にConnect。
func newModel(cfg config.Config) Model {
	return Model{
		cfg:     cfg,
		stage:   stageConnect,
		ctx:     context.Background(),
		connect: screens.NewConnect(cfg),
	}
}

// Init はテキストカーソルの点滅など、最初の画面（Connect）の初期化を行う。
func (m Model) Init() tea.Cmd {
	return m.connect.Init()
}

// Update はメッセージの種類に応じて画面遷移・IO起動・Engineイベントの反映を行う。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.tables.SetSize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			if m.stage == stageProgressB {
				return m.handlePhaseBCtrlC()
			}
			return m, tea.Quit
		}
		if m.stage == stageResult {
			// Result画面のヘルプ表示どおり、任意のキーで終了する。
			return m, tea.Quit
		}

	case screens.ConnectSubmittedMsg:
		return m.handleConnectSubmitted(msg)
	case connectErrMsg:
		m.connect.SetError(msg.err)
		m.stage = stageConnect
		return m, m.connect.Init()
	case tablesLoadedMsg:
		return m.handleTablesLoaded(msg)

	case screens.TablesSubmittedMsg:
		return m.handleTablesSubmitted(msg)

	case screens.CutoffSubmittedMsg:
		return m.handleCutoffSubmitted(msg)

	case screens.PhaseBModeSubmittedMsg:
		return m.handlePhaseBModeSubmitted(msg)
	case screens.PhaseBExcludeSubmittedMsg:
		return m.handlePhaseBExcludeSubmitted(msg)

	case screens.ConfirmSubmittedMsg:
		return m.handleConfirmSubmitted()

	case screens.BackMsg:
		return m.handleBack()

	case engineEventMsg:
		return m.handleEngineEvent(msg)
	case eventsClosedMsg:
		return m.handleEventsClosed()
	case phaseAResultMsg:
		return m.handlePhaseAResult(msg)
	case phaseBResultMsg:
		return m.handlePhaseBResult(msg)

	case screens.PhaseBGateStartMsg:
		return m.handlePhaseBGateStart()
	case phaseBWaitTick:
		return m.handlePhaseBWaitTick()
	case screens.PhaseBGateQuitMsg:
		// フェーズA完了時点の状態はファイルに保存済み。'pgwash phase-b' での再開に委ねて終了する。
		return m, tea.Quit
	}

	return m.delegate(msg)
}

// delegate はステージ固有ではないメッセージ（キー入力・spinner/textinputのTick等）を
// 現在アクティブな画面のUpdateへ渡す。
func (m Model) delegate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.stage {
	case stageConnect:
		m.connect, cmd = m.connect.Update(msg)
	case stageTables:
		m.tables, cmd = m.tables.Update(msg)
	case stageCutoff:
		m.cutoff, cmd = m.cutoff.Update(msg)
	case stagePhaseBMode:
		m.phaseBMode, cmd = m.phaseBMode.Update(msg)
	case stagePhaseBExclude:
		m.phaseBExclude, cmd = m.phaseBExclude.Update(msg)
	case stageConfirm:
		m.confirm, cmd = m.confirm.Update(msg)
	case stageProgressA:
		m.progressA, cmd = m.progressA.Update(msg)
	case stagePhaseBGate:
		m.phaseBGate, cmd = m.phaseBGate.Update(msg)
	case stageProgressB:
		m.progressB, cmd = m.progressB.Update(msg)
	}
	return m, cmd
}

// ─────────────────────────────────────────────
// Connect → Tables（IOを伴う遷移）
// ─────────────────────────────────────────────

func (m Model) handleConnectSubmitted(msg screens.ConnectSubmittedMsg) (Model, tea.Cmd) {
	m.connInfo = msg.ConnInfo
	m.schema = msg.Schema
	m.cfg.Host = msg.ConnInfo.Host
	m.cfg.Port = msg.ConnInfo.Port
	m.cfg.User = msg.ConnInfo.User
	m.cfg.Password = msg.ConnInfo.Password
	m.cfg.Database = msg.ConnInfo.Database
	m.cfg.Schema = msg.Schema
	m.stage = stageConnecting
	return m, connectAndListTablesCmd(m.ctx, msg.ConnInfo, msg.Schema, m.cfg.TimeColumnName())
}

// connectAndListTablesCmd は接続とテーブル一覧取得（IO）を行い、結果をMsgとして返す。
func connectAndListTablesCmd(ctx context.Context, ci db.ConnInfo, schema, timeCol string) tea.Cmd {
	return func() tea.Msg {
		conn, err := db.Connect(ctx, ci)
		if err != nil {
			return connectErrMsg{err: err}
		}
		defer conn.Close(ctx)

		tables, err := db.ListTables(ctx, conn, schema, timeCol, true)
		if err != nil {
			return connectErrMsg{err: err}
		}
		return tablesLoadedMsg{tables: tables}
	}
}

func (m Model) handleTablesLoaded(msg tablesLoadedMsg) (Model, tea.Cmd) {
	tbl := screens.NewTables(m.cfg.Database, m.cfg.TimeColumnName(), msg.tables)
	// 起動引数/環境変数で事前指定された --tables/--all-tables/--drop-tables を初期選択へ反映する。
	tbl.ApplyConfig(m.cfg)
	tbl.SetSize(m.width, m.height)
	m.tables = tbl
	m.stage = stageTables
	return m, nil
}

// ─────────────────────────────────────────────
// Tables → (Cutoff) → PhaseBMode → Confirm
// ─────────────────────────────────────────────

// deriveTableConfig はTables画面で確定した行を、washover.Engine が参照する
// cfg.Tables（足切り対象）/ cfg.DropTables（削除対象）へ変換する。
// Engine 自身はテーブル一覧を再取得し、cfg のこれらのフィールドから TableAction を
// 決定し直す（screens.TableRow をそのままEngineへ渡す経路はない）ため、この変換が必要。
func deriveTableConfig(rows []screens.TableRow) (trim, drop []string) {
	for _, r := range rows {
		switch r.Action {
		case washover.ActionTrim:
			trim = append(trim, r.Info.Name)
		case washover.ActionDrop:
			drop = append(drop, r.Info.Name)
		}
	}
	return trim, drop
}

func (m Model) handleTablesSubmitted(msg screens.TablesSubmittedMsg) (Model, tea.Cmd) {
	m.rows = msg.Rows
	trim, drop := deriveTableConfig(msg.Rows)
	m.cfg.Tables = trim
	m.cfg.DropTables = drop
	m.cfg.AllTables = false // Tables画面で行ごとに確定済みのため、以後は明示リストのみを使う
	m.cfg.TableCutoffs = pruneTableCutoffs(m.cfg.TableCutoffs, trim)

	if len(trim) == 0 {
		// 足切り対象が1つもなければCutoff入力は不要（設計案9章）。
		m.stage = stagePhaseBMode
		m.phaseBMode = screens.NewPhaseBMode(m.cfg.PhaseBMode)
		return m, nil
	}
	m.stage = stageCutoff
	m.cutoff = screens.NewCutoff(m.cfg.Cutoff, trim, m.cfg.TableCutoffs, m.cfg.TimeColumnName())
	return m, m.cutoff.Init()
}

// pruneTableCutoffs はテーブル別の足切り日時のうち、足切り対象 trim に含まれるものだけを残す。
// Tables画面で足切り対象から外したテーブルの個別日時が残ると、Engine の検証でエラーとなるため。
func pruneTableCutoffs(cutoffs map[string]time.Time, trim []string) map[string]time.Time {
	var out map[string]time.Time
	for _, name := range trim {
		if t, ok := cutoffs[name]; ok {
			if out == nil {
				out = make(map[string]time.Time)
			}
			out[name] = t
		}
	}
	return out
}

func (m Model) handleCutoffSubmitted(msg screens.CutoffSubmittedMsg) (Model, tea.Cmd) {
	m.cfg.Cutoff = msg.Cutoff
	m.cfg.TableCutoffs = msg.TableCutoffs
	m.stage = stagePhaseBMode
	m.phaseBMode = screens.NewPhaseBMode(m.cfg.PhaseBMode)
	return m, nil
}

func (m Model) handlePhaseBModeSubmitted(msg screens.PhaseBModeSubmittedMsg) (Model, tea.Cmd) {
	m.cfg.PhaseBMode = msg.Mode
	if msg.Mode == config.PhaseBAuto {
		m.stage = stagePhaseBExclude
		m.phaseBExclude = screens.NewPhaseBExclude(m.cfg.PhaseBExclude)
		return m, m.phaseBExclude.Init()
	}
	// 除外時間は自動実行時の開始タイミングにのみ効くため、自動以外では持ち越さない。
	m.cfg.PhaseBExclude = nil
	m.stage = stageConfirm
	m.confirm = screens.NewConfirm(m.cfg, m.rows)
	return m, nil
}

func (m Model) handlePhaseBExcludeSubmitted(msg screens.PhaseBExcludeSubmittedMsg) (Model, tea.Cmd) {
	m.cfg.PhaseBExclude = msg.Windows
	m.stage = stageConfirm
	m.confirm = screens.NewConfirm(m.cfg, m.rows)
	return m, nil
}

// handleBack は screens.BackMsg（esc キー）を受けて1つ前の画面へ戻る。
// 戻れるのは Tables/Cutoff/PhaseBMode/PhaseBExclude/Confirm の画面のみ。Connect は最初の画面のため
// 戻り先がなく、ProgressA/PhaseBGate/ProgressB/Result は実行開始後で画面を戻すと
// 実行状態とズレて危険なため対象外（画面遷移コメント参照）。
// 各画面のModelはルートModelのフィールドとして保持されたまま作り直さないため、
// 戻ってもTablesでの選択などの入力状態はそのまま残る。
func (m Model) handleBack() (Model, tea.Cmd) {
	switch m.stage {
	case stageTables:
		m.stage = stageConnect
		m.connect.SetError(nil)
		return m, m.connect.Init()
	case stageCutoff:
		m.stage = stageTables
		return m, nil
	case stagePhaseBMode:
		// 足切り対象が無ければ往路でCutoffを飛ばしているため、戻り先もTablesとなる。
		if len(m.cfg.Tables) == 0 {
			m.stage = stageTables
			return m, nil
		}
		m.stage = stageCutoff
		return m, m.cutoff.Init()
	case stagePhaseBExclude:
		m.stage = stagePhaseBMode
		return m, nil
	case stageConfirm:
		// 自動のときは往路で PhaseBExclude を経由しているため、戻り先もそこになる。
		if m.cfg.PhaseBMode == config.PhaseBAuto {
			m.stage = stagePhaseBExclude
			return m, m.phaseBExclude.Init()
		}
		m.stage = stagePhaseBMode
		return m, nil
	}
	return m, nil
}

// ─────────────────────────────────────────────
// Confirm → ProgressA → (PhaseBGate) → ProgressB → Result
// ─────────────────────────────────────────────

// waitForEvent は washover.Event を1件受信してMsgへ変換するtea.Cmd。
// bubbletea の定石どおり、Updateで受け取るたびに再度このCmdを発行してチャネル監視を継続する。
// チャネルがcloseされた場合は eventsClosedMsg を返す。
func waitForEvent(ch chan washover.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return eventsClosedMsg{}
		}
		return engineEventMsg(ev)
	}
}

func waitForPhaseAResult(ch chan phaseAResult) tea.Cmd {
	return func() tea.Msg { return phaseAResultMsg(<-ch) }
}

func waitForPhaseBResult(ch chan phaseBResult) tea.Cmd {
	return func() tea.Msg { return phaseBResultMsg(<-ch) }
}

func (m Model) handleConfirmSubmitted() (Model, tea.Cmd) {
	m.eventsCh = make(chan washover.Event, 64)
	m.phaseAResultCh = make(chan phaseAResult, 1)
	eng := washover.New(m.cfg, m.eventsCh)
	m.progressA = screens.NewProgress("フェーズA")
	m.stage = stageProgressA

	ch, resultCh, ctx := m.eventsCh, m.phaseAResultCh, m.ctx
	// washover のEvent送信はブロッキング契約（api.go参照）のため、受信専用のCmdループ
	// （waitForEvent）が動き続ける前提でgoroutine側から送り続けてよい。
	// RunPhaseA完了後にチャネルをcloseし、受信側が全イベントを読み切ったこと
	// （eventsClosedMsg）を確認してから結果を取り出す設計にすることで、
	// EventDone等の取りこぼしを防ぐ。
	go func() {
		st, err := eng.RunPhaseA(ctx)
		close(ch)
		resultCh <- phaseAResult{st: st, err: err}
	}()

	return m, tea.Batch(m.progressA.Init(), waitForEvent(m.eventsCh))
}

func (m Model) handleEngineEvent(msg engineEventMsg) (Model, tea.Cmd) {
	switch m.stage {
	case stageProgressA:
		m.progressA.ApplyEvent(washover.Event(msg))
	case stageProgressB:
		m.progressB.ApplyEvent(washover.Event(msg))
	}
	return m, waitForEvent(m.eventsCh)
}

func (m Model) handleEventsClosed() (Model, tea.Cmd) {
	switch m.stage {
	case stageProgressA:
		return m, waitForPhaseAResult(m.phaseAResultCh)
	case stageProgressB:
		return m, waitForPhaseBResult(m.phaseBResultCh)
	}
	return m, nil
}

func (m Model) handlePhaseAResult(msg phaseAResultMsg) (Model, tea.Cmd) {
	m.runState = msg.st
	if msg.err != nil {
		m.stage = stageResult
		m.result = screens.NewResult(msg.st, msg.err, m.cfg.StateDir)
		return m, nil
	}
	if m.cfg.PhaseBMode == config.PhaseBSkip {
		// フェーズBは実行しない。一時DBをバックアップとして残したまま結果画面へ進む。
		m.stage = stageResult
		m.result = screens.NewResult(msg.st, nil, m.cfg.StateDir)
		return m, nil
	}
	if m.cfg.PhaseBMode == config.PhaseBManual {
		m.stage = stagePhaseBGate
		m.phaseBGate = screens.NewPhaseBGate(msg.st.RunID, m.cfg.Database, msg.st.TempDBName)
		return m, nil
	}
	now := m.currentTime()
	startAt := config.NextOutsideWindows(now, m.cfg.PhaseBExclude)
	if startAt.After(now) {
		m.stage = stagePhaseBWait
		m.phaseBStartAt = startAt
		m.phaseBWait = screens.NewPhaseBWait(msg.st.RunID, startAt, m.cfg.PhaseBExclude)
		return m, phaseBWaitTickCmd()
	}
	return m.startPhaseB()
}

func (m Model) currentTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// phaseBWaitTickCmd は PhaseBWait 画面で開始予定時刻の到来を確認するための定期Tick。
func phaseBWaitTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return phaseBWaitTick{} })
}

// handlePhaseBWaitTick は開始予定時刻に達していればフェーズBを開始し、未達なら待機を続ける。
func (m Model) handlePhaseBWaitTick() (Model, tea.Cmd) {
	if m.stage != stagePhaseBWait {
		return m, nil
	}
	if m.currentTime().Before(m.phaseBStartAt) {
		return m, phaseBWaitTickCmd()
	}
	return m.startPhaseB()
}

func (m Model) handlePhaseBGateStart() (Model, tea.Cmd) {
	return m.startPhaseB()
}

// startPhaseB はフェーズBのEngine実行をgoroutineで開始する。
// 自動モードでのフェーズA完了直後、または手動モードでPhaseBGate画面から[p]が
// 押されたときのいずれからも呼ばれる。
func (m Model) startPhaseB() (Model, tea.Cmd) {
	m.eventsCh = make(chan washover.Event, 64)
	m.phaseBResultCh = make(chan phaseBResult, 1)
	eng := washover.New(m.cfg, m.eventsCh)
	m.progressB = screens.NewProgress("フェーズB")
	m.stage = stageProgressB

	ch, resultCh, ctx, st := m.eventsCh, m.phaseBResultCh, m.ctx, m.runState
	go func() {
		err := eng.RunPhaseB(ctx, st)
		close(ch)
		resultCh <- phaseBResult{err: err}
	}()

	return m, tea.Batch(m.progressB.Init(), waitForEvent(m.eventsCh))
}

func (m Model) handlePhaseBResult(msg phaseBResultMsg) (Model, tea.Cmd) {
	m.stage = stageResult
	m.result = screens.NewResult(m.runState, msg.err, m.cfg.StateDir)
	return m, nil
}

// phaseBQuitWarning はフェーズB実行中に1回目のCtrl+Cが押されたときに表示する警告。
const phaseBQuitWarning = "フェーズB実行中です。ここで中断すると source の接続権限が剥奪されたまま残り、" +
	"アプリが再接続できなくなる恐れがあります。どうしても中断する場合はもう一度 Ctrl+C を押してください。"

// phaseBForceQuitMessage は2回目のCtrl+Cで強制終了する際の復旧手順。
// --block-mode=revoke のときのみ GRANT CONNECT による復旧手順を示す
// （BlockNone は元々遮断していないため、剥奪されたままの権限は存在しない）。
func phaseBForceQuitMessage(cfg config.Config) string {
	if cfg.BlockMode != config.BlockRevoke {
		return "強制終了しました。"
	}
	return fmt.Sprintf(
		"強制終了しました。source DB (%s) の接続権限が剥奪されたままの可能性があります。\n"+
			"GRANT CONNECT ON DATABASE %s TO PUBLIC; で復旧してください。",
		cfg.Database, cfg.Database)
}

// handlePhaseBCtrlC はフェーズB実行中のCtrl+Cを処理する。
// フェーズBはsourceのリネーム・接続遮断を伴う実ダウンタイムであり、
// --block-mode=revoke の場合は中断するとEngineの復旧パス（restoreAndFail）を
// 経由しないまま source の CONNECT 権限が剥奪された状態が残ってしまう。
// そのため1回目は終了せず警告のみ表示し、2回目で初めて終了する。
func (m Model) handlePhaseBCtrlC() (Model, tea.Cmd) {
	if !m.phaseBQuitArmed {
		m.phaseBQuitArmed = true
		return m, nil
	}
	m.phaseBForceQuit = true
	return m, tea.Quit
}

// View は現在のステージに応じた画面を描画する。
func (m Model) View() string {
	var body string
	switch m.stage {
	case stageConnect:
		body = m.connect.View()
	case stageConnecting:
		body = "接続中...\n"
	case stageTables:
		body = m.tables.View()
	case stageCutoff:
		body = m.cutoff.View()
	case stagePhaseBMode:
		body = m.phaseBMode.View()
	case stagePhaseBExclude:
		body = m.phaseBExclude.View()
	case stageConfirm:
		body = m.confirm.View()
	case stageProgressA:
		body = m.progressA.View()
	case stagePhaseBGate:
		body = m.phaseBGate.View()
	case stagePhaseBWait:
		body = m.phaseBWait.View()
	case stageProgressB:
		body = m.progressB.View()
		if m.phaseBForceQuit {
			body += "\n\n" + fatalErrorStyle.Render(phaseBForceQuitMessage(m.cfg))
		} else if m.phaseBQuitArmed {
			body += "\n\n" + fatalErrorStyle.Render(phaseBQuitWarning)
		}
	case stageResult:
		body = m.result.View()
	}
	return appStyle.Render(body)
}
