# pgwash

PostgreSQL の「洗い替え」——古いトレンドデータを運用DBから切り離し、直近データだけを残してDBを作り直す処理——を行う TUI / CLI ツールです。

`DELETE` + `VACUUM FULL` ではなく **新しいDBを裏で組み立ててから一瞬で入れ替える** 方式を取ることで、サービス停止時間を差分反映＋リネームの数十秒程度に抑えます。

## 特徴

- **ダウンタイムの最小化** — 重いデータコピーは運用DBを止めずに実行し（フェーズA）、停止が必要なのは差分反映とDBリネームだけ（フェーズB）
- **データを減らす操作はオプトイン** — 既定は全テーブル全件コピー（現状維持）。足切り・テーブル削除は明示指定したときだけ発動するため、指定漏れでデータを失いません
- **TUI と ヘッドレス実行の両対応** — 対話ウィザードでの確認実行と、cron 等からの完全自動実行を同じバイナリで
- **フェーズBの実行タイミングを分離可能** — 日中にフェーズAを流しておき、メンテナンス時間帯にフェーズBだけを実行できます
- **中断・再開・状態確認** — 実行状態はファイルに永続化され、`status` / `abort` / `phase-b` で操作できます

## 仕組み

```
フェーズA（サービス稼働中・時間がかかってよい）
  A0  source の接続権限（オーナー / ACL / DB設定）を記録
  A1  テーブル一覧と ptime 列の有無を取得
  A2  pg_dump --schema-only でスキーマを取得
  A3  一時DB(<db>_new_<runid>) を作成
  A4  スキーマを復元
  A5  --drop-tables 指定のテーブルを削除
  A6  dblink 拡張を有効化
  A7  dblink 経由でデータをコピー（足切り対象は ptime >= cutoff のみ）

      ── --phase-b=manual ならここで停止（終了コード 2）/ =skip なら完了（終了コード 0）──

フェーズB（この間だけサービス停止）
  B1  source への新規接続を遮断（REVOKE CONNECT）
  B2  既存接続を切断（pg_terminate_backend）
  B3  フェーズA以降に増えた差分を反映
  B4  source を退避DB名(<db>_bak_<runid>)へリネーム
  B5  一時DBを本来のDB名へリネーム
  B6  A0 で記録した接続権限を新DBへ再適用
  B7  退避DBの接続権限を復旧
```

フェーズAが失敗しても source は無変更です。`pgwash abort` で一時DBを削除して後始末できます。

## 動作要件

- PostgreSQL（`dblink` 拡張が利用可能であること）
- `pg_dump` / `pg_restore`（`--pg-bin-path` で明示指定も可）。存在は `run` / `tui` の開始時に検証され、見つからなければ何も作成せず終了コード `1` で終了します
- 接続ユーザーに **スーパーユーザー権限** — `CREATE/ALTER/DROP DATABASE` と `CREATE EXTENSION` に必要です
- ビルドする場合は Go 1.25 以降

## インストール

```bash
go install github.com/terayu/pgwash/cmd/pgwash@latest
```

リポジトリからビルドする場合（[Task](https://taskfile.dev) を使用）:

```bash
git clone git@github.com:terayu/pgwash.git
cd pgwash
task build          # bin/pgwash を生成
task buildwin       # bin/pgwash.exe を生成（windows/amd64 クロスビルド）
```

Task を使わない場合は `go build -o bin/pgwash ./cmd/pgwash` でも同じです。

## 使い方

### 1. 対象テーブルを調べる

まず、どのテーブルが足切りできるか（`ptime` 列を持つか）と、使われていないテーブルがないかを確認します。

```bash
pgwash list-tables --host db.example.com --user postgres --dbname joywatcher
```

```
テーブル名     ptime  推定行数  最終ptime   サイズ
trend_data     ○      12345678  2026-08-08  4.2 GB
alarm_log      ○        234567  2026-08-08  120.0 MB
master_config  -           128  -           16.0 kB
```

### 2. TUI で実行する

引数なしで起動すると対話ウィザードが立ち上がります。起動引数や環境変数で渡した値は各入力欄の初期値になります。

```bash
pgwash
```

### 3. ヘッドレスで実行する

cron 等からの自動実行を想定したモードです。`--yes` の指定が必須です。

```bash
pgwash run \
  --host db.example.com --user postgres --dbname joywatcher \
  --all-tables \
  --cutoff 2026-08-01T00:00:00+09:00 \
  --phase-b auto \
  --yes
```

`--phase-b auto` に `--phase-b-exclude` を指定すると、フェーズA完了時刻がその時間帯に入っていた場合、時間帯を抜けるまで待ってからフェーズBを開始します（例: 23:00〜01:00 はバックアップが動くので避ける）。時刻はローカル時刻で、日付をまたぐ指定もできます。判定するのは**開始時刻のみ**で、フェーズBの途中で時間帯に入っても中断はしません。待機中に中断した場合は `pgwash phase-b` で再開できます。

```bash
pgwash run ... --phase-b auto --phase-b-exclude 23:00-01:00 --yes
```

### 4. フェーズBを別のタイミングで実行する

`--phase-b manual` を指定するとフェーズA完了時点で停止し、終了コード `2` を返します。

```bash
# 日中：重いコピーだけ済ませておく
pgwash run ... --phase-b manual --yes

# メンテナンス時間帯：停止を伴う処理だけを実行
pgwash phase-b --yes
```

`--run-id` を省略すると `--state-dir` 内の最新の実行が対象になります。

### 5. バックアップ用途（フェーズBを実行しない）

`--phase-b skip` を指定するとフェーズAのみを実行し、`<db>_new_<runid>` をバックアップとして残します。source は一切変更されず、ダウンタイムも発生しません。`--tables` / `--cutoff` / `--drop-tables` も通常どおり適用されるため、間引いたバックアップを取ることもできます。あとから `pgwash phase-b` で本番へ入れ替えることも、不要になれば `pgwash abort` でバックアップを削除することもできます。

フェーズBの差分反映（B3）は行われないため、このバックアップはフェーズAのコピー完了時点のスナップショットです。フェーズA実行中・実行後に source へ書き込まれた行は含まれません。

```bash
pgwash run \
  --host db.example.com --user postgres --dbname joywatcher \
  --phase-b skip \
  --yes
```

### 6. 状態確認・中断

```bash
pgwash status                    # 実行の一覧
pgwash status --run-id 20260808_153000   # 個別の詳細
pgwash abort                     # フェーズB開始前に限り、一時DBを削除して中断
```

## テーブル処理の決定ルール

**`--tables` は「コピーする対象」ではなく「足切りを適用する対象」です。** 非選択のテーブルは全件コピーされます。

| 条件 | 処理 |
|---|---|
| `--drop-tables` に指定あり | **drop** — 新DBへ引き継がない |
| `--tables` / `--all-tables` に該当し、かつ `ptime` 列がある | **trim** — `ptime >= cutoff` の行だけコピー |
| 上記以外（非選択、または `ptime` 列なし） | **copy_all** — 全件コピー（既定） |

`--tables` に指定したテーブルに `ptime` 列がない場合は copy_all に落として警告します。存在しないテーブル名を指定した場合は、破壊的操作を始める前にエラーで中断します。

## 設定

優先順位は **CLIフラグ > 環境変数 > 既定値** です。

| フラグ | 環境変数 | 既定値 | 説明 |
|---|---|---|---|
| `--host` | `PGHOST` | — | 接続先ホスト |
| `--port` | `PGPORT` | `5432` | ポート |
| `--user` | `PGUSER` | — | 接続ユーザー（スーパーユーザー権限が必要） |
| — | `PGPASSWORD` | — | パスワード（下記参照） |
| `--dbname` | `PGDATABASE` | — | 洗い替え対象のDB名 |
| `--schema` | `PGWASH_SCHEMA` | `public` | 対象スキーマ |
| `--tables` | `PGWASH_TABLES` | — | 足切りを適用するテーブル名（カンマ区切り） |
| `--all-tables` | `PGWASH_ALL_TABLES` | `false` | 日時列（`--time-column`）を持つ全テーブルに足切りを適用 |
| `--time-column` | `PGWASH_TIME_COLUMN` | `ptime` | 足切り・差分判定に使う日時列名（全テーブル共通）。`timestamp` / `timestamptz` 型の列を指定。本文中の `ptime` はこの列を指す |
| `--drop-tables` | `PGWASH_DROP_TABLES` | — | 新DBへ引き継がないテーブル名（カンマ区切り） |
| `--cutoff` | `PGWASH_CUTOFF` | — | 足切り日時（全足切り対象に共通）。RFC3339形式（例 `2026-08-01T00:00:00+09:00`） |
| `--table-cutoff` | `PGWASH_TABLE_CUTOFFS` | — | テーブル別の足切り日時。`テーブル名=RFC3339日時` 形式で、フラグは複数回指定可、環境変数はカンマ区切り。指定したテーブルでは `--cutoff` より優先 |
| `--phase-b` | `PGWASH_PHASE_B_MODE` | — | フェーズBの実行タイミング `auto` \| `manual` \| `skip`（**必須**） |
| `--phase-b-exclude` | `PGWASH_PHASE_B_EXCLUDE` | — | `auto` のときフェーズBの開始を避ける時間帯。`HH:MM-HH:MM`、カンマ区切りで複数指定可（例 `23:00-01:00`） |
| `--block-mode` | `PGWASH_BLOCK_MODE` | `revoke` | フェーズBでの新規接続遮断方式 `revoke` \| `none` |
| `--state-dir` | `PGWASH_STATE_DIR` | `.pgwash` | 状態ファイルの保存先 |
| `--pg-bin-path` | — | — | `pg_dump` / `pg_restore` の実行パス |
| `--run-id` | — | 最新の実行 | 操作対象の実行ID |
| `--yes` | — | `false` | 確認プロンプトを省略（ヘッドレス実行では必須） |
| `--log-format` | — | `text` | ログ出力形式 `text` \| `json` |

`--tables` と `--all-tables` は排他です。いずれかを指定する場合は `--cutoff` が必須です（`--tables` の全テーブルに `--table-cutoff` を指定した場合は省略可）。`--table-cutoff` は足切り対象のテーブルにのみ指定できます。

```bash
# orders だけ別の日時で足切りし、それ以外の足切り対象は --cutoff を使う
pgwash run ... \
  --tables orders,access_logs,events \
  --cutoff 2026-08-01T00:00:00+09:00 \
  --table-cutoff orders=2026-05-01T00:00:00+09:00
```

### パスワードの扱い

**パスワードを渡す CLI フラグは意図的に用意していません。** `ps` などのプロセス一覧から見えてしまうためです。`PGPASSWORD` 環境変数を使うか、未設定であれば TUI / 端末のマスク入力から求められます。状態ファイルにも保存されません（フェーズB単独実行時は再入力が必要です）。

### `--block-mode` の選び方

| 値 | 動作 | 適する状況 |
|---|---|---|
| `revoke`（既定） | `REVOKE CONNECT` で一般ユーザーの新規接続を遮断 | アプリが非スーパーユーザーで接続している場合。スーパーユーザー接続の環境でも害はありません |
| `none` | 遮断せず `pg_terminate_backend` のみ | アプリがスーパーユーザーで接続していて `revoke` が効かない場合。**フェーズB中にアプリ側サービスを停止する運用が前提** |

`datallowconn = false` は使用していません。スーパーユーザーを含む全接続を拒否してしまい、続く差分コピーで dblink が source へ接続できなくなるためです。

## 終了コード

| コード | 意味 |
|---|---|
| `0` | 正常完了 |
| `1` | 設定・検証エラー（接続情報不足、テーブル指定不正、確認拒否など） |
| `2` | フェーズA完了・フェーズB手動待ち（**異常ではありません**） |
| `10` | フェーズA失敗（source は無変更。`abort` で後始末可能） |
| `20` | フェーズB失敗（要手動対応） |

フェーズBの途中（B4 以降）で失敗した場合は自動ロールバックを行いません。source が退避DB名へリネーム済みの状態になるため、状況を判断できるよう復旧用のSQLをエラーメッセージに添えて出力します。

## 安全設計

- **既定は現状維持** — データを減らす操作（trim / drop）は明示指定でのみ発動します
- **`pg_dump` / `pg_restore` の終了コードを必ず検査** — ダンプ失敗時に後続のDBリネームへ進みません
- **`DROP TABLE` に `CASCADE` を付けない** — ビューや外部キーの依存があればエラーで中断します
- **SQL識別子は必ずクォート** — テーブル名・列名は `pgx.Identifier.Sanitize()`、dblink へ渡すSQL文字列は `quote_ident` / `quote_literal` 相当でエスケープします
- **DBレベルACLの引き継ぎ** — `pg_dump` に含まれないDBレベルACL・オーナー・DB設定は A0 で記録し B6 で再適用します

## 開発

```bash
go build ./...      # ビルド
go vet ./...        # 静的解析
go test ./...       # ユニットテスト
```

### 結合検証

実DB（Docker上の PostgreSQL）を使った回帰テスト一式が `test/integration/` にあります。

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o test/integration/pgwash-linux-arm64 ./cmd/pgwash
bash test/integration/run.sh
```

詳細は [`test/integration/README.md`](test/integration/README.md) を参照してください。

### 構成

| パッケージ | 責務 |
|---|---|
| `cmd/pgwash` | エントリポイント |
| `internal/cli` | cobra サブコマンド定義 |
| `internal/tui` | bubbletea による対話ウィザード |
| `internal/config` | フラグ・環境変数・既定値のマージと検証 |
| `internal/washover` | 洗い替えのオーケストレーション（UIから独立） |
| `internal/db` | PostgreSQL アクセスの詳細（pgx、SQL組み立て、外部プロセス実行） |

CLI と TUI はどちらも `washover.Engine` を呼び出す薄いラッパーで、DB操作の詳細を知りません。進捗は `Event` チャネル経由で通知されます。
