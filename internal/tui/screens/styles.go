package screens

import "github.com/charmbracelet/lipgloss"

// 色定義。ANSI 256色相当のコードで、ターミナルの明暗どちらでも視認できるものを選ぶ。
const (
	colorPrimary = lipgloss.Color("39")  // シアン系。タイトル・カーソル
	colorAccent  = lipgloss.Color("212") // ピンク系。強調
	colorMuted   = lipgloss.Color("243") // グレー。ヘルプ・補足
	colorSuccess = lipgloss.Color("42")  // 緑。CopyAll・成功
	colorWarn    = lipgloss.Color("214") // オレンジ。Trim・警告
	colorDanger  = lipgloss.Color("203") // 赤。Drop・エラー
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			Padding(0, 1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	labelStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(12)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginTop(1)

	errorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorDanger)

	warnStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWarn)

	successStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSuccess)

	cursorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	headerRowStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	actionCopyAllStyle = lipgloss.NewStyle().Bold(true).Foreground(colorSuccess)
	actionTrimStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorWarn)
	actionDropStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorDanger)
)
