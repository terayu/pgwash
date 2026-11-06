package cli

import (
	"os"
	"strings"

	"github.com/terayu/pgwash/internal/config"
	"github.com/spf13/pflag"
)

// addGlobalFlags はサブコマンド横断で使うフラグを登録する（設計案 5.1/5.2節）。
// 各サブコマンドは自分に必要な値だけを参照する。
// --password フラグは作らない（ps コマンドでプロセス一覧から見えてしまうため。設計案10章）。
func addGlobalFlags(fs *pflag.FlagSet) {
	fs.String("host", "", "接続先ホスト (環境変数 PGHOST)")
	fs.Int("port", 5432, "ポート (環境変数 PGPORT)")
	fs.String("user", "", "接続ユーザー (環境変数 PGUSER)")
	fs.String("dbname", "", "洗い替え対象のDB名 (環境変数 PGDATABASE)")
	fs.String("schema", config.DefaultSchema, "対象スキーマ (環境変数 PGWASH_SCHEMA)")
	fs.String("tables", "", "足切りを適用するテーブル名、カンマ区切り (環境変数 PGWASH_TABLES)")
	fs.Bool("all-tables", false, "日時列(--time-column)を持つ全テーブルに足切りを適用する (環境変数 PGWASH_ALL_TABLES)")
	fs.String("time-column", config.DefaultTimeColumn, "足切り・差分判定に使う日時列名。全テーブル共通 (環境変数 PGWASH_TIME_COLUMN)")
	fs.String("drop-tables", "", "新DBへ引き継がないテーブル名、カンマ区切り (環境変数 PGWASH_DROP_TABLES)")
	fs.String("cutoff", "", "足切り日時。RFC3339形式 (例 2026-08-01T00:00:00+09:00) (環境変数 PGWASH_CUTOFF)")
	fs.StringArray("table-cutoff", nil, "テーブル別の足切り日時。テーブル名=RFC3339形式、複数回指定可 (例 orders=2026-08-01T00:00:00+09:00) (環境変数 PGWASH_TABLE_CUTOFFS、カンマ区切り)")
	fs.String("phase-b", "", "フェーズBの実行タイミング auto|manual|skip。必須指定 (環境変数 PGWASH_PHASE_B_MODE)")
	fs.String("phase-b-exclude", "", "--phase-b=auto のときフェーズBの開始を避ける時間帯。HH:MM-HH:MM、カンマ区切りで複数指定可 (例 23:00-01:00) (環境変数 PGWASH_PHASE_B_EXCLUDE)")
	fs.String("block-mode", string(config.BlockRevoke), "フェーズBでの新規接続遮断方式 revoke|none (環境変数 PGWASH_BLOCK_MODE)")
	fs.String("state-dir", config.DefaultStateDir, "状態ファイルの保存先ディレクトリ (環境変数 PGWASH_STATE_DIR)")
	fs.String("run-id", "", "操作対象の実行ID。省略時は最新の実行を対象とする")
	fs.Bool("yes", false, "確認プロンプトを省略する（ヘッドレス実行では必須）")
	fs.String("pg-bin-path", "", "pg_dump/pg_restoreの実行パス")
	fs.String("log-format", "text", "ログ出力形式 text|json")
}

// splitCSV はカンマ区切り文字列をトリムしつつ分割する。空要素は除外する。
func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// flagsFromCmd はcobraのフラグから config.Flags を組み立てる。
// 「明示指定されたか」を config.Load 側で判定できるよう、fs.Changed のフラグのみ値を設定する
// （未指定ならポインタ/スライスをnilのままにし、環境変数・既定値へのフォールバックを config.Load に委ねる）。
func flagsFromCmd(fs *pflag.FlagSet) config.Flags {
	var f config.Flags

	if fs.Changed("host") {
		v, _ := fs.GetString("host")
		f.Host = &v
	}
	if fs.Changed("port") {
		v, _ := fs.GetInt("port")
		f.Port = &v
	}
	if fs.Changed("user") {
		v, _ := fs.GetString("user")
		f.User = &v
	}
	if fs.Changed("dbname") {
		v, _ := fs.GetString("dbname")
		f.Database = &v
	}
	if fs.Changed("schema") {
		v, _ := fs.GetString("schema")
		f.Schema = &v
	}
	if fs.Changed("tables") {
		v, _ := fs.GetString("tables")
		f.Tables = splitCSV(v)
	}
	if fs.Changed("all-tables") {
		v, _ := fs.GetBool("all-tables")
		f.AllTables = &v
	}
	if fs.Changed("drop-tables") {
		v, _ := fs.GetString("drop-tables")
		f.DropTables = splitCSV(v)
	}
	if fs.Changed("time-column") {
		v, _ := fs.GetString("time-column")
		f.TimeColumn = &v
	}
	if fs.Changed("cutoff") {
		v, _ := fs.GetString("cutoff")
		f.Cutoff = &v
	}
	if fs.Changed("table-cutoff") {
		v, _ := fs.GetStringArray("table-cutoff")
		f.TableCutoffs = v
	}
	if fs.Changed("phase-b") {
		v, _ := fs.GetString("phase-b")
		f.PhaseBMode = &v
	}
	if fs.Changed("phase-b-exclude") {
		v, _ := fs.GetString("phase-b-exclude")
		f.PhaseBExclude = &v
	}
	if fs.Changed("block-mode") {
		v, _ := fs.GetString("block-mode")
		f.BlockMode = &v
	}
	if fs.Changed("state-dir") {
		v, _ := fs.GetString("state-dir")
		f.StateDir = &v
	}
	if fs.Changed("pg-bin-path") {
		v, _ := fs.GetString("pg-bin-path")
		f.PgBinPath = &v
	}
	if fs.Changed("yes") {
		v, _ := fs.GetBool("yes")
		f.Yes = &v
	}
	return f
}

// buildConfig はフラグと環境変数から config.Config を組み立てる（config.Load への薄いラッパー）。
func buildConfig(fs *pflag.FlagSet) (config.Config, error) {
	return config.Load(flagsFromCmd(fs), os.Getenv)
}

// stateDirValue は --state-dir の値を フラグ＞環境変数＞既定値 の優先順位で返す。
// phase-b/status/abort は RunState をロードする前に state-dir だけを知る必要があるため、
// config.Load（DB接続情報一式の組み立て）とは切り離して単独で扱う。
func stateDirValue(fs *pflag.FlagSet) string {
	if fs.Changed("state-dir") {
		v, _ := fs.GetString("state-dir")
		return v
	}
	if v, ok := os.LookupEnv("PGWASH_STATE_DIR"); ok {
		return v
	}
	v, _ := fs.GetString("state-dir")
	return v
}

// logFormatValue は --log-format の値を返す（環境変数には非対応。設計案5.2節に記載なし）。
func logFormatValue(fs *pflag.FlagSet) string {
	v, _ := fs.GetString("log-format")
	return v
}
