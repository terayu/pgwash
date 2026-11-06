package cli

import (
	"reflect"
	"testing"

	"github.com/spf13/pflag"
)

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ,, c ", []string{"a", "b", "c"}},
	}
	for _, c := range cases {
		got := splitCSV(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitCSV(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

func newTestFlagSet() *pflag.FlagSet {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	addGlobalFlags(fs)
	return fs
}

func TestFlagsFromCmdUnchangedFieldsAreNil(t *testing.T) {
	fs := newTestFlagSet()
	f := flagsFromCmd(fs)

	if f.Host != nil {
		t.Errorf("Host = %v, want nil (未指定)", *f.Host)
	}
	if f.Port != nil {
		t.Errorf("Port = %v, want nil (未指定)", *f.Port)
	}
	if f.AllTables != nil {
		t.Errorf("AllTables = %v, want nil (未指定)", *f.AllTables)
	}
	if f.Tables != nil {
		t.Errorf("Tables = %v, want nil (未指定)", f.Tables)
	}
	if f.Cutoff != nil {
		t.Errorf("Cutoff = %v, want nil (未指定)", *f.Cutoff)
	}
}

func TestFlagsFromCmdChangedFieldsArePopulated(t *testing.T) {
	fs := newTestFlagSet()
	if err := fs.Parse([]string{
		"--host=db.example.com",
		"--port=15432",
		"--user=alice",
		"--dbname=joywatcher",
		"--tables=t1, t2 ,t3",
		"--all-tables=true",
		"--cutoff=2026-08-01T00:00:00+09:00",
		"--phase-b=auto",
		"--block-mode=none",
		"--time-column=created_at",
		"--yes",
	}); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	f := flagsFromCmd(fs)

	if f.Host == nil || *f.Host != "db.example.com" {
		t.Errorf("Host = %v, want db.example.com", f.Host)
	}
	if f.Port == nil || *f.Port != 15432 {
		t.Errorf("Port = %v, want 15432", f.Port)
	}
	if f.User == nil || *f.User != "alice" {
		t.Errorf("User = %v, want alice", f.User)
	}
	if f.Database == nil || *f.Database != "joywatcher" {
		t.Errorf("Database = %v, want joywatcher", f.Database)
	}
	wantTables := []string{"t1", "t2", "t3"}
	if !reflect.DeepEqual(f.Tables, wantTables) {
		t.Errorf("Tables = %#v, want %#v", f.Tables, wantTables)
	}
	if f.AllTables == nil || *f.AllTables != true {
		t.Errorf("AllTables = %v, want true", f.AllTables)
	}
	if f.Cutoff == nil || *f.Cutoff != "2026-08-01T00:00:00+09:00" {
		t.Errorf("Cutoff = %v, want 2026-08-01T00:00:00+09:00", f.Cutoff)
	}
	if f.PhaseBMode == nil || *f.PhaseBMode != "auto" {
		t.Errorf("PhaseBMode = %v, want auto", f.PhaseBMode)
	}
	if f.BlockMode == nil || *f.BlockMode != "none" {
		t.Errorf("BlockMode = %v, want none", f.BlockMode)
	}
	if f.TimeColumn == nil || *f.TimeColumn != "created_at" {
		t.Errorf("TimeColumn = %v, want created_at", f.TimeColumn)
	}
	if f.Yes == nil || *f.Yes != true {
		t.Errorf("Yes = %v, want true", f.Yes)
	}
}

func TestBuildConfigFlagOverridesEnv(t *testing.T) {
	t.Setenv("PGHOST", "env-host")
	t.Setenv("PGPORT", "9999")

	fs := newTestFlagSet()
	if err := fs.Parse([]string{"--host=flag-host", "--user=u", "--dbname=d", "--phase-b=manual"}); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	cfg, err := buildConfig(fs)
	if err != nil {
		t.Fatalf("buildConfig() error: %v", err)
	}
	if cfg.Host != "flag-host" {
		t.Errorf("Host = %q, want flag-host (フラグが環境変数より優先されるべき)", cfg.Host)
	}
	if cfg.Port != 9999 {
		t.Errorf("Port = %d, want 9999 (PGPORTが未指定フラグのフォールバックとして使われるべき)", cfg.Port)
	}
}

func TestStateDirValueDefault(t *testing.T) {
	fs := newTestFlagSet()
	got := stateDirValue(fs)
	if got != ".pgwash" {
		t.Errorf("stateDirValue() = %q, want .pgwash", got)
	}
}

func TestStateDirValueEnvOverride(t *testing.T) {
	t.Setenv("PGWASH_STATE_DIR", "/tmp/custom-state")
	fs := newTestFlagSet()
	got := stateDirValue(fs)
	if got != "/tmp/custom-state" {
		t.Errorf("stateDirValue() = %q, want /tmp/custom-state", got)
	}
}

func TestStateDirValueFlagOverridesEnv(t *testing.T) {
	t.Setenv("PGWASH_STATE_DIR", "/tmp/custom-state")
	fs := newTestFlagSet()
	if err := fs.Parse([]string{"--state-dir=/tmp/flag-state"}); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	got := stateDirValue(fs)
	if got != "/tmp/flag-state" {
		t.Errorf("stateDirValue() = %q, want /tmp/flag-state", got)
	}
}

// --table-cutoff は複数回指定でき、指定順のまま config.Flags へ渡る。
func TestFlagsFromCmdTableCutoff(t *testing.T) {
	fs := newTestFlagSet()
	if err := fs.Parse([]string{
		"--table-cutoff=a=2026-05-01T00:00:00+09:00",
		"--table-cutoff", "b=2026-06-01T00:00:00+09:00",
	}); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	f := flagsFromCmd(fs)
	want := []string{"a=2026-05-01T00:00:00+09:00", "b=2026-06-01T00:00:00+09:00"}
	if !reflect.DeepEqual(f.TableCutoffs, want) {
		t.Errorf("TableCutoffs = %#v, want %#v", f.TableCutoffs, want)
	}
}

func TestFlagsFromCmdPhaseBExclude(t *testing.T) {
	fs := newTestFlagSet()
	if err := fs.Parse([]string{"--phase-b-exclude=23:00-01:00,12:00-13:00"}); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	f := flagsFromCmd(fs)
	if f.PhaseBExclude == nil || *f.PhaseBExclude != "23:00-01:00,12:00-13:00" {
		t.Errorf("PhaseBExclude = %v", f.PhaseBExclude)
	}
}
