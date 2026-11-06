package screens

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestParseCutoff_RFC3339(t *testing.T) {
	got, err := ParseCutoff("2026-08-01T00:00:00+09:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 8, 1, 0, 0, 0, 0, time.FixedZone("", 9*3600))
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseCutoff_DateTime(t *testing.T) {
	got, err := ParseCutoff("2026-08-01 12:30:00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Year() != 2026 || got.Month() != 8 || got.Day() != 1 || got.Hour() != 12 || got.Minute() != 30 {
		t.Errorf("unexpected parsed time: %v", got)
	}
}

func TestParseCutoff_DateOnly(t *testing.T) {
	got, err := ParseCutoff("2026-08-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Hour() != 0 || got.Minute() != 0 {
		t.Errorf("date-only input should default to midnight, got %v", got)
	}
}

func TestParseCutoff_Empty(t *testing.T) {
	if _, err := ParseCutoff(""); err == nil {
		t.Fatal("expected error for empty input")
	}
	if _, err := ParseCutoff("   "); err == nil {
		t.Fatal("expected error for whitespace-only input")
	}
}

func TestParseCutoff_Invalid(t *testing.T) {
	if _, err := ParseCutoff("not a date"); err == nil {
		t.Fatal("expected error for invalid input")
	}
}

// escはBackMsgを発行する。
func TestCutoffModel_Esc_ProducesBackMsg(t *testing.T) {
	m := NewCutoff(time.Time{}, nil, nil, "ptime")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd")
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Errorf("expected BackMsg, got %T", cmd())
	}
}

// 空欄で tab を押すとサンプル値が確定する。
func TestCutoffModel_Tab_FillsPlaceholder(t *testing.T) {
	m := NewCutoff(time.Time{}, nil, nil, "ptime")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	want := "2026-08-01 00:00:00"
	if m.input.Value() != want {
		t.Errorf("value = %q, want %q", m.input.Value(), want)
	}

	// もう一度 tab を押しても値は変わらない。
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.input.Value() != want {
		t.Errorf("value after second tab = %q, want %q (unchanged)", m.input.Value(), want)
	}
}

// 初期値がある場合は tab を押しても値は変わらない。
func TestCutoffModel_Tab_NonEmptyUnchanged(t *testing.T) {
	initial := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	m := NewCutoff(initial, nil, nil, "ptime")
	before := m.input.Value()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.input.Value() != before {
		t.Errorf("value = %q, want %q (unchanged)", m.input.Value(), before)
	}
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func submitCutoff(t *testing.T, m CutoffModel) CutoffSubmittedMsg {
	t.Helper()
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected submit Cmd, got nil (err=%q)", m.err)
	}
	msg, ok := cmd().(CutoffSubmittedMsg)
	if !ok {
		t.Fatalf("expected CutoffSubmittedMsg, got %T", cmd())
	}
	return msg
}

// 足切り対象に含まれないテーブルの個別日時は初期値として取り込まない。
func TestNewCutoff_IgnoresOverridesOutsideTables(t *testing.T) {
	ov := map[string]time.Time{"a": time.Now(), "gone": time.Now()}
	m := NewCutoff(time.Time{}, []string{"a", "b"}, ov, "ptime")
	if len(m.overrides) != 1 || !hasKey(m.overrides, "a") {
		t.Errorf("overrides = %v, want only a", m.overrides)
	}
}

// テーブル行で e → 日時入力 → enter で個別日時が設定され、確定時に TableCutoffs として通知される。
func TestCutoffModel_EditOverride(t *testing.T) {
	common := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	m := NewCutoff(common, []string{"a", "b"}, nil, "ptime")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // a
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // b
	m, _ = m.Update(keyRunes("e"))
	if !m.editing {
		t.Fatal("expected editing mode after e")
	}
	m, _ = m.Update(keyRunes("2026-05-01"))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.editing {
		t.Fatal("expected editing to end after enter")
	}

	msg := submitCutoff(t, m)
	if !msg.Cutoff.Equal(common) {
		t.Errorf("Cutoff = %v, want %v", msg.Cutoff, common)
	}
	want := time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)
	if len(msg.TableCutoffs) != 1 || !msg.TableCutoffs["b"].Equal(want) {
		t.Errorf("TableCutoffs = %v, want {b: %v}", msg.TableCutoffs, want)
	}
}

// 編集中に不正な日時で enter を押すとエラーを表示し、編集を続ける。
func TestCutoffModel_EditInvalidKeepsEditing(t *testing.T) {
	m := NewCutoff(time.Time{}, []string{"a"}, nil, "ptime")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(keyRunes("e"))
	m, _ = m.Update(keyRunes("bogus"))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.editing || m.err == "" {
		t.Errorf("editing=%v err=%q, want editing with error", m.editing, m.err)
	}
	if hasKey(m.overrides, "a") {
		t.Error("invalid input must not set an override")
	}
}

// 編集中の esc は編集の取消であり、画面を戻らない。
func TestCutoffModel_EditEscCancels(t *testing.T) {
	m := NewCutoff(time.Time{}, []string{"a"}, nil, "ptime")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(keyRunes("e"))
	m, _ = m.Update(keyRunes("2026-05-01"))
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.editing || cmd != nil {
		t.Errorf("editing=%v cmd=%v, want edit cancelled without BackMsg", m.editing, cmd)
	}
	if hasKey(m.overrides, "a") {
		t.Error("cancelled edit must not set an override")
	}
}

// d、または空欄での確定で個別日時を解除し共通日時に戻す。
func TestCutoffModel_RevertToCommon(t *testing.T) {
	ov := map[string]time.Time{"a": time.Now(), "b": time.Now()}
	m := NewCutoff(time.Now(), []string{"a", "b"}, ov, "ptime")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // a
	m, _ = m.Update(keyRunes("d"))
	if hasKey(m.overrides, "a") {
		t.Error("d should remove override of a")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // b
	m, _ = m.Update(keyRunes("e"))
	for range len(m.edit.Value()) {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if hasKey(m.overrides, "b") {
		t.Error("empty input should remove override of b")
	}

	if msg := submitCutoff(t, m); msg.TableCutoffs != nil {
		t.Errorf("TableCutoffs = %v, want nil", msg.TableCutoffs)
	}
}

// テーブル行にカーソルがあるときの文字キーは共通日時の入力欄へ入らない。
func TestCutoffModel_TableRowDoesNotTypeIntoCommon(t *testing.T) {
	m := NewCutoff(time.Time{}, []string{"a"}, nil, "ptime")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(keyRunes("x"))
	if m.input.Value() != "" {
		t.Errorf("common input = %q, want empty", m.input.Value())
	}
}

// 個別指定のないテーブルがあれば共通日時は必須。
func TestCutoffModel_CommonRequiredWhenSomeTableUsesIt(t *testing.T) {
	m := NewCutoff(time.Time{}, []string{"a", "b"}, map[string]time.Time{"a": time.Now()}, "ptime")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.err == "" {
		t.Errorf("cmd=%v err=%q, want error without submit", cmd, m.err)
	}
}

// 全テーブルに個別指定があれば共通日時は空欄でよい（Cutoff はゼロ値）。
func TestCutoffModel_CommonOptionalWhenAllOverridden(t *testing.T) {
	ov := map[string]time.Time{"a": time.Now(), "b": time.Now()}
	m := NewCutoff(time.Time{}, []string{"a", "b"}, ov, "ptime")
	msg := submitCutoff(t, m)
	if !msg.Cutoff.IsZero() {
		t.Errorf("Cutoff = %v, want zero", msg.Cutoff)
	}
	if len(msg.TableCutoffs) != 2 {
		t.Errorf("TableCutoffs = %v, want 2 entries", msg.TableCutoffs)
	}
}
