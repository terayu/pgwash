package screens

import (
	"strings"
	"time"

	"github.com/terayu/pgwash/internal/config"
)

// PhaseBWaitModel はフェーズB=自動時、フェーズA完了時刻が除外時間に入っていた場合の待機画面。
// 待機の終了判定・フェーズBの開始はルートModelが行う。
type PhaseBWaitModel struct {
	runID   string
	startAt time.Time
	exclude []config.TimeWindow
}

// NewPhaseBWait は待機画面を構築する。startAt はフェーズBを開始する予定時刻。
func NewPhaseBWait(runID string, startAt time.Time, exclude []config.TimeWindow) PhaseBWaitModel {
	return PhaseBWaitModel{runID: runID, startAt: startAt, exclude: exclude}
}

// View はフェーズB開始待ち画面を描画する。
func (m PhaseBWaitModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("フェーズA完了 — 除外時間のためフェーズB開始待ち"))
	b.WriteString("\n\n")
	b.WriteString("除外時間:   " + config.FormatTimeWindows(m.exclude) + "\n")
	b.WriteString("開始予定:   " + m.startAt.Local().Format("2006-01-02 15:04:05") + "\n")
	b.WriteString("\n")
	b.WriteString(warnStyle.Render("開始予定時刻になると自動でフェーズBを開始します（接続遮断・リネームによるダウンタイムが発生します）。"))
	b.WriteString("\n\n")
	b.WriteString("実行ID: " + m.runID + "\n")
	b.WriteString(subtitleStyle.Render("ctrl+c で中断した場合も、フェーズA完了時点の状態はファイルに保存済みです。\n" +
		"'pgwash phase-b --run-id=" + m.runID + "' で後から再開できます。"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("ctrl+c:中断して終了"))
	return b.String()
}
