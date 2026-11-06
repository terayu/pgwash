package cli

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/terayu/pgwash/internal/db"
	"github.com/spf13/cobra"
)

func newListTablesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-tables",
		Short: "テーブル一覧を表示する",
		Long: "テーブル一覧・日時列（--time-column、既定 ptime）の有無・推定行数・最終日時・サイズを表示する（設計案 5.5節）。\n" +
			"足切り対象の選定と、使用停止済みテーブル（--drop-tables候補）の判別に使う。",
		RunE: runListTables,
	}
}

func runListTables(cmd *cobra.Command, args []string) error {
	setupLogger(logFormatValue(cmd.Flags()))

	// list-tables はTables/Cutoff/PhaseBMode等を必要としないため、cfg.Validate() は呼ばず
	// 接続に必要な項目のみを確認する。
	cfg, err := buildConfig(cmd.Flags())
	if err != nil {
		return NewCodedError(ExitConfigError, err)
	}
	if cfg.Host == "" || cfg.User == "" || cfg.Database == "" {
		return NewCodedError(ExitConfigError, errors.New("接続先ホスト(--host)・ユーザー(--user)・DB名(--dbname)を指定してください"))
	}
	if err := cfg.ResolvePassword(passwordPrompt); err != nil {
		return NewCodedError(ExitConfigError, err)
	}

	ci := db.ConnInfo{Host: cfg.Host, Port: cfg.Port, User: cfg.User, Password: cfg.Password, Database: cfg.Database}
	conn, err := db.Connect(cmd.Context(), ci)
	if err != nil {
		return NewCodedError(ExitConfigError, fmt.Errorf("接続に失敗しました: %w", err))
	}
	defer conn.Close(cmd.Context())

	tables, err := db.ListTables(cmd.Context(), conn, cfg.Schema, cfg.TimeColumnName(), true)
	if err != nil {
		return NewCodedError(ExitConfigError, fmt.Errorf("テーブル一覧の取得に失敗しました: %w", err))
	}

	printTables(tables, cfg.TimeColumnName())
	return nil
}

func printTables(tables []db.TableInfo, timeCol string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "テーブル名\t%s\t推定行数\t最終%s\tサイズ\n", timeCol, timeCol)
	for _, t := range tables {
		hasPtime := "-"
		if t.HasPtime {
			hasPtime = "○"
		}
		lastPtime := "-"
		if t.LastPtime != nil {
			lastPtime = t.LastPtime.Local().Format("2006-01-02")
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", t.Name, hasPtime, t.EstRows, lastPtime, formatBytes(t.SizeBytes))
	}
	w.Flush()
}

// formatBytes は pg_size_pretty 相当の人間可読なサイズ表記に変換する（1024進）。
func formatBytes(n int64) string {
	const unit = 1024.0
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := float64(unit), 0
	for f := float64(n) / unit; f >= unit; f /= unit {
		div *= unit
		exp++
	}
	units := []string{"kB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.1f %s", float64(n)/div, units[exp])
}
