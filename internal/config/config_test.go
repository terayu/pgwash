package config

import (
	"testing"
	"time"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
func boolPtr(b bool) *bool    { return &b }

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(Flags{}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Schema != DefaultSchema {
		t.Errorf("Schema = %q, want %q", cfg.Schema, DefaultSchema)
	}
	if cfg.StateDir != DefaultStateDir {
		t.Errorf("StateDir = %q, want %q", cfg.StateDir, DefaultStateDir)
	}
	if cfg.BlockMode != BlockRevoke {
		t.Errorf("BlockMode = %q, want %q", cfg.BlockMode, BlockRevoke)
	}
	if cfg.Port != 5432 {
		t.Errorf("Port = %d, want 5432", cfg.Port)
	}
	if cfg.PhaseBMode != "" {
		t.Errorf("PhaseBMode = %q, want empty (no default)", cfg.PhaseBMode)
	}
}

func TestLoad_Priority_FlagOverEnvOverDefault(t *testing.T) {
	env := map[string]string{
		"PGHOST":            "env-host",
		"PGPORT":            "6000",
		"PGWASH_SCHEMA":     "env_schema",
		"PGWASH_BLOCK_MODE": "none",
	}
	getenv := func(k string) string { return env[k] }

	// 環境変数のみ
	cfg, err := Load(Flags{}, getenv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "env-host" {
		t.Errorf("Host = %q, want env-host", cfg.Host)
	}
	if cfg.Port != 6000 {
		t.Errorf("Port = %d, want 6000", cfg.Port)
	}
	if cfg.Schema != "env_schema" {
		t.Errorf("Schema = %q, want env_schema", cfg.Schema)
	}
	if cfg.BlockMode != BlockNone {
		t.Errorf("BlockMode = %q, want none", cfg.BlockMode)
	}

	// CLIフラグが環境変数を上書きする
	cfg2, err := Load(Flags{
		Host: strPtr("flag-host"),
		Port: intPtr(7000),
	}, getenv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg2.Host != "flag-host" {
		t.Errorf("Host = %q, want flag-host", cfg2.Host)
	}
	if cfg2.Port != 7000 {
		t.Errorf("Port = %d, want 7000", cfg2.Port)
	}
	// フラグ未指定の項目は引き続き環境変数が使われる
	if cfg2.Schema != "env_schema" {
		t.Errorf("Schema = %q, want env_schema", cfg2.Schema)
	}
}

func TestLoad_PasswordFromEnvOnly(t *testing.T) {
	env := map[string]string{"PGPASSWORD": "secret"}
	cfg, err := Load(Flags{}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Password != "secret" {
		t.Errorf("Password = %q, want secret", cfg.Password)
	}
}

func TestLoad_TablesEnvParsing(t *testing.T) {
	env := map[string]string{
		"PGWASH_TABLES":      " tbl1, tbl2 ,tbl3",
		"PGWASH_DROP_TABLES": "old_tbl",
	}
	cfg, err := Load(Flags{}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"tbl1", "tbl2", "tbl3"}
	if len(cfg.Tables) != len(want) {
		t.Fatalf("Tables = %v, want %v", cfg.Tables, want)
	}
	for i, w := range want {
		if cfg.Tables[i] != w {
			t.Errorf("Tables[%d] = %q, want %q", i, cfg.Tables[i], w)
		}
	}
	if len(cfg.DropTables) != 1 || cfg.DropTables[0] != "old_tbl" {
		t.Errorf("DropTables = %v, want [old_tbl]", cfg.DropTables)
	}
}

func TestLoad_TablesFlagOverridesEnv(t *testing.T) {
	env := map[string]string{"PGWASH_TABLES": "env1,env2"}
	cfg, err := Load(Flags{Tables: []string{"flag1"}}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Tables) != 1 || cfg.Tables[0] != "flag1" {
		t.Errorf("Tables = %v, want [flag1]", cfg.Tables)
	}
}

func TestLoad_CutoffParsing(t *testing.T) {
	cfg, err := Load(Flags{Cutoff: strPtr("2026-08-01T00:00:00+09:00")}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-08-01T00:00:00+09:00")
	if !cfg.Cutoff.Equal(want) {
		t.Errorf("Cutoff = %v, want %v", cfg.Cutoff, want)
	}
}

func TestLoad_CutoffInvalidFormat(t *testing.T) {
	_, err := Load(Flags{Cutoff: strPtr("not-a-date")}, func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error for invalid cutoff format, got nil")
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	_, err := Load(Flags{}, func(k string) string {
		if k == "PGPORT" {
			return "not-a-number"
		}
		return ""
	})
	if err == nil {
		t.Fatal("expected error for invalid PGPORT, got nil")
	}
}

func TestLoad_AllTablesBoolParsing(t *testing.T) {
	cfg, err := Load(Flags{}, func(k string) string {
		if k == "PGWASH_ALL_TABLES" {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.AllTables {
		t.Error("AllTables = false, want true")
	}
}

func validBaseConfig() Config {
	return Config{
		Host:       "localhost",
		Port:       5432,
		User:       "postgres",
		Database:   "joywatcher",
		Schema:     DefaultSchema,
		StateDir:   DefaultStateDir,
		BlockMode:  BlockRevoke,
		PhaseBMode: PhaseBManual,
	}
}

func TestValidate_OK(t *testing.T) {
	if err := validBaseConfig().Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_MissingConnectionInfo(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Host = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing host, got nil")
	}
}

func TestValidate_TablesAndAllTablesExclusive(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Tables = []string{"t1"}
	cfg.AllTables = true
	cfg.Cutoff = time.Now()
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for Tables+AllTables exclusivity, got nil")
	}
}

func TestValidate_CutoffRequiredWhenTablesSpecified(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Tables = []string{"t1"}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing cutoff, got nil")
	}
}

func TestValidate_CutoffRequiredWhenAllTables(t *testing.T) {
	cfg := validBaseConfig()
	cfg.AllTables = true
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing cutoff with AllTables, got nil")
	}
}

func TestValidate_CutoffNotRequiredWhenNoTrimRequested(t *testing.T) {
	cfg := validBaseConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_InvalidBlockMode(t *testing.T) {
	cfg := validBaseConfig()
	cfg.BlockMode = "bogus"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid BlockMode, got nil")
	}
}

func TestValidate_InvalidPhaseBMode(t *testing.T) {
	cfg := validBaseConfig()
	cfg.PhaseBMode = "bogus"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid PhaseBMode, got nil")
	}
}

func TestValidate_PhaseBModeSkip(t *testing.T) {
	cfg := validBaseConfig()
	cfg.PhaseBMode = PhaseBSkip
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_PhaseBModeRequired(t *testing.T) {
	cfg := validBaseConfig()
	cfg.PhaseBMode = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty PhaseBMode, got nil")
	}
}

func TestResolvePassword_AlreadySet(t *testing.T) {
	cfg := Config{Password: "existing"}
	called := false
	err := cfg.ResolvePassword(func() (string, error) {
		called = true
		return "prompted", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("prompt should not be called when Password already set")
	}
	if cfg.Password != "existing" {
		t.Errorf("Password = %q, want existing", cfg.Password)
	}
}

func TestResolvePassword_PromptsWhenEmpty(t *testing.T) {
	cfg := Config{}
	err := cfg.ResolvePassword(func() (string, error) {
		return "prompted-pw", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Password != "prompted-pw" {
		t.Errorf("Password = %q, want prompted-pw", cfg.Password)
	}
}

func TestResolvePassword_NilPromptNoop(t *testing.T) {
	cfg := Config{}
	if err := cfg.ResolvePassword(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Password != "" {
		t.Errorf("Password = %q, want empty", cfg.Password)
	}
}

func TestCutoffFor(t *testing.T) {
	common := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	own := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cfg := Config{Cutoff: common, TableCutoffs: map[string]time.Time{"a": own}}
	if got := cfg.CutoffFor("a"); !got.Equal(own) {
		t.Errorf("CutoffFor(a) = %v, want %v", got, own)
	}
	if got := cfg.CutoffFor("b"); !got.Equal(common) {
		t.Errorf("CutoffFor(b) = %v, want %v", got, common)
	}
}

func TestLoad_TableCutoffsFromFlags(t *testing.T) {
	cfg, err := Load(Flags{TableCutoffs: []string{"a=2026-05-01T00:00:00+09:00", " b = 2026-06-01T00:00:00+09:00 "}}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	jst := time.FixedZone("", 9*3600)
	if !cfg.TableCutoffs["a"].Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, jst)) || !cfg.TableCutoffs["b"].Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, jst)) {
		t.Errorf("TableCutoffs = %v", cfg.TableCutoffs)
	}
}

func TestLoad_TableCutoffsFromEnv(t *testing.T) {
	env := map[string]string{"PGWASH_TABLE_CUTOFFS": "a=2026-05-01T00:00:00Z,b=2026-06-01T00:00:00Z"}
	cfg, err := Load(Flags{}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TableCutoffs) != 2 {
		t.Errorf("TableCutoffs = %v, want 2 entries", cfg.TableCutoffs)
	}
}

func TestLoad_TableCutoffsFlagOverridesEnv(t *testing.T) {
	env := map[string]string{"PGWASH_TABLE_CUTOFFS": "a=2026-05-01T00:00:00Z"}
	cfg, err := Load(Flags{TableCutoffs: []string{"b=2026-06-01T00:00:00Z"}}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cfg.TableCutoffs["a"]; ok || len(cfg.TableCutoffs) != 1 {
		t.Errorf("TableCutoffs = %v, want only b", cfg.TableCutoffs)
	}
}

func TestLoad_TableCutoffsInvalid(t *testing.T) {
	cases := [][]string{
		{"a"},                     // = なし
		{"=2026-05-01T00:00:00Z"}, // テーブル名なし
		{"a="},                    // 日時なし
		{"a=2026-05-01"},          // RFC3339でない
		{"a=2026-05-01T00:00:00Z", "a=2026-06-01T00:00:00Z"}, // 重複
	}
	for _, c := range cases {
		if _, err := Load(Flags{TableCutoffs: c}, func(string) string { return "" }); err == nil {
			t.Errorf("Load(%v): expected error, got nil", c)
		}
	}
}

// --tables の全テーブルに個別日時があれば --cutoff は省略できる。
func TestValidate_CutoffOptionalWhenAllTablesHaveOwnCutoff(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Tables = []string{"t1", "t2"}
	cfg.TableCutoffs = map[string]time.Time{"t1": time.Now(), "t2": time.Now()}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_CutoffRequiredWhenSomeTableLacksOwnCutoff(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Tables = []string{"t1", "t2"}
	cfg.TableCutoffs = map[string]time.Time{"t1": time.Now()}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for t2 without cutoff, got nil")
	}
}

func TestValidate_CutoffRequiredWithAllTablesEvenIfTableCutoffs(t *testing.T) {
	cfg := validBaseConfig()
	cfg.AllTables = true
	cfg.TableCutoffs = map[string]time.Time{"t1": time.Now()}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for AllTables without --cutoff, got nil")
	}
}

func TestValidate_TableCutoffOnlyForTrimTargets(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"--tables に含まれない", func(c *Config) { c.Tables = []string{"t1"} }},
		{"足切り指定がない", func(c *Config) {}},
		{"--drop-tables に含まれる", func(c *Config) { c.AllTables = true; c.DropTables = []string{"t9"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validBaseConfig()
			cfg.Cutoff = time.Now()
			cfg.TableCutoffs = map[string]time.Time{"t9": time.Now()}
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestValidate_TableCutoffWithAllTables(t *testing.T) {
	cfg := validBaseConfig()
	cfg.AllTables = true
	cfg.Cutoff = time.Now()
	cfg.TableCutoffs = map[string]time.Time{"t9": time.Now()}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoad_PhaseBExclude(t *testing.T) {
	cfg, err := Load(Flags{PhaseBExclude: strPtr("23:00-01:00")}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if FormatTimeWindows(cfg.PhaseBExclude) != "23:00-01:00" {
		t.Errorf("PhaseBExclude = %v", cfg.PhaseBExclude)
	}

	env := map[string]string{"PGWASH_PHASE_B_EXCLUDE": "12:00-13:00"}
	cfg, err = Load(Flags{}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if FormatTimeWindows(cfg.PhaseBExclude) != "12:00-13:00" {
		t.Errorf("PhaseBExclude from env = %v", cfg.PhaseBExclude)
	}
}

func TestLoad_PhaseBExcludeInvalid(t *testing.T) {
	if _, err := Load(Flags{PhaseBExclude: strPtr("23:00")}, func(string) string { return "" }); err == nil {
		t.Error("expected error for invalid exclude window, got nil")
	}
}

func TestValidate_PhaseBExcludeOnlyWithAuto(t *testing.T) {
	ws, _ := ParseTimeWindows("23:00-01:00")
	for _, mode := range []PhaseBMode{PhaseBManual, PhaseBSkip} {
		cfg := validBaseConfig()
		cfg.PhaseBMode = mode
		cfg.PhaseBExclude = ws
		if err := cfg.Validate(); err == nil {
			t.Errorf("mode=%s: expected error, got nil", mode)
		}
	}
	cfg := validBaseConfig()
	cfg.PhaseBMode = PhaseBAuto
	cfg.PhaseBExclude = ws
	if err := cfg.Validate(); err != nil {
		t.Errorf("mode=auto: unexpected error: %v", err)
	}
}

func TestLoad_TimeColumn(t *testing.T) {
	// 既定値
	cfg, err := Load(Flags{}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TimeColumn != DefaultTimeColumn {
		t.Errorf("TimeColumn = %q, want %q", cfg.TimeColumn, DefaultTimeColumn)
	}

	// 環境変数
	getenv := func(k string) string {
		if k == "PGWASH_TIME_COLUMN" {
			return "created_at"
		}
		return ""
	}
	cfg, err = Load(Flags{}, getenv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TimeColumn != "created_at" {
		t.Errorf("TimeColumn = %q, want created_at", cfg.TimeColumn)
	}

	// CLIフラグが環境変数より優先される
	cfg, err = Load(Flags{TimeColumn: strPtr("logged_at")}, getenv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TimeColumn != "logged_at" {
		t.Errorf("TimeColumn = %q, want logged_at", cfg.TimeColumn)
	}
}

func TestTimeColumnName_FallbackForOldState(t *testing.T) {
	// time_column を持たない旧版の状態ファイルから復元した Config は空文字列になる。
	if got := (Config{}).TimeColumnName(); got != DefaultTimeColumn {
		t.Errorf("TimeColumnName() = %q, want %q", got, DefaultTimeColumn)
	}
	if got := (Config{TimeColumn: "created_at"}).TimeColumnName(); got != "created_at" {
		t.Errorf("TimeColumnName() = %q, want created_at", got)
	}
}
