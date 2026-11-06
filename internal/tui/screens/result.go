package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/washover"
)

// ResultModel は完了（成功/失敗）サマリ画面。
type ResultModel struct {
	state    *washover.RunState
	err      error
	stateDir string
}

// NewResult は実行結果を表示する画面を構築する。
// err が nil でなく state が nil の場合（フェーズA開始前の失敗など）にも対応する。
func NewResult(state *washover.RunState, err error, stateDir string) ResultModel {
	return ResultModel{state: state, err: err, stateDir: stateDir}
}

// tableErrors は Error が設定されているテーブル結果だけを返す。
func tableErrors(tables []washover.TableResult) []washover.TableResult {
	var out []washover.TableResult
	for _, t := range tables {
		if t.Error != "" {
			out = append(out, t)
		}
	}
	return out
}

// elapsedDuration は開始〜完了（未完了ならゼロ値）の所要時間を返す。
func elapsedDuration(state *washover.RunState) time.Duration {
	if state == nil || state.CompletedAt == nil {
		return 0
	}
	return state.CompletedAt.Sub(state.StartedAt)
}

// View は完了サマリを描画する。
func (m ResultModel) View() string {
	var b strings.Builder

	if m.err != nil {
		b.WriteString(errorStyle.Render("処理は失敗しました"))
		b.WriteString("\n\n")
		b.WriteString(errorStyle.Render("エラー: " + m.err.Error()))
		b.WriteString("\n\n")
	} else if m.state != nil && m.state.Config.PhaseBMode == config.PhaseBSkip {
		b.WriteString(successStyle.Render("フェーズA（バックアップ）が完了しました"))
		b.WriteString("\n\n")
	} else {
		b.WriteString(successStyle.Render("洗い替えが完了しました"))
		b.WriteString("\n\n")
	}

	if m.state != nil {
		st := m.state
		skipMode := m.err == nil && st.Config.PhaseBMode == config.PhaseBSkip

		b.WriteString(fmt.Sprintf("実行ID:   %s\n", st.RunID))
		b.WriteString(fmt.Sprintf("フェーズ: %s\n", st.Phase))
		b.WriteString(fmt.Sprintf("対象DB:   %s\n", st.Config.Database))
		if skipMode {
			b.WriteString(fmt.Sprintf("バックアップDB: %s\n", st.TempDBName))
		}

		var rowsA, rowsB int64
		for _, t := range st.Tables {
			rowsA += t.RowsPhaseA
			rowsB += t.RowsPhaseB
		}
		if skipMode {
			b.WriteString(fmt.Sprintf("テーブル: %d 件（コピー %s 行）\n", len(st.Tables), formatRowCount(rowsA)))
		} else {
			b.WriteString(fmt.Sprintf("テーブル: %d 件（フェーズAコピー %s 行 / フェーズB差分 %s 行）\n",
				len(st.Tables), formatRowCount(rowsA), formatRowCount(rowsB)))
		}

		if d := elapsedDuration(st); d > 0 {
			b.WriteString(fmt.Sprintf("所要時間: %s\n", formatElapsed(d)))
		}

		if errs := tableErrors(st.Tables); len(errs) > 0 {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render(fmt.Sprintf("エラーのあるテーブル (%d件):", len(errs))))
			b.WriteString("\n")
			for _, t := range errs {
				b.WriteString(fmt.Sprintf("  - %s: %s\n", t.TableName, t.Error))
			}
		}

		if m.stateDir != "" {
			b.WriteString("\n")
			b.WriteString(subtitleStyle.Render(fmt.Sprintf("状態ファイル: %s/%s.json", m.stateDir, st.RunID)))
			b.WriteString("\n")
		}
		if skipMode {
			b.WriteString(subtitleStyle.Render(fmt.Sprintf(
				"source は変更していません。入れ替える場合は 'pgwash phase-b --run-id=%s' を実行してください。", st.RunID)))
			b.WriteString("\n")
		}
	}

	b.WriteString(helpStyle.Render("\n任意のキーで終了"))
	return b.String()
}
