package config

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Flags は CLI から明示的に指定されたフラグの値を保持する。
//
// 「未指定」と「ゼロ値が指定された」を区別する必要がある項目はポインタで持つ
// （nil なら未指定として環境変数・既定値にフォールバックする）。Tables/DropTables
// はスライスの nil/非nil で未指定/指定を区別する（cobra の StringSlice フラグは
// 未指定であれば nil のままになる）。
//
// Password は含まない。パスワードは CLI フラグで直接受け取らない設計のため
// （設計案 5.1節。ps コマンド等でプロセス一覧から見えてしまうことを避ける）、
// PGPASSWORD 環境変数、または ResolvePassword 経由のマスク入力等でのみ設定する。
type Flags struct {
	Host       *string
	Port       *int
	User       *string
	Database   *string
	Schema     *string
	Tables     []string
	AllTables  *bool
	DropTables []string
	TimeColumn *string
	Cutoff     *string // RFC3339形式の文字列。Load内でパースする
	// TableCutoffs は "テーブル名=RFC3339日時" 形式の文字列。Load内でパースする
	TableCutoffs []string
	PhaseBMode   *string
	// PhaseBExclude は "HH:MM-HH:MM" のカンマ区切り。Load内でパースする
	PhaseBExclude *string
	BlockMode     *string
	StateDir      *string
	PgBinPath     *string
	Yes           *bool
}

// Load はCLIフラグ・環境変数・既定値をマージして Config を組み立てる。
// 優先順位: CLIフラグ ＞ 環境変数 ＞ 既定値（設計案 5.3節）。
//
// getenv は環境変数参照用のフック（テストで os.Getenv を差し替えるため）。
// nil を渡した場合は os.Getenv を使う。
//
// Password は PGPASSWORD 環境変数からのみ設定する（CLIフラグは存在しない）。
// 未設定なら空文字列のままとなり、TUI/プロンプトからの入力は呼び出し側が
// ResolvePassword を使って後から補完する。
func Load(flags Flags, getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	cfg := Config{
		Schema:    DefaultSchema,
		StateDir:  DefaultStateDir,
		BlockMode: BlockRevoke,
		Port:      5432,
	}

	cfg.Host = pickString(flags.Host, getenv("PGHOST"), cfg.Host)
	cfg.User = pickString(flags.User, getenv("PGUSER"), cfg.User)
	cfg.Database = pickString(flags.Database, getenv("PGDATABASE"), cfg.Database)
	cfg.Schema = pickString(flags.Schema, getenv("PGWASH_SCHEMA"), cfg.Schema)
	cfg.StateDir = pickString(flags.StateDir, getenv("PGWASH_STATE_DIR"), cfg.StateDir)
	cfg.TimeColumn = pickString(flags.TimeColumn, getenv("PGWASH_TIME_COLUMN"), DefaultTimeColumn)

	if flags.PgBinPath != nil {
		cfg.PgBinPath = *flags.PgBinPath
	}
	if flags.Yes != nil {
		cfg.Yes = *flags.Yes
	}

	port, err := pickInt(flags.Port, getenv("PGPORT"), cfg.Port)
	if err != nil {
		return Config{}, fmt.Errorf("PGPORT の値が不正です: %w", err)
	}
	cfg.Port = port

	// パスワードはCLIフラグを持たない。環境変数のみから取得する。
	cfg.Password = getenv("PGPASSWORD")

	cfg.Tables = pickStringSlice(flags.Tables, getenv("PGWASH_TABLES"))
	cfg.DropTables = pickStringSlice(flags.DropTables, getenv("PGWASH_DROP_TABLES"))

	allTables, err := pickBool(flags.AllTables, getenv("PGWASH_ALL_TABLES"), false)
	if err != nil {
		return Config{}, fmt.Errorf("PGWASH_ALL_TABLES の値が不正です: %w", err)
	}
	cfg.AllTables = allTables

	cutoffStr := pickString(flags.Cutoff, getenv("PGWASH_CUTOFF"), "")
	if cutoffStr != "" {
		cutoff, err := time.Parse(time.RFC3339, cutoffStr)
		if err != nil {
			return Config{}, fmt.Errorf("cutoff日時の解析に失敗しました（RFC3339形式で指定してください）: %w", err)
		}
		cfg.Cutoff = cutoff
	}

	tableCutoffs, err := parseTableCutoffs(pickStringSlice(flags.TableCutoffs, getenv("PGWASH_TABLE_CUTOFFS")))
	if err != nil {
		return Config{}, err
	}
	cfg.TableCutoffs = tableCutoffs

	cfg.PhaseBMode = PhaseBMode(pickString(flags.PhaseBMode, getenv("PGWASH_PHASE_B_MODE"), ""))
	cfg.BlockMode = BlockMode(pickString(flags.BlockMode, getenv("PGWASH_BLOCK_MODE"), string(cfg.BlockMode)))

	exclude, err := ParseTimeWindows(pickString(flags.PhaseBExclude, getenv("PGWASH_PHASE_B_EXCLUDE"), ""))
	if err != nil {
		return Config{}, fmt.Errorf("フェーズBの除外時間が不正です: %w", err)
	}
	cfg.PhaseBExclude = exclude

	return cfg, nil
}

// pickString はCLIフラグ（非nilかつ非空）＞環境変数（非空）＞既定値、の優先順位で値を選ぶ。
func pickString(flagVal *string, envVal, def string) string {
	if flagVal != nil && *flagVal != "" {
		return *flagVal
	}
	if envVal != "" {
		return envVal
	}
	return def
}

func pickInt(flagVal *int, envVal string, def int) (int, error) {
	if flagVal != nil {
		return *flagVal, nil
	}
	if envVal != "" {
		v, err := strconv.Atoi(envVal)
		if err != nil {
			return 0, err
		}
		return v, nil
	}
	return def, nil
}

func pickBool(flagVal *bool, envVal string, def bool) (bool, error) {
	if flagVal != nil {
		return *flagVal, nil
	}
	if envVal != "" {
		v, err := strconv.ParseBool(envVal)
		if err != nil {
			return false, err
		}
		return v, nil
	}
	return def, nil
}

// pickStringSlice はCLIフラグ（非nil）を優先し、なければ環境変数をカンマ区切りで分割する。
func pickStringSlice(flagVal []string, envVal string) []string {
	if flagVal != nil {
		return flagVal
	}
	if envVal == "" {
		return nil
	}
	parts := strings.Split(envVal, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// parseTableCutoffs は "テーブル名=RFC3339日時" 形式の指定をテーブル別の足切り日時へ変換する。
// 指定がなければ nil を返す。同じテーブルの重複指定は意図が曖昧なためエラーとする。
func parseTableCutoffs(specs []string) (map[string]time.Time, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make(map[string]time.Time, len(specs))
	for _, spec := range specs {
		name, value, ok := strings.Cut(spec, "=")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || name == "" || value == "" {
			return nil, fmt.Errorf("テーブル別の足切り日時の形式が不正です（テーブル名=日時 で指定してください）: %q", spec)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("テーブル %s の足切り日時が重複して指定されています", name)
		}
		t, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, fmt.Errorf("テーブル %s の足切り日時の解析に失敗しました（RFC3339形式で指定してください）: %w", name, err)
		}
		out[name] = t
	}
	return out, nil
}

// Validate は Config の整合性を検証する。
//
// パスワードの有無はここでは検証しない。ヘッドレス実行での要否はCLI側の責務とし
// （--yes とあわせてエラーにするか等）、TUI/プロンプトでの後入力を妨げないためである。
func (c Config) Validate() error {
	var errs []string

	if c.Host == "" {
		errs = append(errs, "接続先ホスト(--host / PGHOST)が指定されていません")
	}
	if c.User == "" {
		errs = append(errs, "接続ユーザー(--user / PGUSER)が指定されていません")
	}
	if c.Database == "" {
		errs = append(errs, "対象DB名(--dbname / PGDATABASE)が指定されていません")
	}
	if c.Port <= 0 {
		errs = append(errs, "ポート番号(--port / PGPORT)が不正です")
	}
	if c.Schema == "" {
		errs = append(errs, "スキーマ(--schema)が指定されていません")
	}
	if c.StateDir == "" {
		errs = append(errs, "状態ファイル保存先(--state-dir)が指定されていません")
	}

	if len(c.Tables) > 0 && c.AllTables {
		errs = append(errs, "--tables と --all-tables は同時に指定できません")
	}

	// 足切り（Trim）を要求する指定がある場合は、対象テーブルすべてに足切り日時が必要。
	// --all-tables は対象テーブルが事前に確定しないため、共通の --cutoff を必須とする。
	if c.AllTables && c.Cutoff.IsZero() {
		errs = append(errs, "--all-tables を指定する場合は --cutoff が必要です")
	}
	if !c.AllTables && c.Cutoff.IsZero() {
		var missing []string
		for _, name := range c.Tables {
			if _, ok := c.TableCutoffs[name]; !ok {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			errs = append(errs, fmt.Sprintf("--cutoff を省略する場合は全ての足切り対象に --table-cutoff が必要です（未指定: %s）", strings.Join(missing, ", ")))
		}
	}

	// テーブル別の足切り日時は足切り対象テーブルにのみ指定できる。
	// 効かない指定を黙って受け入れると、指定どおりに足切りされたと誤解させるため。
	var stray []string
	for name := range c.TableCutoffs {
		if slices.Contains(c.DropTables, name) || (!c.AllTables && !slices.Contains(c.Tables, name)) {
			stray = append(stray, name)
		}
	}
	if len(stray) > 0 {
		slices.Sort(stray)
		errs = append(errs, fmt.Sprintf("--table-cutoff は足切り対象（--tables / --all-tables）のテーブルにのみ指定できます: %s", strings.Join(stray, ", ")))
	}

	switch c.BlockMode {
	case BlockRevoke, BlockNone:
	default:
		errs = append(errs, fmt.Sprintf("--block-mode の値が不正です: %q", c.BlockMode))
	}

	switch c.PhaseBMode {
	case PhaseBAuto, PhaseBManual, PhaseBSkip:
	default:
		errs = append(errs, fmt.Sprintf("--phase-b の値が不正です: %q", c.PhaseBMode))
	}
	// 除外時間は自動実行時の開始タイミングにのみ効く。効かない指定を黙って受け入れない。
	if len(c.PhaseBExclude) > 0 && c.PhaseBMode != PhaseBAuto {
		errs = append(errs, "--phase-b-exclude は --phase-b=auto の場合のみ指定できます")
	}

	if len(errs) > 0 {
		return fmt.Errorf("設定が不正です: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ResolvePassword は Password が空の場合に prompt を呼んで補完する。
// 既に PGPASSWORD 環境変数等で設定済みなら何もしない。
//
// prompt には TUI のマスク入力や標準入力プロンプトなど、呼び出し側（cli/tui）の
// 実装を渡す想定。config パッケージ自体は入出力を持たない。
func (c *Config) ResolvePassword(prompt func() (string, error)) error {
	if c.Password != "" || prompt == nil {
		return nil
	}
	pw, err := prompt()
	if err != nil {
		return fmt.Errorf("パスワードの取得に失敗しました: %w", err)
	}
	c.Password = pw
	return nil
}
