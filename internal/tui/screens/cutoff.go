package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CutoffSubmittedMsg は足切り日時の入力確定を通知する。
// Cutoff は Trim対象のテーブルに共通で適用する日時、TableCutoffs はテーブル別に上書きする日時。
// 全テーブルに個別指定があり共通欄が空の場合、Cutoff はゼロ値となる。
type CutoffSubmittedMsg struct {
	Cutoff       time.Time
	TableCutoffs map[string]time.Time
}

// cutoffLayouts はユーザー入力として受け付ける日時形式。上から順に試す。
var cutoffLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// ParseCutoff は足切り日時の文字列を time.Time へ変換する。
// RFC3339（例 2026-08-01T00:00:00+09:00）のほか、"2006-01-02 15:04:05" や日付のみの
// 入力も受け付ける（後者はローカルタイムゾーン・0時0分0秒として扱う）。
func ParseCutoff(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("足切り日時を入力してください")
	}
	for _, layout := range cutoffLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("日時の形式が不正です（例: 2026-08-01T00:00:00+09:00 または 2026-08-01 00:00:00）")
}

// cutoffSample は入力欄のサンプル値。オフセットなしのローカルタイム表記とし、
// tab で確定したときにそのままローカルタイムとして解釈され（ParseCutoff）、初期値の再表示形式とも揃う。
const cutoffSample = "2026-08-01 00:00:00"

// formatCutoff は足切り日時を入力欄・一覧の表示形式へ変換する。
func formatCutoff(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05")
}

// CutoffModel は足切り日時の入力画面。
// 共通日時の入力欄と、Trim対象テーブルの一覧（テーブル別の上書き）からなる。
type CutoffModel struct {
	input     textinput.Model // 共通日時
	tables    []string
	timeCol   string // 日時列名（説明文の表示用）
	overrides map[string]time.Time

	cursor  int // 0=共通日時の入力欄、1以上=tables[cursor-1] の行
	editing bool
	edit    textinput.Model // テーブル別日時の編集欄（editing中のみ使用）

	err string
}

// NewCutoff は共通日時の初期値 initial（ゼロ値なら空欄）、Trim対象テーブル tables、
// テーブル別日時の初期値 overrides で足切り日時入力画面を構築する。
// overrides のうち tables に含まれないものは無視する。timeCol は説明文に表示する日時列名。
func NewCutoff(initial time.Time, tables []string, overrides map[string]time.Time, timeCol string) CutoffModel {
	ti := newTextInput(cutoffSample)
	ti.CharLimit = 64
	ti.Width = 32
	if !initial.IsZero() {
		ti.SetValue(formatCutoff(initial))
	}
	ti.Focus()

	ov := make(map[string]time.Time)
	for _, name := range tables {
		if t, ok := overrides[name]; ok {
			ov[name] = t
		}
	}

	ed := newTextInput(cutoffSample)
	ed.CharLimit = 64
	ed.Width = 24

	return CutoffModel{input: ti, tables: tables, timeCol: timeCol, overrides: ov, edit: ed}
}

func (m CutoffModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update は入力・送信を処理する。
func (m CutoffModel) Update(msg tea.Msg) (CutoffModel, tea.Cmd) {
	if m.editing {
		return m.updateEditing(msg)
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			return m.submit()
		case "esc":
			return m, backCmd()
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, m.syncFocus()
		case "down":
			if m.cursor < len(m.tables) {
				m.cursor++
			}
			return m, m.syncFocus()
		}
		if m.cursor > 0 {
			name := m.tables[m.cursor-1]
			switch keyMsg.String() {
			case "e":
				m.editing = true
				m.err = ""
				m.edit.SetValue("")
				if t, ok := m.overrides[name]; ok {
					m.edit.SetValue(formatCutoff(t))
				}
				m.edit.CursorEnd()
				return m, m.edit.Focus()
			case "d":
				delete(m.overrides, name)
			}
			return m, nil
		}
		if keyMsg.String() == "tab" {
			if shouldFillPlaceholder(m.input.Value(), m.input.Placeholder) {
				m.input.SetValue(m.input.Placeholder)
				m.input.CursorEnd()
			}
			return m, nil
		}
	}
	if m.cursor > 0 {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateEditing はテーブル別日時の編集中のキー入力を処理する。
// 空欄で確定すると個別指定を解除し、共通日時に戻す。
func (m CutoffModel) updateEditing(msg tea.Msg) (CutoffModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		name := m.tables[m.cursor-1]
		switch keyMsg.String() {
		case "enter":
			if strings.TrimSpace(m.edit.Value()) == "" {
				delete(m.overrides, name)
			} else {
				t, err := ParseCutoff(m.edit.Value())
				if err != nil {
					m.err = err.Error()
					return m, nil
				}
				m.overrides[name] = t
			}
			m.err = ""
			m.editing = false
			m.edit.Blur()
			return m, nil
		case "esc":
			m.err = ""
			m.editing = false
			m.edit.Blur()
			return m, nil
		case "tab":
			if shouldFillPlaceholder(m.edit.Value(), m.edit.Placeholder) {
				m.edit.SetValue(m.edit.Placeholder)
				m.edit.CursorEnd()
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.edit, cmd = m.edit.Update(msg)
	return m, cmd
}

// syncFocus はカーソル位置に応じて共通日時の入力欄のフォーカスを切り替える。
func (m *CutoffModel) syncFocus() tea.Cmd {
	if m.cursor == 0 {
		return m.input.Focus()
	}
	m.input.Blur()
	return nil
}

// submit は入力内容を検証して CutoffSubmittedMsg を発行する。
// 共通日時は、個別指定のないテーブルが1つでもある場合のみ必須とする。
func (m CutoffModel) submit() (CutoffModel, tea.Cmd) {
	var common time.Time
	if strings.TrimSpace(m.input.Value()) != "" || len(m.overrides) < len(m.tables) {
		t, err := ParseCutoff(m.input.Value())
		if err != nil {
			m.err = "共通日時: " + err.Error()
			return m, nil
		}
		common = t
	}
	m.err = ""
	var overrides map[string]time.Time
	if len(m.overrides) > 0 {
		overrides = make(map[string]time.Time, len(m.overrides))
		for k, v := range m.overrides {
			overrides[k] = v
		}
	}
	return m, func() tea.Msg { return CutoffSubmittedMsg{Cutoff: common, TableCutoffs: overrides} }
}

// View は足切り日時入力画面を描画する。
func (m CutoffModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("足切り日時"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(fmt.Sprintf("足切り対象（[切]）のテーブルに適用する日時です。%s >= この日時 のみコピーされます。", m.timeCol)))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("個別に指定しないテーブルには共通日時が適用されます。"))
	b.WriteString("\n\n")

	marker := "  "
	if m.cursor == 0 {
		marker = cursorStyle.Render("> ")
	}
	b.WriteString(marker)
	b.WriteString(labelStyle.Render("共通日時"))
	b.WriteString(" ")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")

	// %-28s はルーン数で桁を数えるため、全角の見出しは表示幅で余白を補う。
	nameHeader := "テーブル名"
	header := "  " + nameHeader + strings.Repeat(" ", 28-lipgloss.Width(nameHeader)) + " 足切り日時"
	b.WriteString(headerRowStyle.Render(header))
	b.WriteString("\n")

	commonLabel := "(未入力)"
	if t, err := ParseCutoff(m.input.Value()); err == nil {
		commonLabel = formatCutoff(t)
	}
	for i, name := range m.tables {
		marker := "  "
		if m.cursor == i+1 {
			marker = cursorStyle.Render("> ")
		}
		var value string
		switch {
		case m.editing && m.cursor == i+1:
			value = m.edit.View()
		case hasKey(m.overrides, name):
			value = actionTrimStyle.Render(formatCutoff(m.overrides[name])) + " " + actionTrimStyle.Render("(個別)")
		default:
			value = commonLabel + " " + subtitleStyle.Render("(共通)")
		}
		b.WriteString(marker)
		b.WriteString(fmt.Sprintf("%-28s %s", truncateName(name, 28), value))
		b.WriteString("\n")
	}

	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("エラー: " + m.err))
		b.WriteString("\n")
	}

	var help string
	switch {
	case m.editing:
		help = "enter:確定（空欄で共通に戻す）  tab:サンプル入力  esc:取消  ctrl+c:終了"
	case m.cursor == 0:
		help = "↑↓:移動  tab:サンプル入力  enter:次へ  esc:戻る  ctrl+c:終了"
	default:
		help = "↑↓:移動  e:個別日時を編集  d:共通に戻す  enter:次へ  esc:戻る  ctrl+c:終了"
	}
	b.WriteString(helpStyle.Render(help))
	return b.String()
}

func hasKey(m map[string]time.Time, k string) bool {
	_, ok := m[k]
	return ok
}
