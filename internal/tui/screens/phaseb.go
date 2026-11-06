package screens

import (
	"strings"

	"github.com/terayu/pgwash/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// PhaseBModeSubmittedMsg はフェーズB実行タイミングの選択確定を通知する。
type PhaseBModeSubmittedMsg struct {
	Mode config.PhaseBMode
}

// PhaseBModeModel はフェーズB（接続遮断〜リネーム）を自動/手動どちらで行うかの選択画面。
type PhaseBModeModel struct {
	mode config.PhaseBMode
}

// NewPhaseBMode は initial を初期選択として画面を構築する。
// initial が空文字（未設定）なら PhaseBAuto を初期選択とする。
func NewPhaseBMode(initial config.PhaseBMode) PhaseBModeModel {
	if initial != config.PhaseBAuto && initial != config.PhaseBManual && initial != config.PhaseBSkip {
		initial = config.PhaseBAuto
	}
	return PhaseBModeModel{mode: initial}
}

// phaseBModeOrder は選択肢の表示順。
var phaseBModeOrder = []config.PhaseBMode{config.PhaseBAuto, config.PhaseBManual, config.PhaseBSkip}

// MovePhaseBMode は上下キー1回分の選択移動を返す。delta は +1（下）/ -1（上）で、
// 一覧の端では巡回する。current が未知の値なら先頭（PhaseBAuto）を起点とする。
func MovePhaseBMode(current config.PhaseBMode, delta int) config.PhaseBMode {
	idx := 0
	for i, mode := range phaseBModeOrder {
		if mode == current {
			idx = i
			break
		}
	}
	n := len(phaseBModeOrder)
	idx = (idx+delta)%n + n
	idx %= n
	return phaseBModeOrder[idx]
}

// Update はキー入力を処理する。
func (m PhaseBModeModel) Update(msg tea.Msg) (PhaseBModeModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "down", "j", "tab":
		m.mode = MovePhaseBMode(m.mode, 1)
	case "up", "k":
		m.mode = MovePhaseBMode(m.mode, -1)
	case "a":
		m.mode = config.PhaseBAuto
	case "m":
		m.mode = config.PhaseBManual
	case "s":
		m.mode = config.PhaseBSkip
	case "enter":
		return m, func() tea.Msg { return PhaseBModeSubmittedMsg{Mode: m.mode} }
	case "esc":
		return m, backCmd()
	}
	return m, nil
}

func phaseBOptionLine(selected bool, label, desc string) string {
	marker := "  "
	if selected {
		marker = cursorStyle.Render("> ")
	}
	return marker + label + "  " + subtitleStyle.Render(desc)
}

// View はフェーズB実行タイミングの選択画面を描画する。
func (m PhaseBModeModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("フェーズB実行タイミング"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("フェーズBは接続遮断・リネームを伴う実ダウンタイムです。"))
	b.WriteString("\n\n")

	b.WriteString(phaseBOptionLine(m.mode == config.PhaseBAuto, "自動 (auto)",
		"フェーズA完了後、即座に成行でフェーズBへ移行する（除外時間を指定した場合はその時間帯を避ける）"))
	b.WriteString("\n")
	b.WriteString(phaseBOptionLine(m.mode == config.PhaseBManual, "手動 (manual)",
		"フェーズA完了時点で停止し、明示的なトリガーを待つ"))
	b.WriteString("\n")
	b.WriteString(phaseBOptionLine(m.mode == config.PhaseBSkip, "実行しない (skip)",
		"フェーズAのみ実行し、コピー先の一時DBをバックアップとして残す（sourceは変更しない）"))
	b.WriteString("\n")

	b.WriteString(helpStyle.Render("↑↓/a/m/s:選択  enter:次へ  esc:戻る  ctrl+c:終了"))
	return b.String()
}
