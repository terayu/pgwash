package cli

import (
	"fmt"
	"log/slog"

	"github.com/terayu/pgwash/internal/washover"
)

// consumeEvents は washover.Event を受信し続け、slog でログ出力する。
// 進捗はテーブル単位でしか取れないため（dblinkにより1テーブル=1SQL文）、
// EventTableDone を「n/m テーブル完了」の形で出力する。
// events がクローズされると返る。
func consumeEvents(events <-chan washover.Event) {
	for ev := range events {
		logEvent(ev)
	}
}

func logEvent(ev washover.Event) {
	switch ev.Kind {
	case washover.EventPhaseStart:
		slog.Info("フェーズ開始", "phase", ev.Phase)
	case washover.EventStep:
		slog.Info(ev.Step, "phase", ev.Phase)
	case washover.EventTableStart:
		slog.Info(fmt.Sprintf("テーブル処理開始 (%d/%d)", ev.Index, ev.Total),
			"table", ev.Table, "action", ev.Action)
	case washover.EventTableDone:
		slog.Info(fmt.Sprintf("%d/%d テーブル完了", ev.Index, ev.Total),
			"table", ev.Table, "action", ev.Action, "rows", ev.Rows)
	case washover.EventLog:
		slog.Info(ev.Message, "phase", ev.Phase)
	case washover.EventError:
		slog.Error(ev.Message, "table", ev.Table, "err", ev.Err)
	case washover.EventDone:
		slog.Info("完了", "phase", ev.Phase, "message", ev.Message)
	default:
		slog.Info(ev.Message, "kind", ev.Kind, "phase", ev.Phase)
	}
}
