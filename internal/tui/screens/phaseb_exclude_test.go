package screens

import (
	"testing"

	"github.com/terayu/pgwash/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func TestPhaseBExclude_InitialValue(t *testing.T) {
	ws, _ := config.ParseTimeWindows("23:00-01:00,12:00-13:00")
	m := NewPhaseBExclude(ws)
	if m.input.Value() != "23:00-01:00,12:00-13:00" {
		t.Errorf("value = %q", m.input.Value())
	}
}

func TestPhaseBExclude_SubmitParsesWindows(t *testing.T) {
	m := NewPhaseBExclude(nil)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // サンプル値 23:00-01:00
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected submit Cmd")
	}
	msg, ok := cmd().(PhaseBExcludeSubmittedMsg)
	if !ok || config.FormatTimeWindows(msg.Windows) != "23:00-01:00" {
		t.Errorf("msg = %#v", cmd())
	}
}

// 空欄で確定すると除外時間なし。
func TestPhaseBExclude_EmptyMeansNone(t *testing.T) {
	m := NewPhaseBExclude(nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected submit Cmd")
	}
	if msg := cmd().(PhaseBExcludeSubmittedMsg); msg.Windows != nil {
		t.Errorf("Windows = %v, want nil", msg.Windows)
	}
}

func TestPhaseBExclude_InvalidShowsError(t *testing.T) {
	m := NewPhaseBExclude(nil)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("23:00")})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.err == "" {
		t.Errorf("cmd = %v err = %q, want error without submit", cmd, m.err)
	}
}

func TestPhaseBExclude_Esc_ProducesBackMsg(t *testing.T) {
	m := NewPhaseBExclude(nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Errorf("expected BackMsg, got %T", cmd())
	}
}
