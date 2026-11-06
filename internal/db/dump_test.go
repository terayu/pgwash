package db

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveBinFromPATH(t *testing.T) {
	got := resolveBin("", "pg_dump")
	if got != "pg_dump" {
		t.Errorf("resolveBin(\"\", \"pg_dump\") = %q, want %q", got, "pg_dump")
	}
}

func TestResolveBinFromDir(t *testing.T) {
	got := resolveBin("/usr/local/pgsql/bin", "pg_restore")

	want := "pg_restore"
	if runtime.GOOS == "windows" {
		want = "pg_restore.exe"
	}
	want = filepath.Join("/usr/local/pgsql/bin", want)

	if got != want {
		t.Errorf("resolveBin() = %q, want %q", got, want)
	}
}

// writeFakeBin は resolveBin(dir, name) が返すパスに実行可能なダミーファイルを作る。
func writeFakeBin(t *testing.T, dir, name string) {
	t.Helper()
	path := resolveBin(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writeFakeBin(%q): %v", name, err)
	}
}

func TestCheckDumpBinariesBothPresent(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "pg_dump")
	writeFakeBin(t, dir, "pg_restore")

	if err := CheckDumpBinaries(dir); err != nil {
		t.Errorf("CheckDumpBinaries(%q) = %v, want nil", dir, err)
	}
}

func TestCheckDumpBinariesOnlyPgDumpPresent(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "pg_dump")

	err := CheckDumpBinaries(dir)
	if err == nil {
		t.Fatal("CheckDumpBinaries() = nil, want error")
	}
	if !strings.Contains(err.Error(), "pg_restore") {
		t.Errorf("CheckDumpBinaries() error = %q, want it to mention pg_restore", err.Error())
	}
}

func TestCheckDumpBinariesNonePresent(t *testing.T) {
	dir := t.TempDir()

	err := CheckDumpBinaries(dir)
	if err == nil {
		t.Fatal("CheckDumpBinaries() = nil, want error")
	}
	if !strings.Contains(err.Error(), "pg_dump") {
		t.Errorf("CheckDumpBinaries() error = %q, want it to mention pg_dump", err.Error())
	}
}

func TestCheckDumpBinariesExplicitDirErrorMentionsFlag(t *testing.T) {
	dir := t.TempDir()

	err := CheckDumpBinaries(dir)
	if err == nil {
		t.Fatal("CheckDumpBinaries() = nil, want error")
	}
	if !strings.Contains(err.Error(), "--pg-bin-path") {
		t.Errorf("CheckDumpBinaries() error = %q, want it to mention --pg-bin-path", err.Error())
	}
}

func TestCheckDumpBinariesFromPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATHEXT依存を避けるためWindowsではスキップ")
	}

	dir := t.TempDir()
	writeFakeBin(t, dir, "pg_dump")
	writeFakeBin(t, dir, "pg_restore")
	t.Setenv("PATH", dir)

	if err := CheckDumpBinaries(""); err != nil {
		t.Errorf("CheckDumpBinaries(\"\") = %v, want nil", err)
	}
}

func TestCheckDumpBinariesFromPATHMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATHEXT依存を避けるためWindowsではスキップ")
	}

	dir := t.TempDir()
	t.Setenv("PATH", dir)

	err := CheckDumpBinaries("")
	if err == nil {
		t.Fatal("CheckDumpBinaries(\"\") = nil, want error")
	}
	if !strings.Contains(err.Error(), "PATH") {
		t.Errorf("CheckDumpBinaries() error = %q, want it to mention PATH", err.Error())
	}
}
