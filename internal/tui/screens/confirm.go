package screens

import (
	"fmt"
	"strings"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/washover"
	tea "github.com/charmbracelet/bubbletea"
)

// ConfirmSubmittedMsg は実行開始の確定を通知する。
type ConfirmSubmittedMsg struct{}

// ConfirmModel は破壊的操作（DBリネーム・接続遮断・テーブル削除）の実行前確認画面（設計案10章）。
//
// [削]（Drop）を1つ以上指定した場合は、削除対象テーブル名・推定行数を再掲したうえで
// 個別の確認（y キーでの明示acknowledge）を enter 受理の条件とする。
type ConfirmModel struct {
	cfg  config.Config
	rows []TableRow

	dropAck bool
	err     string
}

// NewConfirm は cfg と Tables画面で確定した行を元に確認画面を構築する。
func NewConfirm(cfg config.Config, rows []TableRow) ConfirmModel {
	return ConfirmModel{cfg: cfg, rows: rows}
}

// dropRows は削除(Drop)対象の行だけを返す。
func dropRows(rows []TableRow) []TableRow {
	var out []TableRow
	for _, r := range rows {
		if r.Action == washover.ActionDrop {
			out = append(out, r)
		}
	}
	return out
}

// canSubmitConfirm は enter を受理してよいかを判定する。
// 削除対象があるのに未acknowledgeなら受理しない（設計案10章「テーブル削除の確認強化」）。
func canSubmitConfirm(rows []TableRow, dropAck bool) bool {
	return len(dropRows(rows)) == 0 || dropAck
}

// Update はキー入力を処理する。
func (m ConfirmModel) Update(msg tea.Msg) (ConfirmModel, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "y":
		if len(dropRows(m.rows)) > 0 {
			m.dropAck = true
			m.err = ""
		}
	case "n":
		m.dropAck = false
	case "enter":
		if !canSubmitConfirm(m.rows, m.dropAck) {
			m.err = "削除対象テーブルがあります。y キーで削除内容を確認してください"
			return m, nil
		}
		m.err = ""
		return m, func() tea.Msg { return ConfirmSubmittedMsg{} }
	case "esc":
		return m, backCmd()
	}
	return m, nil
}

// View は設定内容の最終確認を描画する。
func (m ConfirmModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("実行内容の確認"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("接続先:     %s@%s:%d\n", m.cfg.User, m.cfg.Host, m.cfg.Port))
	b.WriteString(fmt.Sprintf("対象DB:     %s (schema=%s)\n", m.cfg.Database, m.cfg.Schema))

	copyAll, trim, drop := summarize(m.rows)
	b.WriteString(fmt.Sprintf("テーブル:   全件コピー %d 件 / 足切り %d 件 / 削除 %d 件\n", copyAll, trim, drop))
	if trim > 0 {
		b.WriteString("足切り日時:\n")
		for _, r := range m.rows {
			if r.Action != washover.ActionTrim {
				continue
			}
			kind := subtitleStyle.Render("(共通)")
			if _, ok := m.cfg.TableCutoffs[r.Info.Name]; ok {
				kind = actionTrimStyle.Render("(個別)")
			}
			b.WriteString(fmt.Sprintf("  - %-24s %s %s\n", r.Info.Name, formatCutoff(m.cfg.CutoffFor(r.Info.Name)), kind))
		}
	}
	if m.cfg.PhaseBMode == config.PhaseBSkip {
		b.WriteString("フェーズB:  実行しない (skip)\n")
	} else {
		b.WriteString(fmt.Sprintf("フェーズB:  %s / block-mode=%s\n", phaseBModeLabel(m.cfg.PhaseBMode), m.cfg.BlockMode))
		if m.cfg.PhaseBMode == config.PhaseBAuto && len(m.cfg.PhaseBExclude) > 0 {
			b.WriteString(fmt.Sprintf("除外時間:   %s（フェーズA完了時刻が該当すれば抜けるまで待機）\n", config.FormatTimeWindows(m.cfg.PhaseBExclude)))
		}
	}
	b.WriteString("\n")

	if m.cfg.PhaseBMode == config.PhaseBSkip {
		b.WriteString(successStyle.Render("フェーズAのみ実行します。source は変更されず、ダウンタイムも発生しません。"))
		b.WriteString("\n")
		b.WriteString(subtitleStyle.Render(fmt.Sprintf("コピー先の一時DB (%s_new_<実行ID>) がバックアップとして残ります。", m.cfg.Database)))
		b.WriteString("\n")
	} else {
		b.WriteString(warnStyle.Render("フェーズB実行中は source への新規接続が遮断され、既存接続も切断されます。"))
		b.WriteString("\n")
		b.WriteString(warnStyle.Render("source は退避DBへリネームされ、新DBと入れ替わります（ダウンタイム発生）。"))
		b.WriteString("\n")
	}

	dr := dropRows(m.rows)
	if len(dr) > 0 {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(fmt.Sprintf("以下の %d テーブルは一時DB上で削除され、新DBに引き継がれません:", len(dr))))
		b.WriteString("\n")
		for _, r := range dr {
			b.WriteString(fmt.Sprintf("  - %-24s 推定 %s 行\n", r.Info.Name, formatRowCount(r.Info.EstRows)))
		}
		if m.cfg.PhaseBMode == config.PhaseBSkip {
			b.WriteString(subtitleStyle.Render("(削除は一時DB上で行うため source には影響しません。元データは source にそのまま残ります)"))
		} else {
			b.WriteString(subtitleStyle.Render("(削除は一時DB上=入れ替え前に行うため本番へは影響しません。元データは退避DBに残ります)"))
		}
		b.WriteString("\n")
		ackLine := warnStyle.Render("確認: [n] 未確認")
		if m.dropAck {
			ackLine = successStyle.Render("確認: [y] 削除内容を確認しました")
		}
		b.WriteString(ackLine)
		b.WriteString("\n")
	}

	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("エラー: " + m.err))
		b.WriteString("\n")
	}

	help := "enter:実行開始  esc:戻る  ctrl+c:終了"
	if len(dr) > 0 {
		help = "y:削除内容を確認  enter:実行開始  esc:戻る  ctrl+c:終了"
	}
	b.WriteString(helpStyle.Render(help))
	return b.String()
}

func phaseBModeLabel(mode config.PhaseBMode) string {
	switch mode {
	case config.PhaseBManual:
		return "手動 (manual)"
	case config.PhaseBSkip:
		return "実行しない (skip)"
	default:
		return "自動 (auto)"
	}
}
