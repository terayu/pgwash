// Package config は起動引数・環境変数・TUI入力から設定を組み立てる。
// 優先順位: CLIフラグ > 環境変数 > TUI入力 > 既定値（設計案 5.3節）。
package config

import "time"

// PhaseBMode はフェーズB（接続遮断〜リネーム）の実行タイミング。
type PhaseBMode string

const (
	// PhaseBAuto はフェーズA完了後、成行でフェーズBへ移行する。
	PhaseBAuto PhaseBMode = "auto"
	// PhaseBManual はフェーズA完了時点で停止し、明示的なトリガーを待つ。
	PhaseBManual PhaseBMode = "manual"
	// PhaseBSkip はフェーズBを実行しない。フェーズA完了時点で終了し、コピー先の
	// 一時DBをバックアップとして残す（source は一切変更されず、ダウンタイムもない）。
	PhaseBSkip PhaseBMode = "skip"
)

// BlockMode はフェーズBでの新規接続の遮断方式（設計案 5.4節）。
//
// datallowconn=false は使用しない。スーパーユーザーを含む全接続を拒否するため、
// 続く差分コピーで dblink が source へ接続できなくなり処理が破綻する。
type BlockMode string

const (
	// BlockRevoke は REVOKE CONNECT で一般ユーザーの新規接続を遮断する（既定）。
	// スーパーユーザーは権限チェックをバイパスするため dblink は影響を受けない。
	BlockRevoke BlockMode = "revoke"
	// BlockNone は遮断せず terminate のみ行う。
	// フェーズB中にアプリ側サービスを停止する運用が前提。
	BlockNone BlockMode = "none"
)

// DefaultSchema は対象スキーマの既定値。
const DefaultSchema = "public"

// DefaultStateDir は状態ファイル保存先の既定値。
const DefaultStateDir = ".pgwash"

// DefaultTimeColumn は足切り・差分判定に使う日時列名の既定値。
const DefaultTimeColumn = "ptime"

// Config は1回の洗い替え実行の全設定。
//
// Password は json:"-" により状態ファイルへ永続化されない（設計案 10章）。
// フェーズB単独実行時は PGPASSWORD 環境変数またはマスク入力から再取得する。
type Config struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"-"` // 平文保存を避けるため永続化しない
	Database string `json:"database"`
	Schema   string `json:"schema"`

	// Tables は足切り(Trim)を適用するテーブル名。
	// 「コピーする対象」ではない点に注意（設計案 5.5節）。
	Tables []string `json:"tables"`
	// AllTables が true なら日時列（TimeColumn）を持つ全テーブルに足切りを適用する。
	AllTables bool `json:"all_tables"`
	// TimeColumn は足切り・差分判定に使う日時列名（全テーブル共通）。
	// 空の場合は DefaultTimeColumn とみなす（この項目がない旧版の状態ファイルを読むため）。
	TimeColumn string `json:"time_column,omitempty"`
	// DropTables は新DBへ引き継がないテーブル名。明示指定のみ。
	DropTables []string `json:"drop_tables"`
	// Cutoff は足切り対象テーブル共通の境界日時（TableCutoffs に指定のないテーブルに適用する）。
	Cutoff time.Time `json:"cutoff"`
	// TableCutoffs はテーブル別の足切り日時。指定したテーブルでは Cutoff より優先する。
	TableCutoffs map[string]time.Time `json:"table_cutoffs,omitempty"`

	PhaseBMode PhaseBMode `json:"phase_b_mode"`
	// PhaseBExclude は PhaseBAuto のとき、フェーズBの開始を避ける時間帯。
	// フェーズA完了時刻がこの時間帯に入っていれば、抜けるまで待ってからフェーズBを開始する。
	// 判定するのは開始時刻のみで、フェーズBが時間帯にかかって終わることは防がない。
	PhaseBExclude []TimeWindow `json:"phase_b_exclude,omitempty"`
	BlockMode     BlockMode    `json:"block_mode"`

	PgBinPath string `json:"pg_bin_path"`
	StateDir  string `json:"state_dir"`
	Yes       bool   `json:"-"`
}

// TimeColumnName は足切り・差分判定に使う日時列名を返す。未設定なら DefaultTimeColumn。
func (c Config) TimeColumnName() string {
	if c.TimeColumn == "" {
		return DefaultTimeColumn
	}
	return c.TimeColumn
}

// CutoffFor は table に適用する足切り日時を返す。
// TableCutoffs に指定があればそれを、なければ共通の Cutoff を返す。
func (c Config) CutoffFor(table string) time.Time {
	if t, ok := c.TableCutoffs[table]; ok {
		return t
	}
	return c.Cutoff
}
