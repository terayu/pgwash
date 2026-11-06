// Package cli は pgwash のサブコマンド群（cobra）を定義する。
// UIとしてのCLI/TUIどちらも washover.Engine を呼び出すオーケストレーションの薄いラッパーに徹し、
// DB操作の詳細は internal/db、洗い替え処理の手順は internal/washover が持つ。
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Execute はルートコマンドを構築・実行し、プロセスの終了コードを返す。
// os.Exit は呼び出し側（cmd/pgwash/main.go）が行う。
func Execute(ctx context.Context) int {
	root := newRootCmd()
	root.SilenceErrors = true
	root.SilenceUsage = true

	err := root.ExecuteContext(ctx)
	return handleExecError(err)
}

func handleExecError(err error) int {
	if err == nil {
		return ExitOK
	}
	var ce *CodedError
	if errors.As(err, &ce) {
		// ExitPhaseAWaitingManual は異常ではないため "エラー:" 表示はしない（設計案11章）。
		if ce.Code != ExitPhaseAWaitingManual {
			fmt.Fprintln(os.Stderr, "エラー:", ce.Err)
		}
		return ce.Code
	}
	fmt.Fprintln(os.Stderr, "エラー:", err)
	return ExitConfigError
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "pgwash",
		Short: "PostgreSQL 洗い替えツール",
		Long: "PostgreSQL の「洗い替え」（古いトレンドデータを運用DBから切り離し、\n" +
			"直近データだけを残してDBを作り直す処理）を行うTUI/CLIツール。\n\n" +
			"引数なしで起動するとTUI（対話モード）が立ち上がる。",
		RunE:          runTUI,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	addGlobalFlags(root.PersistentFlags())
	root.MarkFlagsMutuallyExclusive("tables", "all-tables")

	root.AddCommand(newTUICmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newPhaseBCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newAbortCmd())
	root.AddCommand(newListTablesCmd())

	return root
}
