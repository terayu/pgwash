#!/usr/bin/env bash
# 共通関数。run.sh から source して使う。

CID="${PGWASH_TEST_CONTAINER:-pgwash-test}"
PGW_BIN="/work/pgwash-linux-arm64"
STATE_DIR="/work/state"
INTEG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

section() {
    echo
    echo "===== $* ====="
}

# dpsql <args...> : コンテナ内 psql をラップする（-U postgres固定）
dpsql() {
    docker exec "$CID" psql -U postgres "$@"
}

# dpsql_scalar <db> <sql> : -tA（ヘッダ・整形なし）で単一値を取得し前後の空白を除去する。
# クエリ失敗時は空文字列を返す（assert側で期待値と不一致になりFAILとして検出される）。
dpsql_scalar() {
    local db="$1" sql="$2"
    docker exec "$CID" psql -U postgres -d "$db" -tA -c "$sql" 2>/dev/null | tr -d '[:space:]'
}

# dpgw <args...> : コンテナ内 pgwash をヘッドレス実行する（PGPASSWORD経由）。
# phase-b/abort も含め、PGPASSWORD環境変数を読むバグ(#2)は修正済みのため
# 全サブコマンドをこの一本のラッパーで実行できる。
dpgw() {
    docker exec -e PGPASSWORD=postgres "$CID" "$PGW_BIN" "$@"
}

latest_run_id() {
    docker exec "$CID" sh -c "ls -t $STATE_DIR/*.json 2>/dev/null" | head -1 | xargs -n1 basename | sed 's/\.json$//'
}

# normalized_acl <db> : datacl を aclexplode() で展開し、grantee:privilege:is_grantable の
# 集合を辞書順に連結した文字列として返す。aclitem配列の並び順は環境によって変わりうるため
# （実測でも入れ替わりを確認）、比較前に必ずこの正規化形へ通すこと。
normalized_acl() {
    local db="$1"
    dpsql_scalar postgres "
        SELECT coalesce(string_agg(entry, ',' ORDER BY entry), '(empty)')
        FROM (
            SELECT (CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END)
                   || ':' || a.privilege_type || ':' || a.is_grantable AS entry
            FROM pg_database d, aclexplode(d.datacl) a
            WHERE d.datname = '$db'
        ) s;
    "
}

# ─────────────────────────────────────────────
# アサーション（回帰テストとして機能させるための照合機構）
# ─────────────────────────────────────────────
PASS_COUNT=0
FAIL_COUNT=0
FAILURES=()

# assert_eq <説明> <期待値> <実測値>
assert_eq() {
    local desc="$1" expected="$2" actual="$3"
    if [ "$expected" == "$actual" ]; then
        PASS_COUNT=$((PASS_COUNT + 1))
        echo "  PASS: $desc (期待=${expected} 実測=${actual})"
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
        FAILURES+=("$desc (期待=${expected} 実測=${actual})")
        echo "  FAIL: $desc 期待=${expected} 実測=${actual}"
    fi
}

print_summary_and_exit() {
    echo
    section "検証サマリ"
    local total=$((PASS_COUNT + FAIL_COUNT))
    if [ "$FAIL_COUNT" -eq 0 ]; then
        echo "PASS: 全 ${total} 項目"
        exit 0
    else
        echo "FAIL: ${total} 項目中 ${FAIL_COUNT} 件失敗"
        for f in "${FAILURES[@]}"; do
            echo "  - $f"
        done
        exit 1
    fi
}
