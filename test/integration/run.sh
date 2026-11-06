#!/usr/bin/env bash
# pgwash 結合検証ドライバ（実DB使用・回帰テスト）。
#
# 前提: Docker が起動しており、postgres:9.3 (linux/amd64) が pull 済みであること。
# 実行方法:
#   cd pgwash
#   CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o test/integration/pgwash-linux-arm64 ./cmd/pgwash
#   bash test/integration/run.sh
#
# 全項目が assert_eq による期待値照合になっており、1件でも不一致があれば
# 非ゼロで終了する（詳細は lib.sh 参照）。標準出力を目視確認する必要はない。
set -uo pipefail

# ja_JP.UTF-8 等のロケール下では bash の変数名解釈(ctypeベース)が日本語punctuationの
# 一部バイトを識別子文字と誤認し、"$VAR。" のような並びで変数名を巻き込んでしまうことがある
# （実際にこのスクリプトで遭遇した）。bash自身の字句解析をロケール非依存にしておく。
export LC_ALL=C

cd "$(dirname "${BASH_SOURCE[0]}")"
source ./lib.sh

CUTOFF="2026-08-01T00:00:00+00:00"

section "0. コンテナ起動"
docker rm -f "$CID" >/dev/null 2>&1
docker run --platform linux/amd64 -d --name "$CID" --user postgres \
    -e POSTGRES_PASSWORD=postgres -p 15432:5432 \
    -v "$INTEG_DIR:/work" \
    postgres:9.3 >/dev/null
echo "waiting for postgres to accept connections..."
# pg_isready は initdb 中の一時起動(検証用に1回起動→停止する)にも反応してしまうため、
# 実クエリが安定して通ることを条件にする（+ 念のため追加の安定待ち）。
for i in $(seq 1 60); do
    docker exec "$CID" psql -U postgres -d template1 -c "SELECT 1" >/dev/null 2>&1 && break
    sleep 2
done
sleep 3
docker exec "$CID" psql -U postgres -d template1 -c "SELECT 1" >/dev/null 2>&1 || {
    echo "postgres起動待ちがタイムアウトしました" >&2
    docker logs "$CID" 2>&1 | tail -30
    exit 1
}
docker exec "$CID" psql -U postgres -d template1 -c "CREATE EXTENSION IF NOT EXISTS dblink;" >/dev/null
mkdir -p "$INTEG_DIR/state" && chmod 777 "$INTEG_DIR/state"
rm -f "$INTEG_DIR"/state/*.json "$INTEG_DIR"/state/*.dump 2>/dev/null

section "1. シードデータ投入"
dpsql -f /work/seed.sql

# ─────────────────────────────────────────────
# 期待値の定義（seed.sql の内容と CUTOFF から導出。導出根拠をコメントで明示する）
# ─────────────────────────────────────────────
#
# [ベースライン] seed.sql より:
#   master_a=5 (ptimeなし) / trend_a=7 (前3・境界=cutoff 2・後2) / trend_b=5
#   old_kiki=3 / trend_null=4(全行NULL) / trend_empty=3(全行cutoff未満)
#
# [フェーズA] --tables=trend_a,trend_null,trend_empty --cutoff=$CUTOFF --drop-tables=old_kiki
#   master_a : CopyAll                      → 5
#   trend_a  : Trim (ptime>=cutoff)         → 境界2 + 後2 = 4
#   trend_b  : CopyAll(非選択)              → 5
#   trend_empty: Trim                       → 0 (cutoff以降がフェーズA時点で0件。MAX(ptime)がNULLになる)
#   trend_null : Trim                       → 0 (全行NULLは >= cutoff で常に除外)
#   old_kiki : Drop                         → 新DBに存在しない
EXP_PHASEA_MASTER_A=5
EXP_PHASEA_TREND_A=4
EXP_PHASEA_TREND_B=5
EXP_PHASEA_TREND_EMPTY=0
EXP_PHASEA_TREND_NULL=0

# [ギャップ挿入] フェーズA完了後、source(testdb)へ以下を追加する
#   trend_a  : +2 (2026-08-04, 2026-08-05)  → source合計 9
#   trend_b  : +1 (2026-08-07)              → source合計 6
#   master_a : +2                            → source合計 7
#   trend_empty: +2 (2026-08-02, 2026-08-03) → source合計 5 （いずれもcutoff以降）
#   trend_null : +2 (NULL)                   → source合計 6
#   old_kiki : 変更なし                       → 3
#
# [フェーズB差分キャッチアップ後の新DB]（最重要の検証対象）
#   master_a   : TRUNCATE+全件再コピー → 現在のsource全件 = 7
#   trend_a    : 境界(MAX(ptime)=2026-08-03)を削除して >= で再取得
#                → (フェーズA分4 - 境界1) + (境界1+新規2) = 6。重複・欠落なし
#   trend_b    : 境界(MAX(ptime)=2026-08-06)を削除して >= で再取得
#                → (5-1) + (1+1) = 6
#   trend_empty: フェーズA時点でMAX(ptime)がNULL → Trimなのでcutoffを境界に採用
#                → ptime>=cutoffなソース行(新規2件)を取得 = 2
#                  ★MAX(ptime) IS NULL 分岐が機能しているかを検証する最重要項目
#   trend_null : MAX(ptime)がNULL → cutoff境界を採用するが、NULLは>=cutoffで常に除外
#                → 0のまま（重複が起きていないことも合わせて確認）
#   old_kiki   : 新DBに存在しない
EXP_NEW_MASTER_A=7
EXP_NEW_TREND_A=6
EXP_NEW_TREND_B=6
EXP_NEW_TREND_EMPTY=2
EXP_NEW_TREND_NULL=0

section "1a. ベースライン行数・ACLの記録"
dpsql -d testdb -c "
SELECT 'trend_a' t, count(*) FROM trend_a UNION ALL
SELECT 'trend_b', count(*) FROM trend_b UNION ALL
SELECT 'master_a', count(*) FROM master_a UNION ALL
SELECT 'old_kiki', count(*) FROM old_kiki UNION ALL
SELECT 'trend_null', count(*) FROM trend_null UNION ALL
SELECT 'trend_empty', count(*) FROM trend_empty ORDER BY 1;"
BASELINE_ACL=$(normalized_acl testdb)
echo "BASELINE_ACL=$BASELINE_ACL"

section "2. フェーズA実行（--phase-b=manual, --drop-tables=old_kiki）"
dpgw run --host=localhost --port=5432 --user=postgres --dbname=testdb \
    --tables=trend_a,trend_null,trend_empty --cutoff="$CUTOFF" \
    --drop-tables=old_kiki --phase-b=manual --block-mode=revoke \
    --state-dir="$STATE_DIR" --yes
RC_PHASEA=$?
assert_eq "run(--phase-b=manual)の終了コード" "2" "$RC_PHASEA"

RUNID=$(latest_run_id)
echo "RUNID=$RUNID"
TEMPDB="testdb_new_${RUNID}"
BAKDB="testdb_bak_${RUNID}"

section "2a. フェーズA結果の照合（一時DB ${TEMPDB} 。リネーム前なのでこの時点のみ一時DB名で見る）"
assert_eq "フェーズA: master_a" "$EXP_PHASEA_MASTER_A" "$(dpsql_scalar "$TEMPDB" "SELECT count(*) FROM master_a;")"
assert_eq "フェーズA: trend_a"  "$EXP_PHASEA_TREND_A"  "$(dpsql_scalar "$TEMPDB" "SELECT count(*) FROM trend_a;")"
assert_eq "フェーズA: trend_b"  "$EXP_PHASEA_TREND_B"  "$(dpsql_scalar "$TEMPDB" "SELECT count(*) FROM trend_b;")"
assert_eq "フェーズA: trend_empty" "$EXP_PHASEA_TREND_EMPTY" "$(dpsql_scalar "$TEMPDB" "SELECT count(*) FROM trend_empty;")"
assert_eq "フェーズA: trend_null"  "$EXP_PHASEA_TREND_NULL"  "$(dpsql_scalar "$TEMPDB" "SELECT count(*) FROM trend_null;")"
# 注意: EXISTS(...)::text はPostgreSQLでは "t"/"f" ではなく "true"/"false" になる
# （"t"/"f" はpsqlのbool列表示時の整形であり、::textキャストの結果とは異なる）。
assert_eq "フェーズA: trend_aへのappuser GRANTがpg_restoreで引き継がれている" "true" \
    "$(dpsql_scalar "$TEMPDB" "SELECT EXISTS(SELECT 1 FROM information_schema.role_table_grants WHERE table_name='trend_a' AND grantee='appuser' AND privilege_type='SELECT')::text;")"

section "3. source(testdb) にギャップ行を追加（フェーズA完了後〜フェーズB前を模す）"
dpsql -d testdb -c "
INSERT INTO trend_a (id, ptime, val) VALUES (8, '2026-08-04 00:00:00', 3.1), (9, '2026-08-05 00:00:00', 3.2);
INSERT INTO trend_b (id, ptime, val) VALUES (6, '2026-08-07 00:00:00', 9.6);
INSERT INTO master_a (id, name) VALUES (6, 'kiki-F'), (7, 'kiki-G');
INSERT INTO trend_empty (id, ptime, val) VALUES (4, '2026-08-02 00:00:00', 7.4), (5, '2026-08-03 00:00:00', 7.5);
INSERT INTO trend_null (id, ptime, val) VALUES (5, NULL, 5.5), (6, NULL, 5.6);
"
section "3a. フェーズB直前のsource行数を記録（＝退避DBに残るべき件数の期待値）"
EXP_BAK_MASTER_A=$(dpsql_scalar testdb "SELECT count(*) FROM master_a;")
EXP_BAK_TREND_A=$(dpsql_scalar testdb "SELECT count(*) FROM trend_a;")
EXP_BAK_TREND_B=$(dpsql_scalar testdb "SELECT count(*) FROM trend_b;")
EXP_BAK_OLD_KIKI=$(dpsql_scalar testdb "SELECT count(*) FROM old_kiki;")
EXP_BAK_TREND_NULL=$(dpsql_scalar testdb "SELECT count(*) FROM trend_null;")
EXP_BAK_TREND_EMPTY=$(dpsql_scalar testdb "SELECT count(*) FROM trend_empty;")
echo "master_a=$EXP_BAK_MASTER_A trend_a=$EXP_BAK_TREND_A trend_b=$EXP_BAK_TREND_B old_kiki=$EXP_BAK_OLD_KIKI trend_null=$EXP_BAK_TREND_NULL trend_empty=$EXP_BAK_TREND_EMPTY"

section "4. フェーズB実行（最重要: 差分キャッチアップ + block-mode=revoke）"
dpgw phase-b --run-id="$RUNID" --state-dir="$STATE_DIR" --yes
RC_PHASEB=$?
assert_eq "phase-bの終了コード" "0" "$RC_PHASEB"

# フェーズBが成功していれば $TEMPDB は "testdb" にリネーム済みなので、
# 以降の検証はリネーム後の新 testdb に対して行う（一時DB名でのクエリは
# リネーム後は存在しないDBを指すことになり、検証が空振りする）。
DB="testdb"

section "4a. 新DB(${DB})の差分反映後の行数照合（最重要）"
assert_eq "新DB: master_a（TRUNCATE+全件再コピー）" "$EXP_NEW_MASTER_A" "$(dpsql_scalar "$DB" "SELECT count(*) FROM master_a;")"
assert_eq "新DB: trend_a（境界削除+差分再取得）"     "$EXP_NEW_TREND_A"  "$(dpsql_scalar "$DB" "SELECT count(*) FROM trend_a;")"
assert_eq "新DB: trend_b（ActionCopyAllだがptimeありなので差分方式）" "$EXP_NEW_TREND_B" "$(dpsql_scalar "$DB" "SELECT count(*) FROM trend_b;")"
assert_eq "新DB: trend_empty（MAX(ptime) IS NULL→Cutoffフォールバック）" "$EXP_NEW_TREND_EMPTY" "$(dpsql_scalar "$DB" "SELECT count(*) FROM trend_empty;")"
assert_eq "新DB: trend_null（全行NULLは恒久的に0件のまま。重複もしない）" "$EXP_NEW_TREND_NULL" "$(dpsql_scalar "$DB" "SELECT count(*) FROM trend_null;")"

section "4b. 重複チェック（id の total件数 と distinct件数 が一致するか）"
assert_eq "trend_a: idの重複なし(total=distinct)" \
    "$(dpsql_scalar "$DB" "SELECT count(*) FROM trend_a;")" \
    "$(dpsql_scalar "$DB" "SELECT count(DISTINCT id) FROM trend_a;")"
assert_eq "trend_b: idの重複なし(total=distinct)" \
    "$(dpsql_scalar "$DB" "SELECT count(*) FROM trend_b;")" \
    "$(dpsql_scalar "$DB" "SELECT count(DISTINCT id) FROM trend_b;")"

section "4c. old_kikiがDropされ新DBに存在しないこと"
# 注意: to_regclass() は PostgreSQL 9.4 以降の関数のため 9.3 では使えない。
# information_schema.tables で存在確認する（9.3互換）。
assert_eq "新DBにold_kikiが存在しない" "false" \
    "$(dpsql_scalar "$DB" "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='old_kiki')::text;")"

section "5. 退避DB(${BAKDB})の中身照合（元データが全部残っているか）"
assert_eq "退避DB: master_a"    "$EXP_BAK_MASTER_A"    "$(dpsql_scalar "$BAKDB" "SELECT count(*) FROM master_a;")"
assert_eq "退避DB: trend_a"     "$EXP_BAK_TREND_A"     "$(dpsql_scalar "$BAKDB" "SELECT count(*) FROM trend_a;")"
assert_eq "退避DB: trend_b"     "$EXP_BAK_TREND_B"     "$(dpsql_scalar "$BAKDB" "SELECT count(*) FROM trend_b;")"
assert_eq "退避DB: old_kiki"    "$EXP_BAK_OLD_KIKI"    "$(dpsql_scalar "$BAKDB" "SELECT count(*) FROM old_kiki;")"
assert_eq "退避DB: trend_null"  "$EXP_BAK_TREND_NULL"  "$(dpsql_scalar "$BAKDB" "SELECT count(*) FROM trend_null;")"
assert_eq "退避DB: trend_empty" "$EXP_BAK_TREND_EMPTY" "$(dpsql_scalar "$BAKDB" "SELECT count(*) FROM trend_empty;")"

section "6. ACL引き継ぎの照合（B6/B7）"
NEWDB_ACL=$(normalized_acl "$DB")
BAKDB_ACL=$(normalized_acl "$BAKDB")
assert_eq "新DBのdatacl(正規化)が元sourceと一致" "$BASELINE_ACL" "$NEWDB_ACL"
assert_eq "退避DBのdatacl(正規化)が元sourceと一致（B7で復旧済みのはず）" "$BASELINE_ACL" "$BAKDB_ACL"

echo "--- appuser で新DBへ接続 ---"
docker exec -e PGPASSWORD=appuser "$CID" psql -h localhost -U appuser -d "$DB" -c "SELECT 1;" >/dev/null 2>&1
assert_eq "appuserが新DBへ接続できる" "0" "$?"

echo "--- appuser で退避DBへ接続（B7でACLが復旧されているため接続できるのが正しい挙動） ---"
docker exec -e PGPASSWORD=appuser "$CID" psql -h localhost -U appuser -d "$BAKDB" -c "SELECT 1;" >/dev/null 2>&1
assert_eq "appuserが退避DBへ接続できる(B7復旧済み)" "0" "$?"

section "7. シナリオE: abort"
dpgw run --host=localhost --port=5432 --user=postgres --dbname=testdb \
    --phase-b=manual --state-dir="$STATE_DIR" --yes >/dev/null 2>&1
ABORT_RUNID=$(latest_run_id)
echo "ABORT_RUNID=$ABORT_RUNID"
dpgw abort --run-id="$ABORT_RUNID" --state-dir="$STATE_DIR" --yes
assert_eq "abortの終了コード" "0" "$?"
dpsql -c "\l" | grep -q "testdb_new_${ABORT_RUNID}"
assert_eq "abort後、一時DBが削除されている" "1" "$?"

section "8. シナリオE: 存在しないテーブルを --tables に指定"
# 現在の実装は「存在しないテーブルが指定されています」で検証エラーとしてフェーズAを
# 即座に失敗させる（ExitPhaseAFailed=10）。この検証はテーブル一覧取得の直後・一時DB
# 作成より前に行われるため一時DBは作られず、abortでの後始末は不要。
dpgw run --host=localhost --port=5432 --user=postgres --dbname=testdb \
    --tables=no_such_table --cutoff="$CUTOFF" \
    --phase-b=manual --state-dir="$STATE_DIR" --yes
assert_eq "存在しないテーブルを--tablesに指定するとフェーズA失敗になる" "10" "$?"

section "9. シナリオE: --yes なし"
docker exec -e PGPASSWORD=postgres "$CID" "$PGW_BIN" run \
    --host=localhost --port=5432 --user=postgres --dbname=testdb \
    --phase-b=auto --state-dir="$STATE_DIR"
assert_eq "--yesなしでの終了コード" "1" "$?"

# 除外時間 "HH:MM-HH:MM" を、コンテナ内の現在時刻（pgwash が判定に使う時計）から
# 前後 offset 分ずらして組み立てる。window_from_now <開始オフセット分> <終了オフセット分>
window_from_now() {
    local now_min
    now_min=$(docker exec "$CID" sh -c 'echo $(( $(date +%-H) * 60 + $(date +%-M) ))')
    local s=$(( (now_min + $1 + 1440) % 1440 )) e=$(( (now_min + $2 + 1440) % 1440 ))
    printf '%02d:%02d-%02d:%02d' $((s / 60)) $((s % 60)) $((e / 60)) $((e % 60))
}

section "10. シナリオF: --phase-b=auto で完了時刻が除外時間内 → 待機し、中断後に phase-b で再開"
EXCL_IN=$(window_from_now -60 60)
echo "除外時間(現在時刻を含む)=$EXCL_IN"
WAIT_LOG="$INTEG_DIR/state/wait_run.log"
dpgw run --host=localhost --port=5432 --user=postgres --dbname=testdb \
    --phase-b=auto --phase-b-exclude="$EXCL_IN" --state-dir="$STATE_DIR" --yes >"$WAIT_LOG" 2>&1 &
WAIT_PID=$!
# フェーズAが終わって待機に入るまで待つ（最大120秒）
WAITING=1
for i in $(seq 1 60); do
    grep -q "除外時間のため開始を待機します" "$WAIT_LOG" && { WAITING=0; break; }
    kill -0 "$WAIT_PID" 2>/dev/null || break
    sleep 2
done
assert_eq "除外時間内のためフェーズBの開始を待機している" "0" "$WAITING"
WAIT_RUNID=$(latest_run_id)
echo "WAIT_RUNID=$WAIT_RUNID"
assert_eq "待機中の状態はフェーズA完了のまま" "phase_a_complete" \
    "$(docker exec "$CID" sh -c "grep -o '\"phase\": \"[a-z_]*\"' $STATE_DIR/${WAIT_RUNID}.json" | sed 's/.*: "\(.*\)"/\1/')"
# 待機中にSIGTERM（運用での Ctrl+C / サービス停止を模す）
docker exec "$CID" sh -c 'kill -TERM $(pidof pgwash-linux-arm64)'
wait "$WAIT_PID"
assert_eq "待機中に中断したときの終了コード(フェーズB手動待ちと同じ)" "2" "$?"
assert_eq "中断時点でsourceはリネームされていない(退避DBなし)" "0" \
    "$(dpsql_scalar template1 "SELECT count(*) FROM pg_database WHERE datname='testdb_bak_${WAIT_RUNID}';")"
assert_eq "中断時点で一時DBが残っている" "1" \
    "$(dpsql_scalar template1 "SELECT count(*) FROM pg_database WHERE datname='testdb_new_${WAIT_RUNID}';")"
dpgw phase-b --run-id="$WAIT_RUNID" --state-dir="$STATE_DIR" --yes >/dev/null 2>&1
assert_eq "中断後の phase-b による再開の終了コード" "0" "$?"
assert_eq "再開後、sourceが退避DBへリネームされている" "1" \
    "$(dpsql_scalar template1 "SELECT count(*) FROM pg_database WHERE datname='testdb_bak_${WAIT_RUNID}';")"

section "11. シナリオF: --phase-b=auto で完了時刻が除外時間外 → 待たずにフェーズBまで完了"
EXCL_OUT=$(window_from_now 120 180)
echo "除外時間(現在時刻を含まない)=$EXCL_OUT"
NOWAIT_LOG="$INTEG_DIR/state/nowait_run.log"
dpgw run --host=localhost --port=5432 --user=postgres --dbname=testdb \
    --phase-b=auto --phase-b-exclude="$EXCL_OUT" --state-dir="$STATE_DIR" --yes >"$NOWAIT_LOG" 2>&1
assert_eq "除外時間外での run(--phase-b=auto) の終了コード" "0" "$?"
NOWAIT_RUNID=$(latest_run_id)
grep -q "除外時間のため開始を待機します" "$NOWAIT_LOG"
assert_eq "除外時間外では待機しない" "1" "$?"
assert_eq "フェーズBまで完了し、sourceが退避DBへリネームされている" "1" \
    "$(dpsql_scalar template1 "SELECT count(*) FROM pg_database WHERE datname='testdb_bak_${NOWAIT_RUNID}';")"

section "12. シナリオF: --phase-b=auto で除外時間を抜けるまで実際に待ち、自動でフェーズBを開始する（1〜2分かかる）"
# 終了時刻を「2分後の分境界」にする（1分後だと、フェーズAが長引いた場合に待機前に時間帯を抜けうるため）。
# 待ち時間は秒の位置により 60〜120秒になる。
END_EPOCH=$(docker exec "$CID" sh -c 'echo $(( ($(date +%s) / 60 + 2) * 60 ))')
EXCL_END=$(docker exec "$CID" date -d "@$END_EPOCH" +%H:%M)
EXCL_START=$(docker exec "$CID" date -d "@$((END_EPOCH - 180 * 60))" +%H:%M)
echo "除外時間=${EXCL_START}-${EXCL_END}（終了 epoch=$END_EPOCH）"
REAL_LOG="$INTEG_DIR/state/realwait_run.log"
dpgw run --host=localhost --port=5432 --user=postgres --dbname=testdb \
    --phase-b=auto --phase-b-exclude="${EXCL_START}-${EXCL_END}" --state-dir="$STATE_DIR" --yes >"$REAL_LOG" 2>&1
assert_eq "除外時間を抜けた後の run(--phase-b=auto) の終了コード" "0" "$?"
REAL_RUNID=$(latest_run_id)
grep -q "除外時間のため開始を待機します" "$REAL_LOG"
assert_eq "除外時間内のため待機に入った" "0" "$?"
# state_epoch <JSONキー> : 状態ファイルの時刻項目を epoch 秒にする
state_epoch() {
    docker exec "$CID" sh -c "date -d \"\$(grep -o '\"$1\": \"[^\"]*\"' $STATE_DIR/${REAL_RUNID}.json | cut -d'\"' -f4)\" +%s"
}
PA_DONE=$(state_epoch phase_a_completed_at)
PB_START=$(state_epoch phase_b_started_at)
echo "phase_a_completed_at=$PA_DONE phase_b_started_at=$PB_START"
assert_eq "フェーズAは除外時間の終了前に完了している" "1" "$(( PA_DONE < END_EPOCH ))"
assert_eq "フェーズBは除外時間の終了時刻以降に開始している" "1" "$(( PB_START >= END_EPOCH ))"
assert_eq "フェーズBは除外時間の終了後すぐ(10秒以内)に開始している" "1" "$(( PB_START - END_EPOCH <= 10 ))"
assert_eq "フェーズBまで完了し、sourceが退避DBへリネームされている" "1" \
    "$(dpsql_scalar template1 "SELECT count(*) FROM pg_database WHERE datname='testdb_bak_${REAL_RUNID}';")"

echo
echo "(コンテナは検証後に 'docker rm -f $CID' で削除すること。このスクリプトは自動では消さない)"

print_summary_and_exit
