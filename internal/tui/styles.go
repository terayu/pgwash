// Package tui は bubbletea によるウィザード形式のTUIを提供する。
// 画面遷移は設計案 9章のとおり: Connect → Tables → (Cutoff) → PhaseBMode → Confirm
// → ProgressA → (PhaseBGate) → ProgressB → Result。
package tui

import "github.com/charmbracelet/lipgloss"

// appStyle はどの画面でも共通の外枠（余白）。
// 各画面自体の配色（タイトル・エラー表示・行の強調など）は
// internal/tui/screens が持つ（screens は tui をimportできないため、
// 画面内スタイルは screens/styles.go 側に定義がある）。
var appStyle = lipgloss.NewStyle().Padding(1, 2)

// fatalErrorStyle は接続エラーなど致命的な状態を示す（画面遷移できない場合の表示に使う）。
var fatalErrorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
