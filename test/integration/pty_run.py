#!/usr/bin/env python3
"""テスト用ヘルパー: /dev/tty からのマスク入力プロンプト(password.go)を自動応答させるため、
擬似端末(pty)を割り当てて子プロセスを実行する。
アプリコードは変更せず、CLIの対話入力を自動化するためだけの検証補助スクリプト。
使い方: TEST_PGPASSWORD=xxx python3 pty_run.py <cmd> <args...>

【現状の位置づけ】
当初は phase-b/abort が PGPASSWORD 環境変数を読まないバグ（internal/cli/phaseb.go,
abort.go が config.Load を経由しないため）の回避策として run.sh から使っていたが、
そのバグは internal/cli/password.go の applyEnvPassword で修正済み。
そのため run.sh の本流では使用しておらず（PGPASSWORD を直接渡す通常のラッパー
dpgw で足りる）、/dev/tty 経由のマスク入力プロンプト自体を手動で疎通確認したい
場合のための補助として残してある。
"""
import os
import pty
import select
import sys
import time

def main():
    argv = sys.argv[1:]
    password = os.environ.get("TEST_PGPASSWORD", "") + "\n"

    pid, fd = pty.fork()
    if pid == 0:
        os.execvp(argv[0], argv)
        os._exit(127)

    sent = False
    start = time.time()
    last_data = time.time()
    while True:
        try:
            r, _, _ = select.select([fd], [], [], 1.0)
        except OSError:
            break
        if fd in r:
            try:
                data = os.read(fd, 4096)
            except OSError:
                break
            if not data:
                break
            sys.stdout.buffer.write(data)
            sys.stdout.flush()
            last_data = time.time()
            # termenv等の端末問い合わせ(OSC11背景色クエリ/CSI6nカーソル位置クエリ)に
            # ダミー応答を返す。応答が来ないと初期化処理がタイムアウトまでブロックするため。
            if b"\x1b]11;?" in data or b"\x1b]10;?" in data:
                try:
                    os.write(fd, b"\x1b]11;rgb:0000/0000/0000\x07")
                except OSError:
                    pass
            if b"\x1b[6n" in data:
                try:
                    os.write(fd, b"\x1b[24;80R")
                except OSError:
                    pass
        else:
            # 1秒間データが来ておらず、まだパスワード未送信ならプロンプト待ちとみなして送る
            if not sent and time.time() - last_data > 1.0:
                try:
                    os.write(fd, password.encode())
                except OSError:
                    pass
                sent = True
            elif time.time() - start > 30:
                break

    _, status = os.waitpid(pid, 0)
    if os.WIFEXITED(status):
        sys.exit(os.WEXITSTATUS(status))
    sys.exit(1)

if __name__ == "__main__":
    main()
