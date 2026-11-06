package cli

import (
	"github.com/terayu/pgwash/internal/db"
	"github.com/terayu/pgwash/internal/tui"
	"github.com/spf13/cobra"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "対話TUIを起動する",
		Long: "接続情報・対象テーブル・足切り日時・フェーズB方式を画面上で確認・調整してから実行する。\n" +
			"起動引数や環境変数で指定した値は、TUIの各入力欄の初期値として反映される（設計案 5.3節）。",
		RunE: runTUI,
	}
}

// runTUI はTUIを起動する。無引数起動時のルートコマンドからも呼ばれる（設計案 3章）。
func runTUI(cmd *cobra.Command, args []string) error {
	logFormat := logFormatValue(cmd.Flags())
	setupLogger(logFormat)

	cfg, err := buildConfig(cmd.Flags())
	if err != nil {
		return NewCodedError(ExitConfigError, err)
	}
	// パスワード未指定でもここではエラーにしない（config.LoadがPGPASSWORDを反映済み）。
	// 空のままならTUIの接続画面でユーザーに入力させる想定。

	// TUI起動前に検証し、フェーズAで初めて失敗してウィザード入力が無駄にならないようにする。
	if err := db.CheckDumpBinaries(cfg.PgBinPath); err != nil {
		return NewCodedError(ExitConfigError, err)
	}

	if err := tui.Run(cfg); err != nil {
		return NewCodedError(ExitConfigError, err)
	}
	return nil
}
