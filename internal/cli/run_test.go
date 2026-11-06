package cli

import (
	"context"
	"testing"
	"time"

	"github.com/terayu/pgwash/internal/config"
)

// 現在時刻を含む時間帯（前後に十分な幅を持たせる）。
func windowAroundNow() []config.TimeWindow {
	now := time.Now()
	m := now.Hour()*60 + now.Minute()
	return []config.TimeWindow{{Start: (m + 24*60 - 60) % (24 * 60), End: (m + 60) % (24 * 60)}}
}

func TestWaitPhaseBExclude_NoWindowReturnsImmediately(t *testing.T) {
	if err := waitPhaseBExclude(context.Background(), nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// 現在時刻が除外時間の外なら待たずに戻る。
func TestWaitPhaseBExclude_OutsideWindowReturnsImmediately(t *testing.T) {
	now := time.Now()
	m := now.Hour()*60 + now.Minute()
	// 現在時刻の2〜3時間後の時間帯（現在時刻を含まない）。
	ws := []config.TimeWindow{{Start: (m + 120) % (24 * 60), End: (m + 180) % (24 * 60)}}

	done := make(chan error, 1)
	go func() { done <- waitPhaseBExclude(context.Background(), ws) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waitPhaseBExclude が除外時間の外で待機した")
	}
}

// 待機中に ctx がキャンセルされたら（SIGINT等）待たずにエラーを返す。
func TestWaitPhaseBExclude_CancelDuringWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitPhaseBExclude(ctx, windowAroundNow()); err == nil {
		t.Error("expected error when ctx is cancelled during wait, got nil")
	}
}
