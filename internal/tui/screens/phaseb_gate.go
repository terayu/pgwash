package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// PhaseBGateStartMsg はユーザーが [p] を押しフェーズBの開始を指示したことを通知する。
type PhaseBGateStartMsg struct{}

// PhaseBGateQuitMsg はユーザーが [q] を押し、フェーズB手動待ちのまま終了することを通知する。
// 状態はファイルに保存済みのため、後で `pgwash phase-b --run-id=<id>` から再開できる。
type PhaseBGateQuitMsg struct{}

// PhaseBGateModel はフェーズB=手動時、フェーズA完了後に表示する開始待ち画面（設計案9章）。
type PhaseBGateModel struct {
	runID      string
	sourceDB   string
	tempDBName string
}

// NewPhaseBGate は runID・対象DB名・一時DB名を表示する開始待ち画面を構築する。
func NewPhaseBGate(runID, sourceDB, tempDBName string) PhaseBGateModel {
	return PhaseBGateModel{runID: runID, sourceDB: sourceDB, tempDBName: tempDBName}
}

// Update はキー入力を処理する。
func (m PhaseBGateModel) Update(msg tea.Msg) (PhaseBGateModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "p":
		return m, func() tea.Msg { return PhaseBGateStartMsg{} }
	case "q":
		return m, func() tea.Msg { return PhaseBGateQuitMsg{} }
	}
	return m, nil
}

// View はフェーズB開始待ち画面を描画する。
func (m PhaseBGateModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("フェーズA完了 — フェーズB開始待ち"))
	b.WriteString("\n\n")
	b.WriteString("フェーズAが完了しました。フェーズBを開始すると以下が実行されます:\n")
	b.WriteString("  - source (" + m.sourceDB + ") への新規接続の遮断、既存接続の切断\n")
	b.WriteString("  - 差分データの反映（フェーズA完了以降にsourceへ入った分のキャッチアップ）\n")
	b.WriteString("  - source を退避DBへリネーム、一時DB (" + m.tempDBName + ") を source 名へリネーム\n")
	b.WriteString("\n")
	b.WriteString(warnStyle.Render("フェーズB実行中は source への接続が切断され、実ダウンタイムが発生します。"))
	b.WriteString("\n\n")
	b.WriteString("実行ID: " + m.runID + "\n")
	b.WriteString(subtitleStyle.Render("[q] で保留したまま終了した場合、状態はファイルに保存済みです。\n" +
		"'pgwash phase-b --run-id=" + m.runID + "' で後から再開できます。"))
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("p:フェーズB開始  q:保留のまま終了"))
	return b.String()
}
