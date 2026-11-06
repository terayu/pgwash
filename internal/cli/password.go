package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/terayu/pgwash/internal/config"
)

// passwordPrompt は config.Config.ResolvePassword に渡すプロンプト関数。
// config.Load が PGPASSWORD 環境変数を既に Config.Password へ反映しているため、
// ここに到達するのは環境変数未設定時のみ（=マスク入力が必要な場合のみ）。
//
// 設計案 5.1節・10章のとおり、パスワードは --password のようなCLIフラグでは受け付けない
// （ps でプロセス一覧から見えてしまうため）。
//
// マスク入力は golang.org/x/term を追加依存にせず、/dev/tty 上で `stty -echo` を
// 呼び出すことで実現している（Unix系のみ。Windowsは非対応）。/dev/tty が使えない
// 環境（cron等の非対話実行）では PGPASSWORD 環境変数が必須。
func passwordPrompt() (string, error) {
	return promptPasswordMasked("パスワード: ")
}

// applyEnvPassword は PGPASSWORD 環境変数を cfg.Password へ反映して返す。
//
// Config.Password は json:"-" のため状態ファイルには保存されない（HANDOFF.md #8）。
// phase-b/abort のように RunState から Config を復元して使う経路は config.Load を
// 経由しないため、ここで明示的に PGPASSWORD を反映しないと常に空のまま
// ResolvePassword のプロンプトへ落ちてしまう（PGPASSWORD設定済みでも無視される）。
// 既に cfg.Password が設定済みならそのまま維持する。
func applyEnvPassword(cfg config.Config) config.Config {
	if cfg.Password == "" {
		cfg.Password = os.Getenv("PGPASSWORD")
	}
	return cfg
}

func promptPasswordMasked(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("端末からの入力ができません。PGPASSWORD環境変数を設定してください: %w", err)
	}
	defer tty.Close()

	fmt.Fprint(tty, prompt)

	sttyOff := exec.Command("stty", "-echo")
	sttyOff.Stdin = tty
	if err := sttyOff.Run(); err != nil {
		return "", fmt.Errorf("端末のecho無効化に失敗しました。PGPASSWORD環境変数を設定してください: %w", err)
	}
	defer func() {
		sttyOn := exec.Command("stty", "echo")
		sttyOn.Stdin = tty
		_ = sttyOn.Run()
		fmt.Fprintln(tty)
	}()

	reader := bufio.NewReader(tty)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("パスワードの読み取りに失敗しました: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
