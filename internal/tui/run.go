package tui

import (
	"fmt"
	"os"

	"github.com/terayu/pgwash/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// Run はウィザード形式のTUIを起動する。cfg は起動引数・環境変数から組み立てられた
// 初期値で、Connect画面をはじめ各画面の初期値として反映される（設計案 5.3節）。
func Run(cfg config.Config) error {
	p := tea.NewProgram(newModel(cfg))
	finalModel, err := p.Run()

	// フェーズB実行中に2回目のCtrl+Cで強制終了した場合、TUI終了直後の画面にも
	// 復旧手順を表示するが（Model.View）、ターミナルの状態によって見落とされる
	// おそれがあるため標準エラー出力にも必ず残す。
	if fm, ok := finalModel.(Model); ok && fm.phaseBForceQuit {
		fmt.Fprintln(os.Stderr, "警告: "+phaseBForceQuitMessage(fm.cfg))
	}
	// 除外時間の待機中に終了した場合、フェーズBは未実行のまま。再開方法を残す。
	if fm, ok := finalModel.(Model); ok && fm.stage == stagePhaseBWait && fm.runState != nil {
		fmt.Fprintf(os.Stderr, "フェーズBは未実行です。'pgwash phase-b --run-id=%s' で再開できます。\n", fm.runState.RunID)
	}

	return err
}
