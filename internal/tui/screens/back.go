package screens

import tea "github.com/charmbracelet/bubbletea"

// BackMsg は現在の画面から1つ前の画面へ戻る要求を通知する。
// 戻り先の判定はルートModel（internal/tui/model.go）が行う。
//
// 最初の画面（Connect）と、実行開始後の画面（ProgressA/PhaseBGate/ProgressB/Result）は
// BackMsg を発行しない（前者は戻り先がなく、後者は戻ると実行状態と画面がズレて危険なため）。
type BackMsg struct{}

// backCmd は BackMsg を発行する tea.Cmd を返す。
func backCmd() tea.Cmd {
	return func() tea.Msg { return BackMsg{} }
}
