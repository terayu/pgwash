package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/terayu/pgwash/internal/washover"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "実行状態を表示する",
		Long:  "実行中/完了/失敗した処理の状態を表示する。--run-id 省略時は一覧表示する。",
		RunE:  runStatus,
	}
}

func runStatus(cmd *cobra.Command, args []string) error {
	setupLogger(logFormatValue(cmd.Flags()))

	stateDir := stateDirValue(cmd.Flags())
	runID, _ := cmd.Flags().GetString("run-id")

	if runID != "" {
		st, err := washover.LoadState(stateDir, runID)
		if err != nil {
			return NewCodedError(ExitConfigError, fmt.Errorf("状態の読み込みに失敗しました: %w", err))
		}
		printStateDetail(st)
		return nil
	}

	states, err := washover.ListStates(stateDir)
	if err != nil {
		return NewCodedError(ExitConfigError, fmt.Errorf("状態一覧の取得に失敗しました: %w", err))
	}
	if len(states) == 0 {
		fmt.Println("実行履歴はありません")
		return nil
	}
	printStateList(states)
	return nil
}

func printStateDetail(st *washover.RunState) {
	fmt.Printf("実行ID:        %s\n", st.RunID)
	fmt.Printf("フェーズ:      %s\n", st.Phase)
	fmt.Printf("対象DB:        %s@%s:%d\n", st.Config.Database, st.Config.Host, st.Config.Port)
	fmt.Printf("一時DB:        %s\n", st.TempDBName)
	fmt.Printf("退避DB:        %s\n", st.BackupDBName)
	fmt.Printf("開始:          %s\n", formatTime(&st.StartedAt))
	fmt.Printf("フェーズA完了: %s\n", formatTime(st.PhaseACompletedAt))
	fmt.Printf("フェーズB開始: %s\n", formatTime(st.PhaseBStartedAt))
	fmt.Printf("完了:          %s\n", formatTime(st.CompletedAt))

	errCount := 0
	for _, t := range st.Tables {
		if t.Error != "" {
			errCount++
		}
	}
	fmt.Printf("テーブル数:    %d（エラー %d件）\n", len(st.Tables), errCount)
	if st.LastError != "" {
		fmt.Printf("最終エラー:    %s\n", st.LastError)
	}
	if errCount > 0 {
		fmt.Println("\nエラーのあるテーブル:")
		for _, t := range st.Tables {
			if t.Error != "" {
				fmt.Printf("  - %s: %s\n", t.TableName, t.Error)
			}
		}
	}
}

func printStateList(states []*washover.RunState) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "RUN_ID\tPHASE\tDATABASE\tSTARTED_AT\tCOMPLETED_AT")
	for _, st := range states {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			st.RunID, st.Phase, st.Config.Database, formatTime(&st.StartedAt), formatTime(st.CompletedAt))
	}
	w.Flush()
}

func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}
