package washover

import (
	"strings"
	"testing"
	"time"

	"github.com/terayu/pgwash/internal/config"
	"github.com/terayu/pgwash/internal/db"
)

// ─────────────────────────────────────────────
// DecideTableAction（設計案 5.5節）
// ─────────────────────────────────────────────

func TestDecideTableAction(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		hasPtime   bool
		tables     []string
		allTables  bool
		dropTables []string
		wantAction TableAction
		wantWarn   bool
	}{
		{
			name:       "DropTablesに含まれる場合はptime有無に関わらずDrop",
			table:      "old_tbl",
			hasPtime:   true,
			dropTables: []string{"old_tbl"},
			wantAction: ActionDrop,
			wantWarn:   false,
		},
		{
			name:       "DropTablesが優先される（Tablesにも含まれていても）",
			table:      "old_tbl",
			hasPtime:   true,
			tables:     []string{"old_tbl"},
			dropTables: []string{"old_tbl"},
			wantAction: ActionDrop,
			wantWarn:   false,
		},
		{
			name:       "Tablesに該当しptime列があればTrim",
			table:      "trend1",
			hasPtime:   true,
			tables:     []string{"trend1"},
			wantAction: ActionTrim,
			wantWarn:   false,
		},
		{
			name:       "AllTablesかつptime列があればTrim",
			table:      "trend1",
			hasPtime:   true,
			allTables:  true,
			wantAction: ActionTrim,
			wantWarn:   false,
		},
		{
			name:       "Tablesに該当してもptime列がなければCopyAllに落ちて警告",
			table:      "master1",
			hasPtime:   false,
			tables:     []string{"master1"},
			wantAction: ActionCopyAll,
			wantWarn:   true,
		},
		{
			name:       "AllTablesの場合ptime列がなければ自動的に対象外（警告なし）",
			table:      "master1",
			hasPtime:   false,
			allTables:  true,
			wantAction: ActionCopyAll,
			wantWarn:   false,
		},
		{
			name:       "非選択テーブルは常にCopyAll",
			table:      "unrelated",
			hasPtime:   true,
			tables:     []string{"other_table"},
			wantAction: ActionCopyAll,
			wantWarn:   false,
		},
		{
			name:       "何も指定がなければCopyAll（既定は現状維持）",
			table:      "anything",
			hasPtime:   true,
			wantAction: ActionCopyAll,
			wantWarn:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Tables:     tt.tables,
				AllTables:  tt.allTables,
				DropTables: tt.dropTables,
			}
			gotAction, gotWarn := DecideTableAction(tt.table, tt.hasPtime, cfg)
			if gotAction != tt.wantAction {
				t.Errorf("action = %v, want %v", gotAction, tt.wantAction)
			}
			if gotWarn != tt.wantWarn {
				t.Errorf("warn = %v, want %v", gotWarn, tt.wantWarn)
			}
		})
	}
}

// ─────────────────────────────────────────────
// decidePhaseBBoundary（ダウンタイム削減設計案 6章 B4）
// ─────────────────────────────────────────────

func TestDecidePhaseBBoundary(t *testing.T) {
	cutoff := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	maxP := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		hasPtime       bool
		action         TableAction
		maxPtime       *time.Time
		wantTruncate   bool
		wantBoundary   *time.Time
		wantDeleteFlag bool
	}{
		{
			name:         "ptime列なしはTRUNCATE+全件再コピー",
			hasPtime:     false,
			action:       ActionCopyAll,
			maxPtime:     nil,
			wantTruncate: true,
			wantBoundary: nil,
		},
		{
			name:         "ptime列なしはActionに関わらずTRUNCATE",
			hasPtime:     false,
			action:       ActionTrim,
			maxPtime:     &maxP,
			wantTruncate: true,
			wantBoundary: nil,
		},
		{
			name:           "ptime列ありmaxPtimeありはmaxPtimeを境界に削除してから再取得",
			hasPtime:       true,
			action:         ActionTrim,
			maxPtime:       &maxP,
			wantTruncate:   false,
			wantBoundary:   &maxP,
			wantDeleteFlag: true,
		},
		{
			name:           "ptime列ありmaxPtimeありはCopyAllでも差分方式を使う",
			hasPtime:       true,
			action:         ActionCopyAll,
			maxPtime:       &maxP,
			wantTruncate:   false,
			wantBoundary:   &maxP,
			wantDeleteFlag: true,
		},
		{
			name:           "ptime列ありmaxPtimeがnilかつTrimはCutoffを境界に（削除不要）",
			hasPtime:       true,
			action:         ActionTrim,
			maxPtime:       nil,
			wantTruncate:   false,
			wantBoundary:   &cutoff,
			wantDeleteFlag: false,
		},
		{
			// ActionCopyAll はフェーズAが WHERE句なしでコピーするため、ptime が
			// 全行NULLのテーブルではフェーズA分がコピー済みのままMAX(ptime)がNULLになりうる。
			// 増分判定ができないため、単純な無条件CopyAllではフェーズA分と重複する。
			// TRUNCATE + 全件再コピーの安全側に倒す。
			name:           "ptime列ありmaxPtimeがnilかつCopyAllはTRUNCATE+全件再コピー（重複防止）",
			hasPtime:       true,
			action:         ActionCopyAll,
			maxPtime:       nil,
			wantTruncate:   true,
			wantBoundary:   nil,
			wantDeleteFlag: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTruncate, gotBoundary, gotDelete := decidePhaseBBoundary(tt.hasPtime, tt.action, tt.maxPtime, cutoff)
			if gotTruncate != tt.wantTruncate {
				t.Errorf("truncate = %v, want %v", gotTruncate, tt.wantTruncate)
			}
			if (gotBoundary == nil) != (tt.wantBoundary == nil) {
				t.Fatalf("boundary nil-ness mismatch: got %v, want %v", gotBoundary, tt.wantBoundary)
			}
			if gotBoundary != nil && !gotBoundary.Equal(*tt.wantBoundary) {
				t.Errorf("boundary = %v, want %v", *gotBoundary, *tt.wantBoundary)
			}
			if gotDelete != tt.wantDeleteFlag {
				t.Errorf("deleteBoundary = %v, want %v", gotDelete, tt.wantDeleteFlag)
			}
		})
	}
}

func TestRolesWithConnect(t *testing.T) {
	acl := []db.ACLEntry{
		{Grantee: "PUBLIC", PrivilegeType: "CONNECT"},
		{Grantee: "app_user", PrivilegeType: "CONNECT"},
		{Grantee: "app_user", PrivilegeType: "TEMPORARY"},
		{Grantee: "readonly", PrivilegeType: "CONNECT"},
	}
	got := rolesWithConnect(acl)
	want := []string{"app_user", "readonly"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestNewRunID(t *testing.T) {
	now := time.Date(2026, 8, 8, 15, 30, 0, 0, time.UTC)
	got := newRunID(now)
	want := "20260808_153000"
	if got != want {
		t.Errorf("newRunID = %q, want %q", got, want)
	}
}

// ─────────────────────────────────────────────
// validateTableNames（存在しないテーブル名の指定検出）
// ─────────────────────────────────────────────

func TestValidateTableNames(t *testing.T) {
	tables := []db.TableInfo{
		{Name: "t1"},
		{Name: "t2"},
	}

	tests := []struct {
		name       string
		cfgTables  []string
		dropTables []string
		wantErr    bool
	}{
		{name: "全て実在すればOK", cfgTables: []string{"t1"}, dropTables: []string{"t2"}, wantErr: false},
		{name: "指定なしはOK", wantErr: false},
		{name: "Tablesにタイプミスがあればエラー", cfgTables: []string{"t1_typo"}, wantErr: true},
		{name: "DropTablesにタイプミスがあればエラー", dropTables: []string{"unknown_tbl"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{Tables: tt.cfgTables, DropTables: tt.dropTables}
			err := validateTableNames(cfg, tables)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTableNames() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ─────────────────────────────────────────────
// afterRenameHint（B4成功後の失敗時の手動復旧手順）
// ─────────────────────────────────────────────

func TestAfterRenameHint(t *testing.T) {
	st := &RunState{TempDBName: "joywatcher_new_20260808_153000", BackupDBName: "joywatcher_bak_20260808_153000"}
	hint := afterRenameHint(st, "joywatcher")

	for _, want := range []string{
		"joywatcher_bak_20260808_153000",
		"ALTER DATABASE joywatcher_bak_20260808_153000 RENAME TO joywatcher",
		"ALTER DATABASE joywatcher_new_20260808_153000 RENAME TO joywatcher",
	} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint = %q, want it to contain %q", hint, want)
		}
	}
}

func TestValidateTableNames_TableCutoffs(t *testing.T) {
	tables := []db.TableInfo{{Name: "t1"}}
	cfg := config.Config{Tables: []string{"t1"}, TableCutoffs: map[string]time.Time{"t1_typo": time.Now()}}
	if err := validateTableNames(cfg, tables); err == nil {
		t.Error("expected error for unknown table in TableCutoffs, got nil")
	}
}

func TestValidateTableCutoffs(t *testing.T) {
	now := time.Now()
	results := []TableResult{
		{TableName: "trim1", HasPtime: true, Action: ActionTrim},
		{TableName: "trim2", HasPtime: true, Action: ActionTrim},
		{TableName: "noptime", HasPtime: false, Action: ActionCopyAll},
	}
	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
	}{
		{name: "共通日時のみ", cfg: config.Config{Cutoff: now}},
		{name: "共通＋個別", cfg: config.Config{Cutoff: now, TableCutoffs: map[string]time.Time{"trim1": now}}},
		{name: "全て個別で共通なし", cfg: config.Config{TableCutoffs: map[string]time.Time{"trim1": now, "trim2": now}}},
		{name: "日時のないTrim対象があればエラー", cfg: config.Config{TableCutoffs: map[string]time.Time{"trim1": now}}, wantErr: true},
		{name: "Trim対象外への個別指定はエラー", cfg: config.Config{Cutoff: now, TableCutoffs: map[string]time.Time{"noptime": now}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTableCutoffs(tt.cfg, results)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTableCutoffs() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
