# pgwash 結合検証（PostgreSQL 9.3 / Docker）

実DBを使った結合検証一式。ホストが arm64 (Apple Silicon) で `psql`/`pg_dump` が無いため、
`postgres:9.3` コンテナ内で pgwash バイナリ自体を実行する構成を取っている。

## 構成

- `pgwash-linux-arm64` — `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build` で作った静的バイナリ
  （このディレクトリは docker run 時に `/work` としてコンテナへbind mountされる。
  postgres:9.3 イメージ自体は amd64 だが、pgwash はホストと同じ arm64 で動かす。理由は下記）
- `pty_run.py` — （現在は本流では未使用。過去に存在した「phase-b/abortがPGPASSWORDを読まない」
  バグの回避策だったが修正済みのため不要になった。マスク入力プロンプト自体を手動で
  疎通確認したい場合の補助として残置。詳細はファイル冒頭のコメント参照）
- `run.sh` — 全シナリオを通しで実行する回帰テストドライバスクリプト。
  期待値と実測値を `assert_eq` で照合し、不一致があれば非ゼロで終了する
  （末尾に `PASS: 全N項目` / `FAIL: N項目中M件失敗` のサマリを出力）
- `lib.sh` — 共通関数（コンテナ起動、psql/pgwash 実行ラッパー、アサーション機構等）
- `seed.sql` — 検証用テーブル・データ・ロールの投入SQL（`run.sh` から都度実行）

## 実行方法

```bash
cd pgwash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o test/integration/pgwash-linux-arm64 ./cmd/pgwash
bash test/integration/run.sh
echo "exit=$?"   # 0 なら全項目PASS、非0ならFAILあり（詳細はサマリ出力を参照）
```

検証後、コンテナは自動では削除されない。`docker rm -f pgwash-test` で削除すること。

## 既知のハマりどころ（環境固有、pgwashのバグではない）

- `postgres:9.3` イメージを Apple Silicon 上で `--platform linux/amd64` エミュレーション実行すると、
  entrypoint が root から `gosu postgres` へ降格する際に `gosu`（Go 1.6 時代の静的バイナリ）が
  `runtime: failed to create new OS thread ... fatal error: newosproc` でクラッシュしコンテナが起動しない。
  回避策として `docker run --user postgres ...` を指定し、entrypoint の gosu 降格パス自体を
  スキップさせている（entrypoint.sh は `id -u = 0` のときのみ gosu を使う分岐になっている）。
- pgwash 自体を `GOOS=linux GOARCH=amd64` でビルドしてこのRosetta環境で動かすと、ネットワーク接続時に
  `runtime: taggedPointerPack invalid packing ... fatal error: taggedPointerPack` でクラッシュする
  （Go 1.21+ ランタイムのタグ付きポインタ実装が、Rosetta 2 の mmap 返却アドレスと衝突する既知の非互換）。
  pg_dump/pg_restore はCで書かれておりこの問題を踏まないため、pgwash だけ `GOARCH=arm64` でビルドし、
  ホストと同じ arm64 カーネル上でネイティブ実行する（postgres:9.3コンテナのrootfsはamd64のままでよい。
  ELFヘッダがarm64ならカーネルがそのまま実行する）。
- macOS の `/tmp` はコンテナ側のファイル共有と同期されないことがあるため、バイナリは
  プロジェクト配下（`/Users/...` 以下）に置いて bind mount する。
- （過去の注意点。修正済み）`phase-b`/`abort` サブコマンドはかつて `PGPASSWORD` 環境変数を
  読まないバグがあり `pty_run.py` 経由の実行が必須だったが、`internal/cli/password.go` の
  `applyEnvPassword` で修正され、現在は他コマンドと同じく `-e PGPASSWORD=...` を渡すだけでよい。
