package screens

import (
	"testing"

	"github.com/terayu/pgwash/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func TestParseConnectFields_Valid(t *testing.T) {
	msg, err := parseConnectFields("localhost", "5432", "postgres", "secret", "joywatcher", "public")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ConnInfo.Host != "localhost" || msg.ConnInfo.Port != 5432 || msg.ConnInfo.User != "postgres" ||
		msg.ConnInfo.Password != "secret" || msg.ConnInfo.Database != "joywatcher" || msg.Schema != "public" {
		t.Errorf("unexpected result: %+v", msg)
	}
}

func TestParseConnectFields_DefaultPort(t *testing.T) {
	msg, err := parseConnectFields("localhost", "", "postgres", "", "joywatcher", "public")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ConnInfo.Port != 5432 {
		t.Errorf("default port = %d, want 5432", msg.ConnInfo.Port)
	}
}

func TestParseConnectFields_DefaultSchema(t *testing.T) {
	msg, err := parseConnectFields("localhost", "5432", "postgres", "", "joywatcher", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Schema != "public" {
		t.Errorf("default schema = %q, want public", msg.Schema)
	}
}

func TestParseConnectFields_MissingRequired(t *testing.T) {
	cases := []struct {
		name                             string
		host, port, user, pw, db, schema string
	}{
		{"host欠落", "", "5432", "postgres", "", "joywatcher", "public"},
		{"user欠落", "localhost", "5432", "", "", "joywatcher", "public"},
		{"db欠落", "localhost", "5432", "postgres", "", "", "public"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseConnectFields(c.host, c.port, c.user, c.pw, c.db, c.schema)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestParseConnectFields_InvalidPort(t *testing.T) {
	cases := []string{"abc", "0", "-1", "70000"}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			_, err := parseConnectFields("localhost", p, "postgres", "", "joywatcher", "public")
			if err == nil {
				t.Fatalf("port %q: expected error, got nil", p)
			}
		})
	}
}

func TestShouldFillPlaceholder(t *testing.T) {
	cases := []struct {
		name        string
		value       string
		placeholder string
		want        bool
	}{
		{"空でサンプルあり", "", "localhost", true},
		{"値ありでサンプルあり", "db.example.com", "localhost", false},
		{"空でサンプルなし", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldFillPlaceholder(c.value, c.placeholder); got != c.want {
				t.Errorf("shouldFillPlaceholder(%q, %q) = %v, want %v", c.value, c.placeholder, got, c.want)
			}
		})
	}
}

// tab は空欄ならサンプル値を確定し、フォーカスは移動しない。
func TestConnectModel_Tab_FillsPlaceholder(t *testing.T) {
	m := NewConnect(config.Config{})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.inputs[fieldHost].Value() != "localhost" {
		t.Errorf("host = %q, want localhost", m.inputs[fieldHost].Value())
	}
	if m.focus != fieldHost {
		t.Errorf("focus = %v, want fieldHost", m.focus)
	}

	// もう一度 tab を押すと、今度は空でないのでフォーカスが移動する。
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != fieldPort {
		t.Errorf("focus = %v, want fieldPort", m.focus)
	}
	if m.inputs[fieldHost].Value() != "localhost" {
		t.Errorf("host = %q, want localhost (unchanged)", m.inputs[fieldHost].Value())
	}
}

// 値が入っている欄では tab は通常どおりフォーカスを移動するだけ。
func TestConnectModel_Tab_NonEmptyMovesFocus(t *testing.T) {
	m := NewConnect(config.Config{Host: "db.example.com"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != fieldPort {
		t.Errorf("focus = %v, want fieldPort", m.focus)
	}
	if m.inputs[fieldHost].Value() != "db.example.com" {
		t.Errorf("host = %q, want db.example.com (unchanged)", m.inputs[fieldHost].Value())
	}
}

// サンプルが設定されていない欄（パスワード）では tab は通常どおりフォーカスを移動するだけ。
func TestConnectModel_Tab_NoPlaceholderMovesFocus(t *testing.T) {
	m := NewConnect(config.Config{})
	// fieldPassword までフォーカスを移動する。
	for m.focus != fieldPassword {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != fieldDatabase {
		t.Errorf("focus = %v, want fieldDatabase", m.focus)
	}
	if m.inputs[fieldPassword].Value() != "" {
		t.Errorf("password = %q, want empty", m.inputs[fieldPassword].Value())
	}
}
