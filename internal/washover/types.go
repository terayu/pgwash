// Package washover は洗い替えのオーケストレーションを担う。
// UI（TUI/CLI）から独立しており、進捗は Event チャネル経由で通知する。
package washover

import (
	"time"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/db"
)

// Phase は実行状態。状態ファイルに永続化され、フェーズB単独実行時の再開に使う。
type Phase string

const (
	PhaseNotStarted Phase = "not_started"
	PhaseARunning   Phase = "phase_a_running"
	PhaseAComplete  Phase = "phase_a_complete" // manual時はここで停止
	PhaseBRunning   Phase = "phase_b_running"
	PhaseComplete   Phase = "complete"
	PhaseFailed     Phase = "failed"
)

// TableAction は各テーブルに対する処理。決定ルールは設計案 5.5節。
//
//	DropTables に指定あり                       → ActionDrop
//	Tables/AllTables に該当 かつ ptime列あり     → ActionTrim
//	上記以外（非選択、または ptime列なし）        → ActionCopyAll
//
// 既定が ActionCopyAll（現状維持）であることが重要。
// データを減らす操作は明示指定でのみ発動し、指定漏れによる喪失を起こさない。
type TableAction string

const (
	ActionCopyAll TableAction = "copy_all"
	ActionTrim    TableAction = "trim"
	ActionDrop    TableAction = "drop"
)

// TableResult は1テーブルの処理内容と結果。
type TableResult struct {
	TableName  string      `json:"table_name"`
	HasPtime   bool        `json:"has_ptime"`
	Action     TableAction `json:"action"`
	RowsPhaseA int64       `json:"rows_phase_a"`
	RowsPhaseB int64       `json:"rows_phase_b"` // 差分キャッチアップ分
	Error      string      `json:"error,omitempty"`
}

// RunState は1回の実行の状態。<StateDir>/<RunID>.json に永続化する。
// Config.Password は json:"-" のため保存されない。
type RunState struct {
	RunID  string        `json:"run_id"`
	Config config.Config `json:"config"`
	Phase  Phase         `json:"phase"`

	// SourcePrivs はフェーズA(A0)で記録し、フェーズB(B7)で新DBへ再適用する。
	// フェーズB単独実行時にも必要なため永続化する。
	SourcePrivs db.SourcePrivileges `json:"source_privs"`

	TempDBName   string `json:"temp_db_name"`   // 例: joywatcher_new_20260808_153000
	BackupDBName string `json:"backup_db_name"` // 例: joywatcher_bak_20260808_153000

	StartedAt         time.Time  `json:"started_at"`
	PhaseACompletedAt *time.Time `json:"phase_a_completed_at,omitempty"`
	PhaseBStartedAt   *time.Time `json:"phase_b_started_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`

	Tables []TableResult `json:"tables"`

	// LastError は失敗時の内容。Phase が PhaseFailed のとき設定される。
	LastError string `json:"last_error,omitempty"`
}

// EventKind は進捗イベントの種別。
type EventKind string

const (
	EventPhaseStart EventKind = "phase_start"
	EventStep       EventKind = "step"       // フェーズ内の大きな手順の開始
	EventTableStart EventKind = "table_start"
	EventTableDone  EventKind = "table_done"
	EventLog        EventKind = "log"
	EventError      EventKind = "error"
	EventDone       EventKind = "done"
)

// Event は Engine から UI への進捗通知。
//
// 進捗はテーブル単位でしか取れない（dblink により1テーブル=1SQL文のため）。
// テーブル内部の進捗は存在しないので、UI側は実行中テーブル名とspinnerで示す。
type Event struct {
	Kind    EventKind
	Phase   Phase
	Step    string      // 人間可読なステップ名（EventStep 用）
	Table   string      // 対象テーブル名（EventTable* 用）
	Action  TableAction // 対象テーブルの処理内容
	Rows    int64       // 投入行数（EventTableDone 用）
	Index   int         // 何番目のテーブルか（1始まり）
	Total   int         // テーブル総数
	Message string
	Err     error
}
