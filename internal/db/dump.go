package db

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

// resolveBin は binPath が空文字ならPATH上の実行ファイル名をそのまま返し、
// 指定があればそのディレクトリ配下の実行ファイルパスを返す。
func resolveBin(binPath, name string) string {
	if binPath == "" {
		return name
	}
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(binPath, name)
}

// CheckDumpBinaries は pg_dump / pg_restore が実行可能かを事前に確認する。
// pg_dump はフェーズA2で、pg_restore はA4で実行されるが、A4はA3で一時DBを作成した後段のため、
// pg_restore の不在をそこで初めて検出すると一時DBだけが残り `pgwash abort` が必要になる。
// 作業開始前にこの関数でまとめて検査し、両方存在しなければ何も作らずに設定エラーとして終了させる。
func CheckDumpBinaries(binPath string) error {
	for _, name := range []string{"pg_dump", "pg_restore"} {
		bin := resolveBin(binPath, name)
		if _, err := exec.LookPath(bin); err != nil {
			if binPath == "" {
				return fmt.Errorf("%s が見つかりません。PATH を通すか --pg-bin-path で実行ファイルのあるディレクトリを指定してください: %w", name, err)
			}
			return fmt.Errorf("%s が見つかりません（--pg-bin-path=%s）: %w", name, binPath, err)
		}
	}
	return nil
}

// Dump は pg_dump --schema-only でスキーマのみを outFile へ出力する。
// --section 指定は使わない（テーブルACLが出力から漏れるのを避けるため、設計案 7.2節）。
// 終了コードを必ず検査し、失敗時はエラーを返す。
func Dump(ctx context.Context, binPath string, ci ConnInfo, outFile string) error {
	bin := resolveBin(binPath, "pg_dump")
	args := []string{
		"--host", ci.Host,
		"--port", strconv.Itoa(ci.Port),
		"--username", ci.User,
		"--no-password",
		"--format", "custom",
		"--blobs",
		"--schema-only",
		"--verbose",
		"--file", outFile,
		ci.Database,
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+ci.Password)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dumpの実行に失敗しました (%s): %w\nstderr: %s", ci.Database, err, stderr.String())
	}
	return nil
}

// Restore は pg_restore で inFile を ci.Database へ復元する。終了コードを必ず検査する。
// --exit-on-error を付け、最初のエラーで即座に失敗させる
// （既定動作のままだとエラーを継続・集計するだけで、スキーマが不完全なまま「成功」扱いになり得る）。
func Restore(ctx context.Context, binPath string, ci ConnInfo, inFile string) error {
	bin := resolveBin(binPath, "pg_restore")
	args := []string{
		"--host", ci.Host,
		"--port", strconv.Itoa(ci.Port),
		"--username", ci.User,
		"--no-password",
		"--dbname", ci.Database,
		"--verbose",
		"--exit-on-error",
		inFile,
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+ci.Password)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restoreの実行に失敗しました (%s): %w\nstderr: %s", ci.Database, err, stderr.String())
	}
	return nil
}
