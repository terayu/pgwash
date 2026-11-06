package cli

// ヘッドレス実行時の終了コード（設計案 11章）。
const (
	// ExitOK は正常完了。
	ExitOK = 0
	// ExitConfigError は設定・検証エラー（接続情報不足、テーブル指定不正、確認拒否など）。
	ExitConfigError = 1
	// ExitPhaseAWaitingManual はフェーズA完了・フェーズB手動待ち（異常ではない）。
	ExitPhaseAWaitingManual = 2
	// ExitPhaseAFailed はフェーズA失敗（sourceは無変更。abortで後始末可能）。
	ExitPhaseAFailed = 10
	// ExitPhaseBFailed はフェーズB失敗（要手動対応）。
	ExitPhaseBFailed = 20
)
