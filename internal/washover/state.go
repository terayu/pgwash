// state.go は RunState の JSON永続化（<stateDir>/<RunID>.json）を実装する。
package washover

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func statePath(stateDir, runID string) string {
	return filepath.Join(stateDir, runID+".json")
}

// SaveState は st を <stateDir>/<RunID>.json へ書き出す。
// 書き込み途中でのファイル破損を避けるため、一時ファイルに書いてから rename する。
func SaveState(stateDir string, st *RunState) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("状態ディレクトリの作成に失敗しました: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("状態のJSON変換に失敗しました: %w", err)
	}
	path := statePath(stateDir, st.RunID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("状態ファイルの書き込みに失敗しました: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("状態ファイルの確定に失敗しました: %w", err)
	}
	return nil
}

// LoadState は指定 RunID の状態を読み込む。
func LoadState(stateDir, runID string) (*RunState, error) {
	data, err := os.ReadFile(statePath(stateDir, runID))
	if err != nil {
		return nil, fmt.Errorf("状態ファイルの読み込みに失敗しました: %w", err)
	}
	var st RunState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("状態ファイルの解析に失敗しました: %w", err)
	}
	return &st, nil
}

// LatestState は stateDir 内で最も新しい実行の状態を読み込む（--run-id 省略時に使う）。
func LatestState(stateDir string) (*RunState, error) {
	states, err := ListStates(stateDir)
	if err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return nil, fmt.Errorf("状態ファイルが見つかりません: %s", stateDir)
	}
	return states[0], nil
}

// ListStates は stateDir 内の全実行を新しい順（StartedAtの降順）に返す。
// stateDir が存在しない場合はエラーにせず空スライスを返す（初回実行前を許容するため）。
func ListStates(stateDir string) ([]*RunState, error) {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("状態ディレクトリの読み込みに失敗しました: %w", err)
	}

	var states []*RunState
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		runID := strings.TrimSuffix(ent.Name(), ".json")
		st, err := LoadState(stateDir, runID)
		if err != nil {
			return nil, err
		}
		states = append(states, st)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].StartedAt.After(states[j].StartedAt)
	})
	return states, nil
}

// CanRunPhaseB はフェーズBを開始してよい状態かを判定する。
// PhaseAComplete かつ全テーブルにエラーがないことが条件（設計案 10章）。
func CanRunPhaseB(st *RunState) error {
	if st.Phase != PhaseAComplete {
		return fmt.Errorf("フェーズBはフェーズA完了後のみ実行できます（現在のフェーズ: %s）", st.Phase)
	}
	for _, t := range st.Tables {
		if t.Error != "" {
			return fmt.Errorf("テーブル %s でエラーが発生しているためフェーズBを実行できません: %s", t.TableName, t.Error)
		}
	}
	return nil
}
