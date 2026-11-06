package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/db"
	"github.com/terayu/pgwash/internal/washover"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "ヘッドレスで洗い替えを実行する",
		Long: "フェーズAを実行したのち、--phase-b=auto なら成行でフェーズBまで継続する。\n" +
			"--phase-b-exclude を指定した場合、フェーズA完了時刻がその時間帯に入っていれば抜けるまで待ってからフェーズBを開始する。\n" +
			"--phase-b=manual の場合はフェーズA完了時点で状態を保存して終了する（終了コード2）。\n" +
			"--phase-b=skip の場合はフェーズAのみ実行し、一時DBをバックアップとして残して正常終了する（終了コード0）。\n" +
			"cron等からの完全自動実行を想定しており、--yes の指定が必須。",
		RunE: runRun,
	}
}

func runRun(cmd *cobra.Command, args []string) error {
	setupLogger(logFormatValue(cmd.Flags()))

	cfg, err := buildConfig(cmd.Flags())
	if err != nil {
		return NewCodedError(ExitConfigError, err)
	}

	// 破壊的操作の確認（設計案10章）。ヘッドレス実行では --yes が必須で、未指定ならエラー終了。
	if !cfg.Yes {
		return NewCodedError(ExitConfigError, errors.New("ヘッドレス実行では --yes の指定が必須です（確認プロンプトは対話TUIでのみ表示されます）"))
	}

	if err := cfg.ResolvePassword(passwordPrompt); err != nil {
		return NewCodedError(ExitConfigError, err)
	}

	if err := cfg.Validate(); err != nil {
		return NewCodedError(ExitConfigError, fmt.Errorf("設定エラー: %w", err))
	}

	if err := db.CheckDumpBinaries(cfg.PgBinPath); err != nil {
		return NewCodedError(ExitConfigError, err)
	}

	logRunSummary(cfg)
	if len(cfg.DropTables) > 0 {
		logDropTablesSummary(cmd.Context(), cfg)
	}

	events := make(chan washover.Event, 64)
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		consumeEvents(events)
	}()

	eng := washover.New(cfg, events)
	st, err := eng.RunPhaseA(cmd.Context())
	if err != nil {
		close(events)
		<-consumerDone
		return NewCodedError(ExitPhaseAFailed, fmt.Errorf("フェーズA失敗: %w", err))
	}
	slog.Info("フェーズA完了", "run_id", st.RunID, "temp_db", st.TempDBName)

	if cfg.PhaseBMode == config.PhaseBSkip {
		close(events)
		<-consumerDone
		slog.Info("フェーズBは実行しません。一時DBをバックアップとして残します", "run_id", st.RunID, "backup_db", st.TempDBName)
		return nil
	}

	if cfg.PhaseBMode == config.PhaseBManual {
		close(events)
		<-consumerDone
		slog.Info("フェーズBは手動実行です。'pgwash phase-b --run-id=" + st.RunID + "' で継続してください")
		return NewCodedError(ExitPhaseAWaitingManual, fmt.Errorf("フェーズA完了。フェーズB手動待ち（run-id=%s）", st.RunID))
	}

	if err := waitPhaseBExclude(cmd.Context(), cfg.PhaseBExclude); err != nil {
		close(events)
		<-consumerDone
		// フェーズA完了時点の状態は保存済みのため、手動待ちと同じく phase-b コマンドで再開できる。
		return NewCodedError(ExitPhaseAWaitingManual, fmt.Errorf("フェーズB開始待ちの間に中断しました。'pgwash phase-b --run-id=%s' で再開できます: %w", st.RunID, err))
	}

	err = eng.RunPhaseB(cmd.Context(), st)
	close(events)
	<-consumerDone
	if err != nil {
		return NewCodedError(ExitPhaseBFailed, fmt.Errorf("フェーズB失敗: %w", err))
	}
	slog.Info("フェーズB完了。洗い替えが完了しました", "run_id", st.RunID)
	return nil
}

// waitPhaseBExclude は現在時刻が除外時間に入っていれば、抜けるまで待機する。
// ctx がキャンセルされた場合（SIGINT/SIGTERM）はその時点で ctx.Err() を返す。
func waitPhaseBExclude(ctx context.Context, exclude []config.TimeWindow) error {
	startAt := config.NextOutsideWindows(time.Now(), exclude)
	wait := time.Until(startAt)
	if wait <= 0 {
		return nil
	}
	slog.Info("フェーズBの除外時間のため開始を待機します",
		"exclude", config.FormatTimeWindows(exclude), "start_at", startAt.Format("2006-01-02 15:04:05"))
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// logRunSummary は実行前に破壊的操作の内容を明示する（設計案10章）。
// 「sourceを退避DBへリネームし、新DBと入れ替える」旨と、フェーズB中は接続が切れる旨を記録する。
func logRunSummary(cfg config.Config) {
	slog.Warn("洗い替えを開始します。source を退避DBへリネームし、新DBと入れ替えます。フェーズB実行中は source への接続が切断されます。",
		"host", cfg.Host, "database", cfg.Database, "schema", cfg.Schema, "phase_b_mode", cfg.PhaseBMode, "block_mode", cfg.BlockMode)
}

// logDropTablesSummary は --drop-tables 指定時、対象テーブル名と推定行数を再掲する（設計案10章）。
// 行数取得に失敗しても警告に留め、実行自体は継続する（本来の削除は Engine が行う）。
func logDropTablesSummary(ctx context.Context, cfg config.Config) {
	rows, err := estimateTableRows(ctx, cfg, cfg.DropTables)
	if err != nil {
		slog.Warn("削除対象テーブルの推定行数取得に失敗しました。テーブル名のみ表示します", "err", err)
	}
	for _, t := range cfg.DropTables {
		if r, ok := rows[t]; ok {
			slog.Warn("新DBへ引き継がれず削除されるテーブル", "table", t, "estimated_rows", r)
		} else {
			slog.Warn("新DBへ引き継がれず削除されるテーブル", "table", t)
		}
	}
}

func estimateTableRows(ctx context.Context, cfg config.Config, names []string) (map[string]int64, error) {
	ci := db.ConnInfo{Host: cfg.Host, Port: cfg.Port, User: cfg.User, Password: cfg.Password, Database: cfg.Database}
	conn, err := db.Connect(ctx, ci)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	tables, err := db.ListTables(ctx, conn, cfg.Schema, cfg.TimeColumnName(), true)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	out := make(map[string]int64)
	for _, t := range tables {
		if want[t.Name] {
			out[t.Name] = t.EstRows
		}
	}
	return out, nil
}
