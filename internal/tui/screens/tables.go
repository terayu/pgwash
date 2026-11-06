// Package screens はTUIウィザードの各画面（bubbletea Model）を提供する。
// ルートModel（internal/tui/model.go）が画面遷移を管理し、各画面は自分の入力状態のみを持つ。
package screens

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/db"
	"github.com/terayu/pgwash/internal/washover"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TableRow はTables画面上の1行。db.TableInfo に、ユーザーが選択した処理を添える。
//
// 既定は washover.ActionCopyAll（全件コピー）。これは「選択＝足切り対象」であって
// 「選択＝コピー対象」ではないという設計案5.5節の原則をUI側でも徹底するためで、
// 何も操作しなければ Action は一切変わらずデータは減らない。
type TableRow struct {
	Info   db.TableInfo
	Action washover.TableAction
	// Note は行に添える補足（例: 日時列がないため足切り指定を適用できなかった旨）。
	// 空文字なら表示しない。
	Note string
}

// TablesSubmittedMsg はTables画面でユーザーが enter を押し、行ごとの処理確定を通知する。
type TablesSubmittedMsg struct {
	Rows []TableRow
}

// TablesModel はテーブル一覧・行ごとの処理割り当て画面。
type TablesModel struct {
	database string
	timeCol  string // 日時列名（表示用）
	rows     []TableRow

	cursor int // rows のうち現在フィルタにマッチしているものの中でのカーソル位置（visibleIndices内のインデックス）

	filterMode  bool
	filterInput textinput.Model
	filterQuery string

	width, height int
}

// NewTables はテーブル一覧からTables画面モデルを構築する。
// 既定の処理は全行 washover.ActionCopyAll。名前順に並べ、操作性を安定させる。
// timeCol は日時列名で、列見出しの表示に使う。
func NewTables(database, timeCol string, infos []db.TableInfo) TablesModel {
	rows := make([]TableRow, len(infos))
	for i, info := range infos {
		rows[i] = TableRow{Info: info, Action: washover.ActionCopyAll}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Info.Name < rows[j].Info.Name })

	fi := textinput.New()
	fi.Placeholder = "テーブル名で絞り込み"
	fi.CharLimit = 128
	fi.Width = 40

	return TablesModel{
		database:    database,
		timeCol:     timeCol,
		rows:        rows,
		filterInput: fi,
		width:       80,
		height:      20,
	}
}

// SetSize はレイアウト計算に使う表示領域を設定する。
func (m *TablesModel) SetSize(width, height int) {
	m.width, m.height = width, height
}

// ApplyConfig は起動引数/環境変数で事前指定された cfg.Tables/AllTables/DropTables を
// 各行の初期 Action へ反映する（設計案 5.3節：起動引数の値は各画面の初期値とする）。
// 判定ルールは washover.DecideTableAction そのものを使う。「どのテーブルのデータを
// 減らすか」という安全上重要なルールをTUIとEngineで重複させないため（TUIの表示と
// 実際の処理がズレることを防ぐ）。
func (m *TablesModel) ApplyConfig(cfg config.Config) {
	for i := range m.rows {
		action, warn := washover.DecideTableAction(m.rows[i].Info.Name, m.rows[i].Info.HasPtime, cfg)
		m.rows[i].Action = action
		if warn {
			m.rows[i].Note = cfg.TimeColumnName() + "列がないため足切りできません（全件コピーします）"
		}
	}
}

// CycleAction は現在の処理を次の処理へ進める。
// 巡回順は 全件コピー → 足切り → 削除 → 全件コピー。
// ptime列を持たないテーブルは足切りできないため、[全]から直接[削]へ進む（設計案9章）。
func CycleAction(current washover.TableAction, hasPtime bool) washover.TableAction {
	switch current {
	case washover.ActionCopyAll:
		if hasPtime {
			return washover.ActionTrim
		}
		return washover.ActionDrop
	case washover.ActionTrim:
		return washover.ActionDrop
	case washover.ActionDrop:
		return washover.ActionCopyAll
	default:
		return washover.ActionCopyAll
	}
}

// visibleIndices は現在のフィルタ文字列にマッチする rows のインデックス一覧を返す。
// フィルタが空文字なら全行を返す。
func visibleIndices(rows []TableRow, query string) []int {
	if query == "" {
		out := make([]int, len(rows))
		for i := range rows {
			out[i] = i
		}
		return out
	}
	q := strings.ToLower(query)
	var out []int
	for i, r := range rows {
		if strings.Contains(strings.ToLower(r.Info.Name), q) {
			out = append(out, i)
		}
	}
	return out
}

// Update はキー入力を処理する。
func (m TablesModel) Update(msg tea.Msg) (TablesModel, tea.Cmd) {
	if m.filterMode {
		return m.updateFilterMode(msg)
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	vis := visibleIndices(m.rows, m.filterQuery)

	switch keyMsg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
	case " ":
		if len(vis) > 0 {
			idx := vis[m.cursor]
			m.rows[idx].Action = CycleAction(m.rows[idx].Action, m.rows[idx].Info.HasPtime)
		}
	case "a":
		for i := range m.rows {
			if m.rows[i].Info.HasPtime {
				m.rows[i].Action = washover.ActionTrim
			}
		}
	case "/":
		m.filterMode = true
		m.filterInput.SetValue(m.filterQuery)
		m.filterInput.CursorEnd()
		return m, m.filterInput.Focus()
	case "enter":
		return m, func() tea.Msg { return TablesSubmittedMsg{Rows: append([]TableRow(nil), m.rows...)} }
	case "esc":
		return m, backCmd()
	}

	if m.cursor >= len(vis) {
		m.cursor = len(vis) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	return m, nil
}

func (m TablesModel) updateFilterMode(msg tea.Msg) (TablesModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter", "esc":
			m.filterMode = false
			m.filterInput.Blur()
			m.filterQuery = m.filterInput.Value()
			m.cursor = 0
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
}

// actionLabel は処理を画面表示用の短いタグへ変換する。
func actionLabel(a washover.TableAction) string {
	switch a {
	case washover.ActionTrim:
		return "[切]"
	case washover.ActionDrop:
		return "[削]"
	default:
		return "[全]"
	}
}

func ptimeMark(hasPtime bool) string {
	if hasPtime {
		return "○"
	}
	return "-"
}

func formatLastPtime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Local().Format("2006-01-02")
}

// formatRowCount は行数を3桁区切りで整形する。
func formatRowCount(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
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

// summarize は各Actionの件数を数える（Confirm画面での表示にも使う）。
func summarize(rows []TableRow) (copyAll, trim, drop int) {
	for _, r := range rows {
		switch r.Action {
		case washover.ActionTrim:
			trim++
		case washover.ActionDrop:
			drop++
		default:
			copyAll++
		}
	}
	return
}

// View はテーブル一覧を設計案9章のモックアップに沿って描画する。
func (m TablesModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("テーブル選択"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(fmt.Sprintf("対象DB: %s", m.database)))
	b.WriteString("\n\n")

	header := fmt.Sprintf("  %-6s %-22s %-6s %14s %-12s %10s", "処理", "テーブル名", m.timeCol, "推定行数", "最終"+m.timeCol, "サイズ")
	b.WriteString(headerRowStyle.Render(header))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(strings.Repeat("─", lipgloss.Width(header))))
	b.WriteString("\n")

	vis := visibleIndices(m.rows, m.filterQuery)
	if len(vis) == 0 {
		b.WriteString(subtitleStyle.Render("  (該当するテーブルがありません)"))
		b.WriteString("\n")
	}
	for i, idx := range vis {
		row := m.rows[idx]
		marker := "  "
		if i == m.cursor && !m.filterMode {
			marker = cursorStyle.Render("> ")
		}
		tag := actionLabel(row.Action)
		var tagStyled string
		switch row.Action {
		case washover.ActionTrim:
			tagStyled = actionTrimStyle.Render(tag)
		case washover.ActionDrop:
			tagStyled = actionDropStyle.Render(tag)
		default:
			tagStyled = actionCopyAllStyle.Render(tag)
		}
		line := fmt.Sprintf("%-6s %-22s %-6s %14s %-12s %10s",
			tagStyled, truncateName(row.Info.Name, 22), ptimeMark(row.Info.HasPtime),
			formatRowCount(row.Info.EstRows), formatLastPtime(row.Info.LastPtime), formatBytes(row.Info.SizeBytes))
		b.WriteString(marker)
		b.WriteString(line)
		if row.Note != "" {
			b.WriteString(" ")
			b.WriteString(subtitleStyle.Render("(" + row.Note + ")"))
		}
		b.WriteString("\n")
	}

	b.WriteString(subtitleStyle.Render(strings.Repeat("─", lipgloss.Width(header))))
	b.WriteString("\n")
	copyAll, trim, drop := summarize(m.rows)
	b.WriteString(subtitleStyle.Render(fmt.Sprintf(
		"[全] 全件コピー(%d)  [切] 足切りコピー(%d)  [削] 引き継がない(%d)", copyAll, trim, drop)))
	b.WriteString("\n")

	if m.filterMode {
		b.WriteString("\n絞り込み: ")
		b.WriteString(m.filterInput.View())
	} else if m.filterQuery != "" {
		b.WriteString(subtitleStyle.Render(fmt.Sprintf("\n絞り込み中: %q (/で変更)", m.filterQuery)))
	}

	b.WriteString(helpStyle.Render(
		"\nspace:処理切替  a:全て足切り  /:絞り込み  ↑↓:移動  enter:次へ  esc:戻る  ctrl+c:終了"))
	return b.String()
}

func truncateName(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
}
