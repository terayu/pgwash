package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ListTables は schema 配下の通常テーブル(BASE TABLE)を列挙する。
// pg_stat_user_tables は統計ビューのため使わず information_schema.tables を用いる。
// HasPtime（日時列 timeCol の有無）は washover の TableAction 決定に必須のため stats に関わらず常に判定する。
// stats が false なら EstRows/LastPtime/SizeBytes は取得しない（高速）。
// stats=true は timeCol にインデックスがないテーブルでは MAX(timeCol) がフルスキャンとなり高コストになりうる。
func ListTables(ctx context.Context, conn *pgx.Conn, schema, timeCol string, stats bool) ([]TableInfo, error) {
	rows, err := conn.Query(ctx, `
		SELECT t.table_name,
		       EXISTS (
		           SELECT 1 FROM information_schema.columns c
		           WHERE c.table_schema = t.table_schema
		             AND c.table_name = t.table_name
		             AND c.column_name = $2
		       ) AS has_ptime
		FROM information_schema.tables t
		WHERE t.table_schema = $1 AND t.table_type = 'BASE TABLE'
		ORDER BY t.table_name
	`, schema, timeCol)
	if err != nil {
		return nil, fmt.Errorf("テーブル一覧の取得に失敗しました (schema=%s): %w", schema, err)
	}

	var tables []TableInfo
	for rows.Next() {
		var ti TableInfo
		if err := rows.Scan(&ti.Name, &ti.HasPtime); err != nil {
			rows.Close()
			return nil, fmt.Errorf("テーブル一覧の読み取りに失敗しました (schema=%s): %w", schema, err)
		}
		tables = append(tables, ti)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("テーブル一覧の取得中にエラーが発生しました (schema=%s): %w", schema, err)
	}

	if !stats {
		return tables, nil
	}

	const statSQL = `
		SELECT c.reltuples::bigint, pg_total_relation_size($1::regclass)
		FROM pg_class c
		WHERE c.oid = $1::regclass
	`
	for i := range tables {
		qualified := pgx.Identifier{schema, tables[i].Name}.Sanitize()

		var estRows, sizeBytes int64
		if err := conn.QueryRow(ctx, statSQL, qualified).Scan(&estRows, &sizeBytes); err != nil {
			return nil, fmt.Errorf("テーブル統計の取得に失敗しました (%s.%s): %w", schema, tables[i].Name, err)
		}
		tables[i].EstRows = estRows
		tables[i].SizeBytes = sizeBytes

		if tables[i].HasPtime {
			t, err := MaxPtime(ctx, conn, schema, tables[i].Name, timeCol)
			if err != nil {
				return nil, err
			}
			tables[i].LastPtime = t
		}
	}

	return tables, nil
}

// ColumnDefs は dblink の結果列定義に使う "col1 type1, col2 type2, ..." を返す。
// 列名は pgx.Identifier でクォートする（設計案 4.5節）。
//
// information_schema.columns.data_type ではなく pg_attribute + format_type() を使う。
// data_type は配列型で "ARRAY"、ユーザー定義型（enum/複合型）で "USER-DEFINED" という
// 汎用値を返し、dblink の AS t(...) 列定義としてそのまま使うと構文エラーになるため。
// NOT a.attisdropped の条件も必須（削除済み列を拾うと列数がずれる）。
func ColumnDefs(ctx context.Context, conn *pgx.Conn, schema, table string) (string, error) {
	rows, err := conn.Query(ctx, `
		SELECT a.attname, format_type(a.atttypid, a.atttypmod)
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
		  AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum
	`, schema, table)
	if err != nil {
		return "", fmt.Errorf("列定義の取得に失敗しました (%s.%s): %w", schema, table, err)
	}
	defer rows.Close()

	var defs []string
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return "", fmt.Errorf("列定義の読み取りに失敗しました (%s.%s): %w", schema, table, err)
		}
		defs = append(defs, pgx.Identifier{name}.Sanitize()+" "+dataType)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("列定義の取得中にエラーが発生しました (%s.%s): %w", schema, table, err)
	}
	if len(defs) == 0 {
		return "", fmt.Errorf("テーブルの列が見つかりません: %s.%s", schema, table)
	}

	return strings.Join(defs, ", "), nil
}
