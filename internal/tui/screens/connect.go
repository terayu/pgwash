package screens

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/db"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ConnectSubmittedMsg は接続情報の入力が確定したことを通知する。
// ルートModelはこれを受けて実際の接続・テーブル一覧取得（IO）を行う。
type ConnectSubmittedMsg struct {
	ConnInfo db.ConnInfo
	Schema   string
}

type connectField int

const (
	fieldHost connectField = iota
	fieldPort
	fieldUser
	fieldPassword
	fieldDatabase
	fieldSchema
	connectFieldCount
)

var connectFieldLabels = [connectFieldCount]string{
	fieldHost:     "ホスト",
	fieldPort:     "ポート",
	fieldUser:     "ユーザー",
	fieldPassword: "パスワード",
	fieldDatabase: "DB名",
	fieldSchema:   "スキーマ",
}

// ConnectModel は接続情報入力画面。
// 起動引数/環境変数から渡された cfg の値を初期値として表示する（設計案 5.3節）。
type ConnectModel struct {
	inputs [connectFieldCount]textinput.Model
	focus  connectField
	err    string
}

// NewConnect は cfg の値を初期値として接続情報入力画面を構築する。
func NewConnect(cfg config.Config) ConnectModel {
	m := ConnectModel{}

	m.inputs[fieldHost] = newTextInput("localhost")
	m.inputs[fieldHost].SetValue(cfg.Host)

	m.inputs[fieldPort] = newTextInput("5432")
	if cfg.Port != 0 {
		m.inputs[fieldPort].SetValue(strconv.Itoa(cfg.Port))
	}

	m.inputs[fieldUser] = newTextInput("postgres")
	m.inputs[fieldUser].SetValue(cfg.User)

	m.inputs[fieldPassword] = newTextInput("")
	m.inputs[fieldPassword].SetValue(cfg.Password)
	m.inputs[fieldPassword].EchoMode = textinput.EchoPassword

	m.inputs[fieldDatabase] = newTextInput("")
	m.inputs[fieldDatabase].SetValue(cfg.Database)

	schema := cfg.Schema
	if schema == "" {
		schema = config.DefaultSchema
	}
	m.inputs[fieldSchema] = newTextInput(config.DefaultSchema)
	m.inputs[fieldSchema].SetValue(schema)

	m.focus = fieldHost
	m.inputs[fieldHost].Focus()
	return m
}

func newTextInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 128
	ti.Width = 40
	return ti
}

// Init はテキストカーソルの点滅を開始する。
func (m ConnectModel) Init() tea.Cmd {
	return textinput.Blink
}

// SetError は接続失敗など、画面外(root)で発生したエラーを表示する。
func (m *ConnectModel) SetError(err error) {
	if err == nil {
		m.err = ""
		return
	}
	m.err = err.Error()
}

// parseConnectFields は各入力欄の文字列を検証し、ConnectSubmittedMsg を組み立てる。
// 純粋関数として切り出すことでテキスト入力の検証をUI/IOなしにテストできるようにする。
func parseConnectFields(host, portStr, user, password, database, schema string) (ConnectSubmittedMsg, error) {
	host = strings.TrimSpace(host)
	user = strings.TrimSpace(user)
	database = strings.TrimSpace(database)
	schema = strings.TrimSpace(schema)
	if schema == "" {
		schema = config.DefaultSchema
	}

	var errs []string
	if host == "" {
		errs = append(errs, "ホストを入力してください")
	}
	if user == "" {
		errs = append(errs, "ユーザーを入力してください")
	}
	if database == "" {
		errs = append(errs, "DB名を入力してください")
	}

	port := 5432
	portStr = strings.TrimSpace(portStr)
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil || p <= 0 || p > 65535 {
			errs = append(errs, "ポートは1〜65535の数値で入力してください")
		} else {
			port = p
		}
	}

	if len(errs) > 0 {
		return ConnectSubmittedMsg{}, fmt.Errorf("%s", strings.Join(errs, " / "))
	}

	return ConnectSubmittedMsg{
		ConnInfo: db.ConnInfo{
			Host:     host,
			Port:     port,
			User:     user,
			Password: password,
			Database: database,
		},
		Schema: schema,
	}, nil
}

// shouldFillPlaceholder は tab キーで placeholder（暗い色で表示されるサンプル値）を
// 実際の入力として確定してよいかを判定する。入力欄が空で、かつサンプルが
// 設定されている場合のみ true。純粋関数として切り出し、UIなしでテストできるようにする。
func shouldFillPlaceholder(value, placeholder string) bool {
	return value == "" && placeholder != ""
}

// tryFillPlaceholder はフォーカス中の入力欄が空なら、表示中のサンプル値を実際の入力として
// 確定する。確定した場合は true を返し、フォーカスは移動しない
// （もう一度 tab を押せば通常どおり次の項目へ移動する）。
func (m ConnectModel) tryFillPlaceholder() (ConnectModel, bool) {
	ti := m.inputs[m.focus]
	if !shouldFillPlaceholder(ti.Value(), ti.Placeholder) {
		return m, false
	}
	ti.SetValue(ti.Placeholder)
	ti.CursorEnd()
	m.inputs[m.focus] = ti
	m.err = ""
	return m, true
}

// Update はフィールド間の移動・送信・各入力欄への文字入力を処理する。
func (m ConnectModel) Update(msg tea.Msg) (ConnectModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "tab":
			if filled, ok := m.tryFillPlaceholder(); ok {
				return filled, nil
			}
			return m.moveFocus(1)
		case "down":
			return m.moveFocus(1)
		case "shift+tab", "up":
			return m.moveFocus(-1)
		case "enter":
			if m.focus != connectFieldCount-1 {
				return m.moveFocus(1)
			}
			return m.trySubmit()
		}
	}

	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

func (m ConnectModel) moveFocus(delta int) (ConnectModel, tea.Cmd) {
	m.err = ""
	m.inputs[m.focus].Blur()
	next := int(m.focus) + delta
	if next < 0 {
		next = int(connectFieldCount) - 1
	}
	if next >= int(connectFieldCount) {
		next = 0
	}
	m.focus = connectField(next)
	cmd := m.inputs[m.focus].Focus()
	return m, cmd
}

func (m ConnectModel) trySubmit() (ConnectModel, tea.Cmd) {
	msg, err := parseConnectFields(
		m.inputs[fieldHost].Value(),
		m.inputs[fieldPort].Value(),
		m.inputs[fieldUser].Value(),
		m.inputs[fieldPassword].Value(),
		m.inputs[fieldDatabase].Value(),
		m.inputs[fieldSchema].Value(),
	)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.err = ""
	return m, func() tea.Msg { return msg }
}

// View は接続情報入力画面を描画する。
func (m ConnectModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("接続情報"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("起動引数/環境変数の値が初期値として入っています。必要なら書き換えてください。"))
	b.WriteString("\n\n")

	for i := connectField(0); i < connectFieldCount; i++ {
		label := labelStyle.Render(connectFieldLabels[i])
		b.WriteString(label)
		b.WriteString(" ")
		b.WriteString(m.inputs[i].View())
		b.WriteString("\n")
	}

	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("エラー: " + m.err))
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("tab:サンプル入力/移動  ↑↓:移動  enter:次の項目/接続  ctrl+c:終了"))
	return b.String()
}
