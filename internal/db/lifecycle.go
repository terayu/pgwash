package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateDatabase は CREATE DATABASE <name> OWNER <owner> を実行する。
// トランザクションブロック内では実行できない点に注意。
func CreateDatabase(ctx context.Context, conn *pgx.Conn, name, owner string) error {
	sql := fmt.Sprintf("CREATE DATABASE %s OWNER %s",
		pgx.Identifier{name}.Sanitize(), pgx.Identifier{owner}.Sanitize())
	if _, err := conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("データベースの作成に失敗しました (%s): %w", name, err)
	}
	return nil
}

// DropDatabase は DROP DATABASE <name> を実行する（abort による一時DBの後始末用）。
// トランザクションブロック内では実行できない点に注意。
func DropDatabase(ctx context.Context, conn *pgx.Conn, name string) error {
	sql := "DROP DATABASE " + pgx.Identifier{name}.Sanitize()
	if _, err := conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("データベースの削除に失敗しました (%s): %w", name, err)
	}
	return nil
}

// RenameDatabase は ALTER DATABASE <from> RENAME TO <to> を実行する。
// トランザクションブロック内では実行できない点に注意。
func RenameDatabase(ctx context.Context, conn *pgx.Conn, from, to string) error {
	sql := fmt.Sprintf("ALTER DATABASE %s RENAME TO %s",
		pgx.Identifier{from}.Sanitize(), pgx.Identifier{to}.Sanitize())
	if _, err := conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("データベースのリネームに失敗しました (%s -> %s): %w", from, to, err)
	}
	return nil
}

// RevokeConnect は PUBLIC および roles から CONNECT 権限を剥奪する。
// スーパーユーザーは権限チェックをバイパスするため dblink 接続は影響を受けない。
func RevokeConnect(ctx context.Context, conn *pgx.Conn, dbname string, roles []string) error {
	dbIdent := pgx.Identifier{dbname}.Sanitize()

	sql := fmt.Sprintf("REVOKE CONNECT ON DATABASE %s FROM PUBLIC", dbIdent)
	if _, err := conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("PUBLICからのCONNECT権限剥奪に失敗しました (%s): %w", dbname, err)
	}

	for _, role := range roles {
		sql := fmt.Sprintf("REVOKE CONNECT ON DATABASE %s FROM %s", dbIdent, pgx.Identifier{role}.Sanitize())
		if _, err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("ロールからのCONNECT権限剥奪に失敗しました (%s, role=%s): %w", dbname, role, err)
		}
	}
	return nil
}

// GrantConnectPublic は PUBLIC への CONNECT を復旧する（失敗時のロールバック、退避DBの復旧用）。
func GrantConnectPublic(ctx context.Context, conn *pgx.Conn, dbname string) error {
	sql := "GRANT CONNECT ON DATABASE " + pgx.Identifier{dbname}.Sanitize() + " TO PUBLIC"
	if _, err := conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("PUBLICへのCONNECT権限復旧に失敗しました (%s): %w", dbname, err)
	}
	return nil
}

// TerminateConnections は dbname への自分以外の接続を切断し、切断数を返す。
func TerminateConnections(ctx context.Context, conn *pgx.Conn, dbname string) (int, error) {
	rows, err := conn.Query(ctx, `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()
	`, dbname)
	if err != nil {
		return 0, fmt.Errorf("接続の切断に失敗しました (%s): %w", dbname, err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var ok bool
		if err := rows.Scan(&ok); err != nil {
			return count, fmt.Errorf("切断結果の読み取りに失敗しました (%s): %w", dbname, err)
		}
		if ok {
			count++
		}
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("切断結果の取得中にエラーが発生しました (%s): %w", dbname, err)
	}
	return count, nil
}
