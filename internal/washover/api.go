package washover

import (
	"github.com/terayu/pgwash/internal/config"
)

// Engine は洗い替えのオーケストレーター。UI に依存しない。
//
// New/RunPhaseA/RunPhaseB/Abort の実装は engine.go、
// 状態永続化関数（SaveState/LoadState等）の実装は state.go にある。
type Engine struct {
	cfg    config.Config
	events chan<- Event
}
