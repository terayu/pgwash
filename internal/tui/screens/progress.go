package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/terayu/pgwash/internal/washover"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// maxLogLines は進捗画面に表示する直近ログ行の最大数。
const maxLogLines = 8

// ProgressModel はフェーズA/フェーズB共通の進捗表示画面。
//
// 進捗はテーブル単位でしか取れない（dblinkにより1テーブル=1SQL文で実行されるため、
// テーブル内部の進捗は存在しない。設計案9章）。そのため:
//   - プログレスバーは「n / m テーブル完了」のテーブル単位
//   - 実行中テーブルはspinnerと経過時間で「止まっていないこと」を示す
type ProgressModel struct {
	phaseLabel string // 画面タイトルに出す "フェーズA" / "フェーズB"

	spinner spinner.Model

	total int
	done  int

	currentTable   string
	currentAction  washover.TableAction
	currentStarted time.Time

	logs []string

	err      error
	finished bool
	doneMsg  string
}

// NewProgress は phaseLabel（例: "フェーズA"）を冠した進捗画面を構築する。
func NewProgress(phaseLabel string) ProgressModel {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	return ProgressModel{phaseLabel: phaseLabel, spinner: sp}
}

func (m ProgressModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update はspinnerのTickや、ApplyEventで生成された副作用メッセージを処理する。
func (m ProgressModel) Update(msg tea.Msg) (ProgressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ApplyEvent は washover.Event を進捗画面の状態へ反映する。
// ルートModelが engine のイベントチャネルから受信するたびに呼び出す。
func (m *ProgressModel) ApplyEvent(ev washover.Event) {
	switch ev.Kind {
	case washover.EventPhaseStart:
		m.total = ev.Total
		m.done = 0
		m.appendLog(fmt.Sprintf("開始: %s", ev.Message))
	case washover.EventStep:
		m.appendLog(ev.Step)
	case washover.EventTableStart:
		m.total = ev.Total
		m.currentTable = ev.Table
		m.currentAction = ev.Action
		m.currentStarted = time.Now()
	case washover.EventTableDone:
		m.done = ev.Index
		m.total = ev.Total
		m.currentTable = ""
		m.appendLog(fmt.Sprintf("%d/%d 完了: %s (%s行)", ev.Index, ev.Total, ev.Table, formatRowCount(ev.Rows)))
	case washover.EventLog:
		m.appendLog(ev.Message)
	case washover.EventError:
		m.err = ev.Err
		m.appendLog(fmt.Sprintf("エラー: %s", ev.Message))
	case washover.EventDone:
		m.finished = true
		m.doneMsg = ev.Message
	}
}

func (m *ProgressModel) appendLog(line string) {
	if line == "" {
		return
	}
	m.logs = append(m.logs, line)
	if len(m.logs) > maxLogLines {
		m.logs = m.logs[len(m.logs)-maxLogLines:]
	}
}

// progressBar は幅widthのテキストプログレスバーを描画する（純粋関数、テスト容易）。
// 進捗はテーブル単位でのみ計測できるため、内部処理の滑らかなアニメーションは行わない。
func progressBar(done, total, width int) string {
	if width < 4 {
		width = 4
	}
	if total <= 0 {
		return "[" + strings.Repeat("-", width-2) + "]"
	}
	ratio := float64(done) / float64(total)
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width-2))
	if filled > width-2 {
		filled = width - 2
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-2-filled)
	return "[" + bar + "]"
}

// formatElapsed は経過時間を "1分23秒" のような日本語表記に整形する。
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	h, rem := total/3600, total%3600
	mnt, sec := rem/60, rem%60
	if h > 0 {
		return fmt.Sprintf("%d時間%d分%d秒", h, mnt, sec)
	}
	if mnt > 0 {
		return fmt.Sprintf("%d分%d秒", mnt, sec)
	}
	return fmt.Sprintf("%d秒", sec)
}

// Finished は EventDone を受信済みかを返す。
func (m ProgressModel) Finished() bool { return m.finished }

// Err は EventError で通知された最後のエラーを返す（なければ nil）。
func (m ProgressModel) Err() error { return m.err }

// View は進捗画面を描画する。
func (m ProgressModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.phaseLabel + " 実行中"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("%s %d / %d テーブル完了\n", progressBar(m.done, m.total, 30), m.done, m.total))
	b.WriteString("\n")

	if m.currentTable != "" {
		elapsed := formatElapsed(time.Since(m.currentStarted))
		b.WriteString(fmt.Sprintf("%s 処理中: %s %s (経過 %s)\n",
			m.spinner.View(), actionLabel(m.currentAction), m.currentTable, elapsed))
	} else if !m.finished {
		b.WriteString(fmt.Sprintf("%s 準備中...\n", m.spinner.View()))
	}
	b.WriteString(subtitleStyle.Render("大きなテーブルでは数分かかることがあります。バーが止まって見えても処理は継続しています。"))
	b.WriteString("\n\n")

	if len(m.logs) > 0 {
		b.WriteString(subtitleStyle.Render("ログ:"))
		b.WriteString("\n")
		for _, l := range m.logs {
			b.WriteString("  " + l + "\n")
		}
	}

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("エラー: " + m.err.Error()))
		b.WriteString("\n")
	}

	return b.String()
}
