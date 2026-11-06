package screens

import (
	"strings"

	"github.com/terayu/pgwash/internal/config"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// PhaseBExcludeSubmittedMsg はフェーズB除外時間の入力確定を通知する。
// 空欄で確定した場合 Windows は nil（除外時間なし）。
type PhaseBExcludeSubmittedMsg struct {
	Windows []config.TimeWindow
}

// PhaseBExcludeModel はフェーズB=自動時に、フェーズBの開始を避ける時間帯を入力する画面。
type PhaseBExcludeModel struct {
	input textinput.Model
	err   string
}

// NewPhaseBExclude は initial を初期値として画面を構築する。
func NewPhaseBExclude(initial []config.TimeWindow) PhaseBExcludeModel {
	ti := newTextInput("23:00-01:00")
	ti.CharLimit = 128
	ti.Width = 40
	ti.SetValue(config.FormatTimeWindows(initial))
	ti.Focus()
	return PhaseBExcludeModel{input: ti}
}

func (m PhaseBExcludeModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update は入力・送信を処理する。
func (m PhaseBExcludeModel) Update(msg tea.Msg) (PhaseBExcludeModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			ws, err := config.ParseTimeWindows(m.input.Value())
			if err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.err = ""
			return m, func() tea.Msg { return PhaseBExcludeSubmittedMsg{Windows: ws} }
		case "tab":
			if shouldFillPlaceholder(m.input.Value(), m.input.Placeholder) {
				m.input.SetValue(m.input.Placeholder)
				m.input.CursorEnd()
			}
			return m, nil
		case "esc":
			return m, backCmd()
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View はフェーズB除外時間の入力画面を描画する。
func (m PhaseBExcludeModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("フェーズB除外時間"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("フェーズA完了時刻がこの時間帯に入っていれば、時間帯を抜けるまで待ってからフェーズBを開始します。"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("HH:MM-HH:MM 形式、カンマ区切りで複数指定できます。空欄なら除外時間なし（フェーズA完了後すぐに開始）。"))
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render("除外時間"))
	b.WriteString(" ")
	b.WriteString(m.input.View())
	b.WriteString("\n")

	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("エラー: " + m.err))
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("enter:次へ  tab:サンプル入力  esc:戻る  ctrl+c:終了"))
	return b.String()
}
