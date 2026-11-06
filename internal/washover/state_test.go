package washover

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	st := &RunState{
		RunID:     "20260808_153000",
		Phase:     PhaseAComplete,
		StartedAt: time.Date(2026, 8, 8, 15, 30, 0, 0, time.UTC),
		Tables: []TableResult{
			{TableName: "t1", HasPtime: true, Action: ActionTrim, RowsPhaseA: 100},
		},
	}
	if err := SaveState(dir, st); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := LoadState(dir, st.RunID)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded.RunID != st.RunID {
		t.Errorf("RunID = %q, want %q", loaded.RunID, st.RunID)
	}
	if loaded.Phase != st.Phase {
		t.Errorf("Phase = %q, want %q", loaded.Phase, st.Phase)
	}
	if len(loaded.Tables) != 1 || loaded.Tables[0].TableName != "t1" {
		t.Errorf("Tables = %+v", loaded.Tables)
	}
}

func TestSaveState_PasswordNotPersisted(t *testing.T) {
	dir := t.TempDir()
	st := &RunState{
		RunID: "run1",
		Phase: PhaseARunning,
	}
	st.Config.Password = "super-secret"
	if err := SaveState(dir, st); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	data, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(data) != 1 {
		t.Fatalf("expected 1 state file, got %v (err=%v)", data, err)
	}

	loaded, err := LoadState(dir, "run1")
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded.Config.Password != "" {
		t.Errorf("Password should not be persisted, got %q", loaded.Config.Password)
	}
}

func TestListStates_NewestFirst(t *testing.T) {
	dir := t.TempDir()
	older := &RunState{RunID: "20260101_000000", StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Phase: PhaseComplete}
	newer := &RunState{RunID: "20260808_000000", StartedAt: time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC), Phase: PhaseAComplete}
	if err := SaveState(dir, older); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(dir, newer); err != nil {
		t.Fatal(err)
	}

	states, err := ListStates(dir)
	if err != nil {
		t.Fatalf("ListStates failed: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("expected 2 states, got %d", len(states))
	}
	if states[0].RunID != newer.RunID {
		t.Errorf("states[0].RunID = %q, want %q (newest first)", states[0].RunID, newer.RunID)
	}
	if states[1].RunID != older.RunID {
		t.Errorf("states[1].RunID = %q, want %q", states[1].RunID, older.RunID)
	}
}

func TestListStates_EmptyDirNoError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	states, err := ListStates(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("expected 0 states, got %d", len(states))
	}
}

func TestLatestState(t *testing.T) {
	dir := t.TempDir()
	older := &RunState{RunID: "20260101_000000", StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	newer := &RunState{RunID: "20260808_000000", StartedAt: time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)}
	_ = SaveState(dir, older)
	_ = SaveState(dir, newer)

	got, err := LatestState(dir)
	if err != nil {
		t.Fatalf("LatestState failed: %v", err)
	}
	if got.RunID != newer.RunID {
		t.Errorf("LatestState RunID = %q, want %q", got.RunID, newer.RunID)
	}
}

func TestLatestState_NoStates(t *testing.T) {
	dir := t.TempDir()
	_, err := LatestState(dir)
	if err == nil {
		t.Error("expected error when no states exist, got nil")
	}
}

func TestCanRunPhaseB(t *testing.T) {
	tests := []struct {
		name    string
		phase   Phase
		tables  []TableResult
		wantErr bool
	}{
		{name: "PhaseACompleteでエラーなしならOK", phase: PhaseAComplete, wantErr: false},
		{name: "PhaseNotStartedはNG", phase: PhaseNotStarted, wantErr: true},
		{name: "PhaseARunningはNG", phase: PhaseARunning, wantErr: true},
		{
			name:    "テーブルにエラーがあればNG",
			phase:   PhaseAComplete,
			tables:  []TableResult{{TableName: "t1", Error: "boom"}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &RunState{Phase: tt.phase, Tables: tt.tables}
			err := CanRunPhaseB(st)
			if (err != nil) != tt.wantErr {
				t.Errorf("CanRunPhaseB() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
