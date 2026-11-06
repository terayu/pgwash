package cli

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/terayu/pgwash/internal/washover"
	"github.com/spf13/cobra"
)

func newAbortCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "abort",
		Short: "フェーズB開始前に限り一時DBを削除して中断する",
		Long: "フェーズB開始前（sourceが未変更の段階）に限り、作成済みの一時DBを削除して中断する。\n" +
			"--run-id 省略時は --state-dir 内の最新の実行が対象。",
		RunE: runAbort,
	}
}

func runAbort(cmd *cobra.Command, args []string) error {
	setupLogger(logFormatValue(cmd.Flags()))

	stateDir := stateDirValue(cmd.Flags())
	runID, _ := cmd.Flags().GetString("run-id")

	st, err := loadTargetState(stateDir, runID)
	if err != nil {
		return NewCodedError(ExitConfigError, err)
	}

	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		return NewCodedError(ExitConfigError, errors.New("ヘッドレス実行では --yes の指定が必須です（一時DBを削除します）"))
	}

	cfg := applyEnvPassword(st.Config)
	if err := cfg.ResolvePassword(passwordPrompt); err != nil {
		return NewCodedError(ExitConfigError, err)
	}
	st.Config = cfg

	slog.Warn("中断処理を開始します。一時DBを削除します（sourceには影響しません）", "run_id", st.RunID, "temp_db", st.TempDBName)

	eng := washover.New(cfg, nil)
	if err := eng.Abort(cmd.Context(), st); err != nil {
		return NewCodedError(ExitConfigError, fmt.Errorf("中断処理に失敗しました: %w", err))
	}
	slog.Info("中断処理が完了しました", "run_id", st.RunID)
	return nil
}
