// engine.go は washover.Engine のオーケストレーション実装（RunPhaseA/RunPhaseB/Abort）を持つ。
// 型定義は api.go、状態永続化は state.go を参照。
package washover

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/db"
	"github.com/jackc/pgx/v5"
)

// New は Engine を生成する。events は nil でもよい（進捗通知なし）。
//
// events を渡す場合、呼び出し側は Engine の処理が終わるまで受信し続ける必要がある
// （api.go のドキュメントに明記された契約）。emit はこの契約を前提にブロッキング送信
// する。受信側が一時的に詰まっても Engine 側でイベントを取りこぼさないことを優先し、
// 非ブロッキング化（バッファなしチャネルへの select+default での破棄）は行わない。
func New(cfg config.Config, events chan<- Event) *Engine {
	return &Engine{cfg: cfg, events: events}
}

// emit は進捗イベントを送信する。events が nil の場合は何もしない。
func (e *Engine) emit(ev Event) {
	if e.events == nil {
		return
	}
	e.events <- ev
}

// ─────────────────────────────────────────────
// TableAction 決定ロジック（設計案 5.5節）
// ─────────────────────────────────────────────

// DecideTableAction は1テーブルの TableAction を決定する純粋関数。
//
//	DropTables に含まれる                          → ActionDrop
//	Tables/AllTables に該当 かつ ptime列あり        → ActionTrim
//	上記以外（非選択、または ptime列なし）           → ActionCopyAll
//
// Tables に明示指定されたのに ptime 列がないテーブルは ActionCopyAll に落とし、
// warn=true を返す（呼び出し側で警告イベントを出すため）。
//
// 設計案5.5節のテーブル処理決定ルール。TUIの初期選択表示と Engine の実処理の
// 双方がこの関数を使うこと（判定ルールを2箇所に重複させないため。TUI側は
// internal/tui/screens.TablesModel.ApplyConfig から利用する）。
func DecideTableAction(table string, hasPtime bool, cfg config.Config) (action TableAction, warn bool) {
	if slices.Contains(cfg.DropTables, table) {
		return ActionDrop, false
	}
	explicit := slices.Contains(cfg.Tables, table)
	if (cfg.AllTables || explicit) && hasPtime {
		return ActionTrim, false
	}
	if explicit && !hasPtime {
		return ActionCopyAll, true
	}
	return ActionCopyAll, false
}

// ─────────────────────────────────────────────
// フェーズB 差分反映の境界値決定（ダウンタイム削減設計案 6章 B4）
// ─────────────────────────────────────────────

// decidePhaseBBoundary はフェーズBでの1テーブルの差分反映方法を決定する純粋関数。
// 判定基準は Action ではなく ptime 列の有無であることに注意（設計案の重要な決定）。
//
//	ptime列なし                             → truncate=true（TRUNCATE + 全件再コピー）
//	ptime列あり かつ maxPtime != nil        → boundary=maxPtime, deleteBoundary=true
//	                                           （境界行を削除してから境界以降を再取得）
//	ptime列あり かつ maxPtime == nil かつ ActionTrim    → boundary=cutoff, deleteBoundary=false
//	ptime列あり かつ maxPtime == nil かつ ActionCopyAll → truncate=true（下記の注意）
//
// maxPtime が nil のときの分岐を落とすと、"= NULL" も ">= NULL" も0件を返すため、
// 差分がエラーも出さずに欠落する（設計案 6章 B4 の重要な注意）。
//
// ActionCopyAll で maxPtime が nil の場合、単純に無条件の CopyAll を再実行すると
// 「ptime が全行 NULL のテーブル」でフェーズAコピー分と重複する
// （フェーズAの CopyAll は WHERE 句なしで NULL 行もコピー済みだが、MAX(ptime) は
// 非NULL値が1つもないとNULLを返すため、増分判定ができない）。
// ActionTrim はフェーズAが WHERE ptime >= cutoff で NULL 行を除外しているため
// destination が確実に空であり、この重複は起こらない。ActionCopyAll のみ
// 増分判定を諦めて TRUNCATE + 全件再コピーの安全側に倒す。
func decidePhaseBBoundary(hasPtime bool, action TableAction, maxPtime *time.Time, cutoff time.Time) (truncate bool, boundary *time.Time, deleteBoundary bool) {
	if !hasPtime {
		return true, nil, false
	}
	if maxPtime != nil {
		b := *maxPtime
		return false, &b, true
	}
	if action == ActionTrim {
		b := cutoff
		return false, &b, false
	}
	// ActionCopyAll かつ maxPtime == nil: 増分判定不能なため全件入れ替え。
	return true, nil, false
}

// rolesWithConnect は ACL のうち PUBLIC 以外で CONNECT 権限を持つロール名を返す。
// RevokeConnect で PUBLIC に加えて個別に REVOKE する対象の抽出に使う（設計案 6章B1）。
func rolesWithConnect(acl []db.ACLEntry) []string {
	var roles []string
	for _, a := range acl {
		if a.PrivilegeType == "CONNECT" && a.Grantee != "PUBLIC" {
			roles = append(roles, a.Grantee)
		}
	}
	return roles
}

// newRunID は実行時刻ベースで一意になる RunID を生成する（例: 20260808_153000）。
func newRunID(now time.Time) string {
	return now.Format("20060102_150405")
}

// validateTableNames は cfg.Tables / cfg.DropTables に実在しないテーブル名が
// 含まれていないかを検証する純粋関数。
//
// DecideTableAction は実在テーブルのみを走査するため、タイプミスで指定した
// テーブル名は警告もエラーもなく黙って無視される（足切りされないまま完了する、
// 削除したいテーブルが残る等）。破壊的操作を始める前に検出して中断させる。
func validateTableNames(cfg config.Config, tables []db.TableInfo) error {
	known := make(map[string]bool, len(tables))
	for _, t := range tables {
		known[t.Name] = true
	}
	var missing []string
	for _, name := range cfg.Tables {
		if !known[name] {
			missing = append(missing, name)
		}
	}
	for _, name := range cfg.DropTables {
		if !known[name] {
			missing = append(missing, name)
		}
	}
	for name := range cfg.TableCutoffs {
		if !known[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		missing = slices.Compact(missing)
		return fmt.Errorf("存在しないテーブルが指定されています: %s", strings.Join(missing, ", "))
	}
	return nil
}

// validateTableCutoffs は TableAction 確定後の results に対し、足切り日時の指定が
// 整合しているかを検証する純粋関数。
//
//   - ActionTrim のテーブルは全て足切り日時（個別または共通）を持つこと。
//     ゼロ値のまま CopyFrom すると WHERE ptime >= '0001-01-01' となり、足切りされずに完了してしまう。
//   - TableCutoffs は ActionTrim のテーブルにのみ指定されていること
//     （ptime列がない等で足切りされないテーブルへの指定を黙って無視しないため）。
func validateTableCutoffs(cfg config.Config, results []TableResult) error {
	var noCutoff, notTrim []string
	for _, tr := range results {
		_, hasOwn := cfg.TableCutoffs[tr.TableName]
		if tr.Action == ActionTrim && cfg.CutoffFor(tr.TableName).IsZero() {
			noCutoff = append(noCutoff, tr.TableName)
		}
		if tr.Action != ActionTrim && hasOwn {
			notTrim = append(notTrim, tr.TableName)
		}
	}
	var errs []string
	if len(noCutoff) > 0 {
		errs = append(errs, fmt.Sprintf("足切り日時が指定されていないテーブルがあります: %s", strings.Join(noCutoff, ", ")))
	}
	if len(notTrim) > 0 {
		errs = append(errs, fmt.Sprintf("足切り対象でないテーブルに足切り日時が指定されています: %s", strings.Join(notTrim, ", ")))
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// afterRenameHint は B4（source rename）成功後に失敗した場合の手動復旧手順を組み立てる。
//
// 設計方針どおり自動ロールバックはしないが（設計案10章）、source は既に退避DB名へ
// リネーム済みで元のDB名が存在しない状態になっているため、運用者が状況を判断して
// 取り消す／完了させるかを選べるよう具体的なSQLを添える。
func afterRenameHint(st *RunState, sourceName string) string {
	return fmt.Sprintf(
		"source DB は既に %s へリネーム済みです。手動で復旧してください:\n"+
			"  洗い替えを取り消す場合 : ALTER DATABASE %s RENAME TO %s;\n"+
			"  洗い替えを完了させる場合: ALTER DATABASE %s RENAME TO %s;",
		st.BackupDBName, st.BackupDBName, sourceName, st.TempDBName, sourceName,
	)
}

// restoreACL は記録済み ACL (p) に基づいて dbname の接続権限を元の状態へ戻す。
//
// RevokeConnect は PUBLIC に加えて rolesWithConnect で得た個別ロールからも CONNECT を
// 剥奪しているため、単純に GrantConnectPublic するだけでは復元として不十分：
//   - p.HasACL == true の場合、個別ロールのCONNECTが復元されず、
//     アプリが非スーパーユーザーなら再接続できないまま残る
//   - 逆に元々 PUBLIC から意図的に CONNECT を剥奪していた場合、
//     GrantConnectPublic によって権限が意図せず緩くなる
//
// そのため p.HasACL に応じて復元方法を分ける。
func restoreACL(ctx context.Context, conn *pgx.Conn, dbname string, p db.SourcePrivileges) error {
	if !p.HasACL {
		// 元は既定状態（PUBLIC に CONNECT あり）なので PUBLIC へ復旧するだけでよい。
		return db.GrantConnectPublic(ctx, conn, dbname)
	}
	// 元は明示的なACL。記録内容を再適用すれば PUBLIC・個別ロールとも元の状態に戻る。
	return db.ApplyPrivileges(ctx, conn, dbname, p)
}

// RunPhaseA は source 稼働中に実行できる処理をすべて行う（ダウンタイムなし）。
func (e *Engine) RunPhaseA(ctx context.Context) (*RunState, error) {
	now := time.Now()
	runID := newRunID(now)
	st := &RunState{
		RunID:        runID,
		Config:       e.cfg,
		Phase:        PhaseARunning,
		StartedAt:    now,
		TempDBName:   fmt.Sprintf("%s_new_%s", e.cfg.Database, runID),
		BackupDBName: fmt.Sprintf("%s_bak_%s", e.cfg.Database, runID),
	}
	e.emit(Event{Kind: EventPhaseStart, Phase: PhaseARunning, Message: "フェーズA開始"})
	if err := SaveState(e.cfg.StateDir, st); err != nil {
		return st, fmt.Errorf("状態保存に失敗しました: %w", err)
	}

	adminCI := db.ConnInfo{Host: e.cfg.Host, Port: e.cfg.Port, User: e.cfg.User, Password: e.cfg.Password, Database: "postgres"}
	adminConn, err := db.Connect(ctx, adminCI)
	if err != nil {
		return e.failA(st, fmt.Errorf("管理用接続に失敗しました: %w", err))
	}
	defer adminConn.Close(ctx)

	// A0: source の権限を記録
	e.emit(Event{Kind: EventStep, Phase: PhaseARunning, Step: "A0", Message: "接続権限を記録しています"})
	privs, err := db.ReadPrivileges(ctx, adminConn, e.cfg.Database)
	if err != nil {
		return e.failA(st, fmt.Errorf("接続権限の記録に失敗しました: %w", err))
	}
	st.SourcePrivs = privs
	if err := SaveState(e.cfg.StateDir, st); err != nil {
		return e.failA(st, fmt.Errorf("状態保存に失敗しました: %w", err))
	}

	// A1: テーブル一覧取得と TableAction 確定
	e.emit(Event{Kind: EventStep, Phase: PhaseARunning, Step: "A1", Message: "テーブル一覧を取得しています"})
	srcCI := adminCI.WithDatabase(e.cfg.Database)
	srcConn, err := db.Connect(ctx, srcCI)
	if err != nil {
		return e.failA(st, fmt.Errorf("source への接続に失敗しました: %w", err))
	}
	tables, err := db.ListTables(ctx, srcConn, e.cfg.Schema, e.cfg.TimeColumnName(), false)
	srcConn.Close(ctx)
	if err != nil {
		return e.failA(st, fmt.Errorf("テーブル一覧の取得に失敗しました: %w", err))
	}
	// 破壊的操作（dump/DB作成/DROP等）を始める前に、--tables / --drop-tables に
	// タイプミス等で実在しないテーブル名が指定されていないかを検証する。
	if err := validateTableNames(e.cfg, tables); err != nil {
		return e.failA(st, err)
	}
	for _, t := range tables {
		action, warn := DecideTableAction(t.Name, t.HasPtime, e.cfg)
		if warn {
			e.emit(Event{
				Kind:    EventLog,
				Phase:   PhaseARunning,
				Table:   t.Name,
				Message: fmt.Sprintf("%s は --tables に指定されましたが %s 列がないため全件コピーします", t.Name, e.cfg.TimeColumnName()),
			})
		}
		st.Tables = append(st.Tables, TableResult{TableName: t.Name, HasPtime: t.HasPtime, Action: action})
	}
	if err := validateTableCutoffs(e.cfg, st.Tables); err != nil {
		return e.failA(st, err)
	}
	if err := SaveState(e.cfg.StateDir, st); err != nil {
		return e.failA(st, fmt.Errorf("状態保存に失敗しました: %w", err))
	}

	// A2: pg_dump --schema-only
	e.emit(Event{Kind: EventStep, Phase: PhaseARunning, Step: "A2", Message: "スキーマをdumpしています"})
	dumpFile := filepath.Join(e.cfg.StateDir, runID+".dump")
	if err := db.Dump(ctx, e.cfg.PgBinPath, srcCI, dumpFile); err != nil {
		return e.failA(st, fmt.Errorf("pg_dump に失敗しました: %w", err))
	}

	// A3: 一時DB作成
	e.emit(Event{Kind: EventStep, Phase: PhaseARunning, Step: "A3", Message: "一時DBを作成しています"})
	if err := db.CreateDatabase(ctx, adminConn, st.TempDBName, privs.Owner); err != nil {
		return e.failA(st, fmt.Errorf("一時DBの作成に失敗しました: %w", err))
	}

	// A4: スキーマ復元（テーブルACLはここで引き継がれる）
	e.emit(Event{Kind: EventStep, Phase: PhaseARunning, Step: "A4", Message: "スキーマを復元しています"})
	tempCI := adminCI.WithDatabase(st.TempDBName)
	if err := db.Restore(ctx, e.cfg.PgBinPath, tempCI, dumpFile); err != nil {
		return e.failA(st, fmt.Errorf("pg_restore に失敗しました: %w", err))
	}
	if err := SaveState(e.cfg.StateDir, st); err != nil {
		return e.failA(st, fmt.Errorf("状態保存に失敗しました: %w", err))
	}

	tempConn, err := db.Connect(ctx, tempCI)
	if err != nil {
		return e.failA(st, fmt.Errorf("一時DBへの接続に失敗しました: %w", err))
	}
	defer tempConn.Close(ctx)

	// A5: ActionDrop のテーブルを一時DB上で DROP（CASCADEなし。入れ替え前なので本番に影響しない）
	e.emit(Event{Kind: EventStep, Phase: PhaseARunning, Step: "A5", Message: "不要テーブルを削除しています"})
	for i, tr := range st.Tables {
		if tr.Action != ActionDrop {
			continue
		}
		e.emit(Event{Kind: EventTableStart, Phase: PhaseARunning, Table: tr.TableName, Action: tr.Action, Index: i + 1, Total: len(st.Tables)})
		if err := db.DropTable(ctx, tempConn, e.cfg.Schema, tr.TableName); err != nil {
			st.Tables[i].Error = err.Error()
			return e.failA(st, fmt.Errorf("テーブル %s の削除に失敗しました: %w", tr.TableName, err))
		}
		e.emit(Event{Kind: EventTableDone, Phase: PhaseARunning, Table: tr.TableName, Action: tr.Action, Index: i + 1, Total: len(st.Tables)})
	}
	if err := SaveState(e.cfg.StateDir, st); err != nil {
		return e.failA(st, fmt.Errorf("状態保存に失敗しました: %w", err))
	}

	// A6: dblink 拡張を有効化
	e.emit(Event{Kind: EventStep, Phase: PhaseARunning, Step: "A6", Message: "dblink拡張を有効化しています"})
	if err := db.EnsureDblink(ctx, tempConn); err != nil {
		return e.failA(st, fmt.Errorf("dblink拡張の有効化に失敗しました: %w", err))
	}

	// A7: 残りのテーブルを TableAction に従ってコピー
	e.emit(Event{Kind: EventStep, Phase: PhaseARunning, Step: "A7", Message: "テーブルをコピーしています"})
	srcDSN := srcCI.DSN()
	for i := range st.Tables {
		tr := &st.Tables[i]
		if tr.Action == ActionDrop {
			continue
		}
		e.emit(Event{Kind: EventTableStart, Phase: PhaseARunning, Table: tr.TableName, Action: tr.Action, Index: i + 1, Total: len(st.Tables)})

		colDefs, err := db.ColumnDefs(ctx, tempConn, e.cfg.Schema, tr.TableName)
		if err != nil {
			tr.Error = err.Error()
			return e.failA(st, fmt.Errorf("テーブル %s の列定義取得に失敗しました: %w", tr.TableName, err))
		}

		var rows int64
		if tr.Action == ActionTrim {
			rows, err = db.CopyFrom(ctx, tempConn, srcDSN, e.cfg.Schema, tr.TableName, colDefs, e.cfg.TimeColumnName(), e.cfg.CutoffFor(tr.TableName))
		} else {
			rows, err = db.CopyAll(ctx, tempConn, srcDSN, e.cfg.Schema, tr.TableName, colDefs)
		}
		if err != nil {
			tr.Error = err.Error()
			return e.failA(st, fmt.Errorf("テーブル %s のコピーに失敗しました: %w", tr.TableName, err))
		}
		tr.RowsPhaseA = rows
		e.emit(Event{Kind: EventTableDone, Phase: PhaseARunning, Table: tr.TableName, Action: tr.Action, Rows: rows, Index: i + 1, Total: len(st.Tables)})

		if err := SaveState(e.cfg.StateDir, st); err != nil {
			return e.failA(st, fmt.Errorf("状態保存に失敗しました: %w", err))
		}
	}

	completedAt := time.Now()
	st.PhaseACompletedAt = &completedAt
	st.Phase = PhaseAComplete
	if err := SaveState(e.cfg.StateDir, st); err != nil {
		return st, fmt.Errorf("状態保存に失敗しました: %w", err)
	}
	e.emit(Event{Kind: EventDone, Phase: PhaseAComplete, Message: "フェーズA完了"})
	return st, nil
}

// failA は失敗を状態に記録・保存し、エラーイベントを通知したうえでエラーを返す。
func (e *Engine) failA(st *RunState, err error) (*RunState, error) {
	st.Phase = PhaseFailed
	st.LastError = err.Error()
	_ = SaveState(e.cfg.StateDir, st) // 状態保存に失敗しても元のエラーを優先して返す
	e.emit(Event{Kind: EventError, Phase: PhaseFailed, Err: err, Message: err.Error()})
	return st, err
}

// RunPhaseB は接続遮断〜リネームまでを行う（実ダウンタイム）。
func (e *Engine) RunPhaseB(ctx context.Context, st *RunState) error {
	if err := CanRunPhaseB(st); err != nil {
		return err
	}

	st.Phase = PhaseBRunning
	startedAt := time.Now()
	st.PhaseBStartedAt = &startedAt
	if err := SaveState(e.cfg.StateDir, st); err != nil {
		return fmt.Errorf("状態保存に失敗しました: %w", err)
	}
	e.emit(Event{Kind: EventPhaseStart, Phase: PhaseBRunning, Message: "フェーズB開始"})

	adminCI := db.ConnInfo{Host: e.cfg.Host, Port: e.cfg.Port, User: e.cfg.User, Password: e.cfg.Password, Database: "postgres"}
	adminConn, err := db.Connect(ctx, adminCI)
	if err != nil {
		return e.failB(st, fmt.Errorf("管理用接続に失敗しました: %w", err))
	}
	defer adminConn.Close(ctx)

	revoked := false
	// restoreAndFail は B4（source rename）到達前の失敗時に、BlockRevoke で
	// 剥奪した CONNECT 権限を source へ復元してからエラーを返す（設計案 10章）。
	// 復元しないとアプリが再接続できないまま残ってしまう。
	// restoreACL を使うのは、RevokeConnect が PUBLIC に加えて個別ロールからも
	// CONNECT を剥奪しているため（単純な GrantConnectPublic だけでは、個別ロールの
	// 復元漏れ、または元々PUBLICから意図的に剥奪していた場合の過剰付与が起こる）。
	restoreAndFail := func(origErr error) error {
		if revoked {
			if grantErr := restoreACL(ctx, adminConn, e.cfg.Database, st.SourcePrivs); grantErr != nil {
				origErr = fmt.Errorf("%w（さらにACL復元にも失敗しました: %v）", origErr, grantErr)
			}
		}
		return e.failB(st, origErr)
	}

	// B1: 新規接続の遮断
	e.emit(Event{Kind: EventStep, Phase: PhaseBRunning, Step: "B1", Message: "新規接続を遮断しています"})
	if e.cfg.BlockMode == config.BlockRevoke {
		roles := rolesWithConnect(st.SourcePrivs.ACL)
		if err := db.RevokeConnect(ctx, adminConn, e.cfg.Database, roles); err != nil {
			return restoreAndFail(fmt.Errorf("接続遮断に失敗しました: %w", err))
		}
		revoked = true
	}

	// B2: 既存接続の強制切断
	e.emit(Event{Kind: EventStep, Phase: PhaseBRunning, Step: "B2", Message: "既存接続を切断しています"})
	if _, err := db.TerminateConnections(ctx, adminConn, e.cfg.Database); err != nil {
		return restoreAndFail(fmt.Errorf("既存接続の切断に失敗しました: %w", err))
	}

	tempCI := adminCI.WithDatabase(st.TempDBName)
	tempConn, err := db.Connect(ctx, tempCI)
	if err != nil {
		return restoreAndFail(fmt.Errorf("一時DBへの接続に失敗しました: %w", err))
	}
	tempClosed := false
	// B4/B5のリネーム前に明示的に閉じるが、それより前に return するパスもあるため
	// defer でも保険をかける（tempClosed で二重クローズを避ける）。
	defer func() {
		if !tempClosed {
			tempConn.Close(ctx)
		}
	}()

	srcDSN := adminCI.WithDatabase(e.cfg.Database).DSN()

	// B3: 差分反映。判定基準は Action ではなく ptime 列の有無（設計案の重要な決定）。
	e.emit(Event{Kind: EventStep, Phase: PhaseBRunning, Step: "B3", Message: "差分を反映しています"})
	for i := range st.Tables {
		tr := &st.Tables[i]
		if tr.Action == ActionDrop {
			continue
		}
		e.emit(Event{Kind: EventTableStart, Phase: PhaseBRunning, Table: tr.TableName, Action: tr.Action, Index: i + 1, Total: len(st.Tables)})

		colDefs, err := db.ColumnDefs(ctx, tempConn, e.cfg.Schema, tr.TableName)
		if err != nil {
			tr.Error = err.Error()
			return restoreAndFail(fmt.Errorf("テーブル %s の列定義取得に失敗しました: %w", tr.TableName, err))
		}

		var maxPtime *time.Time
		if tr.HasPtime {
			maxPtime, err = db.MaxPtime(ctx, tempConn, e.cfg.Schema, tr.TableName, e.cfg.TimeColumnName())
			if err != nil {
				tr.Error = err.Error()
				return restoreAndFail(fmt.Errorf("テーブル %s の MAX(%s) 取得に失敗しました: %w", tr.TableName, e.cfg.TimeColumnName(), err))
			}
		}

		truncate, boundary, deleteBoundary := decidePhaseBBoundary(tr.HasPtime, tr.Action, maxPtime, e.cfg.CutoffFor(tr.TableName))

		var rows int64
		switch {
		case truncate:
			if err := db.Truncate(ctx, tempConn, e.cfg.Schema, tr.TableName); err != nil {
				tr.Error = err.Error()
				return restoreAndFail(fmt.Errorf("テーブル %s のTRUNCATEに失敗しました: %w", tr.TableName, err))
			}
			rows, err = db.CopyAll(ctx, tempConn, srcDSN, e.cfg.Schema, tr.TableName, colDefs)
		case boundary != nil:
			if deleteBoundary {
				if err := db.DeleteAtPtime(ctx, tempConn, e.cfg.Schema, tr.TableName, e.cfg.TimeColumnName(), *boundary); err != nil {
					tr.Error = err.Error()
					return restoreAndFail(fmt.Errorf("テーブル %s の境界行削除に失敗しました: %w", tr.TableName, err))
				}
			}
			rows, err = db.CopyFrom(ctx, tempConn, srcDSN, e.cfg.Schema, tr.TableName, colDefs, e.cfg.TimeColumnName(), *boundary)
		default:
			rows, err = db.CopyAll(ctx, tempConn, srcDSN, e.cfg.Schema, tr.TableName, colDefs)
		}
		if err != nil {
			tr.Error = err.Error()
			return restoreAndFail(fmt.Errorf("テーブル %s の差分反映に失敗しました: %w", tr.TableName, err))
		}
		tr.RowsPhaseB = rows
		e.emit(Event{Kind: EventTableDone, Phase: PhaseBRunning, Table: tr.TableName, Action: tr.Action, Rows: rows, Index: i + 1, Total: len(st.Tables)})
	}
	if err := SaveState(e.cfg.StateDir, st); err != nil {
		return restoreAndFail(fmt.Errorf("状態保存に失敗しました: %w", err))
	}

	// B3完了。B4/B5のリネーム前に一時DBへの接続を明示的に閉じる。
	// PostgreSQL はリネーム対象DBに他接続が残っていると
	// ALTER DATABASE ... RENAME TO を拒否するため、閉じ忘れるとB5が必ず失敗する
	// （かつその時点ではB4がすでに成功しているため、source名が存在しない状態のまま
	// 復旧手段がなくなるという重大な事故になる）。
	if err := tempConn.Close(ctx); err != nil {
		return restoreAndFail(fmt.Errorf("一時DB接続のクローズに失敗しました: %w", err))
	}
	tempClosed = true

	// B4: source rename → 退避DB名
	e.emit(Event{Kind: EventStep, Phase: PhaseBRunning, Step: "B4", Message: "sourceをリネームしています"})
	if err := db.RenameDatabase(ctx, adminConn, e.cfg.Database, st.BackupDBName); err != nil {
		return restoreAndFail(fmt.Errorf("sourceのリネームに失敗しました: %w", err))
	}

	// これ以降は source が既にリネーム済みのため、失敗しても自動ロールバックは行わない
	// （設計案 10章：フェーズB開始後の失敗は状態ファイルを保持し手動対応を促す）。
	// その代わり、運用者が手動で復旧できるよう afterRenameHint でSQLを提示する。

	// 防御的措置: 一時DBへの想定外の残存接続（tempConn は上でクローズ済みだが、念のため）
	// が残っていてもリネームできるよう、自分（adminConn）以外の接続を強制切断する。
	if _, err := db.TerminateConnections(ctx, adminConn, st.TempDBName); err != nil {
		return e.failB(st, fmt.Errorf("一時DBへの残存接続の切断に失敗しました: %w\n%s", err, afterRenameHint(st, e.cfg.Database)))
	}

	// B5: 一時DB rename → source名
	e.emit(Event{Kind: EventStep, Phase: PhaseBRunning, Step: "B5", Message: "新DBをリネームしています"})
	if err := db.RenameDatabase(ctx, adminConn, st.TempDBName, e.cfg.Database); err != nil {
		return e.failB(st, fmt.Errorf("新DBのリネームに失敗しました: %w\n%s", err, afterRenameHint(st, e.cfg.Database)))
	}

	// B6: 新DBへオーナー・DBレベルACL・DB設定を再適用
	e.emit(Event{Kind: EventStep, Phase: PhaseBRunning, Step: "B6", Message: "接続権限を再適用しています"})
	if err := db.ApplyPrivileges(ctx, adminConn, e.cfg.Database, st.SourcePrivs); err != nil {
		return e.failB(st, fmt.Errorf("接続権限の再適用に失敗しました: %w\n%s", err, afterRenameHint(st, e.cfg.Database)))
	}

	// B7: BlockRevoke だった場合、退避DBのACLを復旧（後から検証・参照できるようにするため）。
	// 退避DBはB4でリネームされた元sourceそのものであり、B1で剥奪した状態が残っているため、
	// restoreACL で記録済みACLに基づいて元の状態へ戻す（PUBLIC一括付与だと個別ロール分の
	// 復元漏れや、意図的にPUBLICから剥奪していた場合の過剰付与が起こる）。
	if e.cfg.BlockMode == config.BlockRevoke {
		e.emit(Event{Kind: EventStep, Phase: PhaseBRunning, Step: "B7", Message: "退避DBの接続権限を復旧しています"})
		if err := restoreACL(ctx, adminConn, st.BackupDBName, st.SourcePrivs); err != nil {
			return e.failB(st, fmt.Errorf("退避DBの接続権限復旧に失敗しました: %w\n%s", err, afterRenameHint(st, e.cfg.Database)))
		}
	}

	completedAt := time.Now()
	st.CompletedAt = &completedAt
	st.Phase = PhaseComplete
	if err := SaveState(e.cfg.StateDir, st); err != nil {
		return fmt.Errorf("状態保存に失敗しました: %w", err)
	}
	e.emit(Event{Kind: EventDone, Phase: PhaseComplete, Message: "フェーズB完了"})
	return nil
}

// failB は失敗を状態に記録・保存し、エラーイベントを通知したうえでエラーを返す。
func (e *Engine) failB(st *RunState, err error) error {
	st.Phase = PhaseFailed
	st.LastError = err.Error()
	_ = SaveState(e.cfg.StateDir, st)
	e.emit(Event{Kind: EventError, Phase: PhaseFailed, Err: err, Message: err.Error()})
	return err
}

// Abort はフェーズB開始前（source 未変更）に限り、作成済みの一時DBを削除する。
func (e *Engine) Abort(ctx context.Context, st *RunState) error {
	if st.Phase == PhaseBRunning || st.Phase == PhaseComplete {
		return fmt.Errorf("フェーズB開始後は abort できません（現在のフェーズ: %s）", st.Phase)
	}
	// PhaseFailed であっても、フェーズBが一度でも開始されていた場合は
	// source のリネーム（B4）が既に実行された可能性があり、その場合 TempDBName は
	// 実質「新DB本体」を指しているため削除は危険。契約コメントは Phase のみを条件に
	// しているが、追加の安全策として PhaseBStartedAt も確認する。
	if st.PhaseBStartedAt != nil {
		return fmt.Errorf("フェーズBが開始されているため abort できません（現在のフェーズ: %s）", st.Phase)
	}
	if st.TempDBName == "" {
		return nil
	}

	adminCI := db.ConnInfo{Host: e.cfg.Host, Port: e.cfg.Port, User: e.cfg.User, Password: e.cfg.Password, Database: "postgres"}
	adminConn, err := db.Connect(ctx, adminCI)
	if err != nil {
		return fmt.Errorf("管理用接続に失敗しました: %w", err)
	}
	defer adminConn.Close(ctx)

	if err := db.DropDatabase(ctx, adminConn, st.TempDBName); err != nil {
		return fmt.Errorf("一時DBの削除に失敗しました: %w", err)
	}

	st.Phase = PhaseFailed
	st.LastError = "abort により中断されました"
	if err := SaveState(e.cfg.StateDir, st); err != nil {
		return fmt.Errorf("状態保存に失敗しました: %w", err)
	}
	return nil
}
