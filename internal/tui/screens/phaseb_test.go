package screens

import (
	"strings"
	"testing"

	"github.com/terayu/pgwash/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewPhaseBMode_DefaultsToAutoWhenUnset(t *testing.T) {
	m := NewPhaseBMode("")
	if m.mode != config.PhaseBAuto {
		t.Errorf("got %v, want PhaseBAuto", m.mode)
	}
}

func TestNewPhaseBMode_RespectsInitial(t *testing.T) {
	m := NewPhaseBMode(config.PhaseBManual)
	if m.mode != config.PhaseBManual {
		t.Errorf("got %v, want PhaseBManual", m.mode)
	}
}

func TestNewPhaseBMode_RespectsInitialSkip(t *testing.T) {
	m := NewPhaseBMode(config.PhaseBSkip)
	if m.mode != config.PhaseBSkip {
		t.Errorf("got %v, want PhaseBSkip", m.mode)
	}
}

func TestMovePhaseBMode_Forward(t *testing.T) {
	if got := MovePhaseBMode(config.PhaseBAuto, 1); got != config.PhaseBManual {
		t.Errorf("Auto+1->%v, want Manual", got)
	}
	if got := MovePhaseBMode(config.PhaseBManual, 1); got != config.PhaseBSkip {
		t.Errorf("Manual+1->%v, want Skip", got)
	}
	if got := MovePhaseBMode(config.PhaseBSkip, 1); got != config.PhaseBAuto {
		t.Errorf("Skip+1->%v, want Auto (末尾で巡回)", got)
	}
}

func TestMovePhaseBMode_Backward(t *testing.T) {
	if got := MovePhaseBMode(config.PhaseBManual, -1); got != config.PhaseBAuto {
		t.Errorf("Manual-1->%v, want Auto", got)
	}
	if got := MovePhaseBMode(config.PhaseBSkip, -1); got != config.PhaseBManual {
		t.Errorf("Skip-1->%v, want Manual", got)
	}
	if got := MovePhaseBMode(config.PhaseBAuto, -1); got != config.PhaseBSkip {
		t.Errorf("Auto-1->%v, want Skip (先頭で巡回)", got)
	}
}

func TestMovePhaseBMode_UnknownCurrentStartsAtAuto(t *testing.T) {
	if got := MovePhaseBMode("", 1); got != config.PhaseBManual {
		t.Errorf("unknown+1->%v, want Manual (先頭Autoを起点とする)", got)
	}
	if got := MovePhaseBMode("", -1); got != config.PhaseBSkip {
		t.Errorf("unknown-1->%v, want Skip (先頭Autoを起点とする)", got)
	}
}

// escはBackMsgを発行する。
func TestPhaseBModeModel_Esc_ProducesBackMsg(t *testing.T) {
	m := NewPhaseBMode(config.PhaseBAuto)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Errorf("expected BackMsg, got %T", cmd())
	}
}

// sキーでskipを選択できる。
func TestPhaseBModeModel_SKey_SelectsSkip(t *testing.T) {
	m := NewPhaseBMode(config.PhaseBAuto)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if m.mode != config.PhaseBSkip {
		t.Errorf("mode = %v, want PhaseBSkip", m.mode)
	}
}

func TestPhaseBModeModel_View_ContainsSkipOption(t *testing.T) {
	m := NewPhaseBMode(config.PhaseBAuto)
	if !strings.Contains(m.View(), "実行しない") {
		t.Error("View() should contain the skip option label")
	}
}
