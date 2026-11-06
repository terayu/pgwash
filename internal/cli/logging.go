package cli

import (
	"log/slog"
	"os"
)

// setupLogger は --log-format に従って slog のデフォルトロガーを設定する。
// ログは標準出力と分離するため stderr へ出す（stdout はコマンドの結果表示用）。
func setupLogger(format string) {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}
