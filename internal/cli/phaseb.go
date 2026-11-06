package cli

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/terayu/pgwash/internal/washover"
	"github.com/spf13/cobra"
)

func newPhaseBCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "phase-b",
		Short: "保留中のフェーズBを実行する",
		Long: "'pgwash run --phase-b=manual' で保留された処理を、任意のタイミング\n" +
			"（メンテナンス時間帯など）で実行する。--run-id 省略時は --state-dir 内の最新の実行が対象。",
		RunE: runPhaseB,
	}
}

func runPhaseB(cmd *cobra.Command, args []string) error {
	setupLogger(logFormatValue(cmd.Flags()))

	stateDir := stateDirValue(cmd.Flags())
	runID, _ := cmd.Flags().GetString("run-id")

	st, err := loadTargetState(stateDir, runID)
	if err != nil {
		return NewCodedError(ExitConfigError, err)
	}

	if err := washover.CanRunPhaseB(st); err != nil {
		return NewCodedError(ExitConfigError, fmt.Errorf("フェーズBを開始できません: %w", err))
	}

	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		return NewCodedError(ExitConfigError, errors.New("ヘッドレス実行では --yes の指定が必須です（フェーズBはsourceへの接続切断・リネームを伴います）"))
	}

	cfg := applyEnvPassword(st.Config)
	if err := cfg.ResolvePassword(passwordPrompt); err != nil {
		return NewCodedError(ExitConfigError, err)
	}
	st.Config = cfg

	slog.Warn("フェーズBを開始します。sourceへの新規接続を遮断し、退避DBへリネームします。実行中は接続が切断されます。",
		"run_id", st.RunID, "database", cfg.Database, "block_mode", cfg.BlockMode)

	events := make(chan washover.Event, 64)
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		consumeEvents(events)
	}()

	eng := washover.New(cfg, events)
	err = eng.RunPhaseB(cmd.Context(), st)
	close(events)
	<-consumerDone
	if err != nil {
		return NewCodedError(ExitPhaseBFailed, fmt.Errorf("フェーズB失敗: %w", err))
	}
	slog.Info("フェーズB完了。洗い替えが完了しました", "run_id", st.RunID)
	return nil
}

// loadTargetState は runID を指定の状態、または省略時は最新の状態を読み込む。
func loadTargetState(stateDir, runID string) (*washover.RunState, error) {
	if runID == "" {
		return washover.LatestState(stateDir)
	}
	return washover.LoadState(stateDir, runID)
}
