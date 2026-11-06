package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnsureDblink は CREATE EXTENSION IF NOT EXISTS dblink を実行する。
func EnsureDblink(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS dblink"); err != nil {
		return fmt.Errorf("dblink拡張の有効化に失敗しました: %w", err)
	}
	return nil
}

// CopyAll は src から schema.table の全行を dblink 経由でコピーし、投入行数を返す。
// conn は投入先（一時DB）への接続、srcDSN は読み出し元DBの接続文字列。
// source側のSELECT文は PostgreSQL側の format('%I.%I', ...) で組み立て、
// テーブル名・スキーマ名を安全にエスケープする（設計案 4.3節）。
func CopyAll(ctx context.Context, conn *pgx.Conn, srcDSN, schema, table, colDefs string) (int64, error) {
	qualified := pgx.Identifier{schema, table}.Sanitize()
	sql := fmt.Sprintf(
		`INSERT INTO %s
		 SELECT * FROM dblink($1, format('SELECT * FROM %%I.%%I', $2::text, $3::text)) AS t(%s)`,
		qualified, colDefs,
	)
	tag, err := conn.Exec(ctx, sql, srcDSN, schema, table)
	if err != nil {
		return 0, fmt.Errorf("全件コピーに失敗しました (%s.%s): %w", schema, table, err)
	}
	return tag.RowsAffected(), nil
}

// CopyFrom は timeCol >= boundary の行のみを dblink 経由でコピーし、投入行数を返す（設計案 4.4節）。
// timeCol は日時列名（既定 ptime）。source側で実行するSQLには format の %I でクォートして埋め込む。
//
// boundary は 日時列の実際の型（timestamp / timestamptz のいずれか未確認）に依存しない形で渡す。
// RFC3339（オフセット付き）文字列にして %L でリテラル化し、source側の比較時に
// PostgreSQL が日時列の実型へ自動解決する。Go側で ::timestamp 等に固定キャストすると、
// timestamptz の場合はセッションのタイムゾーンで変換されて境界がずれる可能性があるため避ける。
func CopyFrom(ctx context.Context, conn *pgx.Conn, srcDSN, schema, table, colDefs, timeCol string, boundary time.Time) (int64, error) {
	qualified := pgx.Identifier{schema, table}.Sanitize()
	sql := fmt.Sprintf(
		`INSERT INTO %s
		 SELECT * FROM dblink($1, format('SELECT * FROM %%I.%%I WHERE %%I >= %%L', $2::text, $3::text, $4::text, $5::text)) AS t(%s)`,
		qualified, colDefs,
	)
	tag, err := conn.Exec(ctx, sql, srcDSN, schema, table, timeCol, boundary.Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("差分コピーに失敗しました (%s.%s): %w", schema, table, err)
	}
	return tag.RowsAffected(), nil
}

// MaxPtime は schema.table の MAX(timeCol) を返す。行がなければ nil。
// 呼び出し側は nil のとき境界値を別途決める必要がある（設計案 6章 B4）。
func MaxPtime(ctx context.Context, conn *pgx.Conn, schema, table, timeCol string) (*time.Time, error) {
	qualified := pgx.Identifier{schema, table}.Sanitize()
	sql := "SELECT MAX(" + pgx.Identifier{timeCol}.Sanitize() + ") FROM " + qualified

	var t *time.Time
	if err := conn.QueryRow(ctx, sql).Scan(&t); err != nil {
		return nil, fmt.Errorf("MAX(%s)の取得に失敗しました (%s.%s): %w", timeCol, schema, table, err)
	}
	return t, nil
}

// DeleteAtPtime は timeCol が t と完全一致する行を削除する（差分コピーの重複防止）。
func DeleteAtPtime(ctx context.Context, conn *pgx.Conn, schema, table, timeCol string, t time.Time) error {
	qualified := pgx.Identifier{schema, table}.Sanitize()
	sql := "DELETE FROM " + qualified + " WHERE " + pgx.Identifier{timeCol}.Sanitize() + " = $1"
	if _, err := conn.Exec(ctx, sql, t); err != nil {
		return fmt.Errorf("境界行（%s）の削除に失敗しました (%s.%s): %w", timeCol, schema, table, err)
	}
	return nil
}

// Truncate は schema.table を空にする。
func Truncate(ctx context.Context, conn *pgx.Conn, schema, table string) error {
	qualified := pgx.Identifier{schema, table}.Sanitize()
	if _, err := conn.Exec(ctx, "TRUNCATE "+qualified); err != nil {
		return fmt.Errorf("TRUNCATEに失敗しました (%s.%s): %w", schema, table, err)
	}
	return nil
}

// DropTable は schema.table を削除する。
// CASCADE は使わない。依存オブジェクトがあればエラーを返して中断させる（設計案 5.5節）。
func DropTable(ctx context.Context, conn *pgx.Conn, schema, table string) error {
	qualified := pgx.Identifier{schema, table}.Sanitize()
	if _, err := conn.Exec(ctx, "DROP TABLE "+qualified); err != nil {
		return fmt.Errorf("テーブルの削除に失敗しました (%s.%s): %w", schema, table, err)
	}
	return nil
}
