package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ReadPrivileges は dbname のオーナー・DBレベルACL・DB個別設定を取得する（フェーズA A0、設計案 7.1節）。
func ReadPrivileges(ctx context.Context, conn *pgx.Conn, dbname string) (SourcePrivileges, error) {
	var p SourcePrivileges

	err := conn.QueryRow(ctx, `
		SELECT pg_get_userbyid(datdba), datacl IS NOT NULL
		FROM pg_database WHERE datname = $1
	`, dbname).Scan(&p.Owner, &p.HasACL)
	if err != nil {
		return p, fmt.Errorf("オーナー情報の取得に失敗しました (%s): %w", dbname, err)
	}

	if p.HasACL {
		rows, err := conn.Query(ctx, `
			SELECT
				CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END AS grantee,
				a.privilege_type,
				a.is_grantable
			FROM pg_database d, aclexplode(d.datacl) a
			WHERE d.datname = $1
		`, dbname)
		if err != nil {
			return p, fmt.Errorf("ACLの取得に失敗しました (%s): %w", dbname, err)
		}
		for rows.Next() {
			var e ACLEntry
			if err := rows.Scan(&e.Grantee, &e.PrivilegeType, &e.IsGrantable); err != nil {
				rows.Close()
				return p, fmt.Errorf("ACL行の読み取りに失敗しました (%s): %w", dbname, err)
			}
			p.ACL = append(p.ACL, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return p, fmt.Errorf("ACL取得中にエラーが発生しました (%s): %w", dbname, err)
		}
	}

	var setconfig []string
	err = conn.QueryRow(ctx, `
		SELECT s.setconfig
		FROM pg_db_role_setting s
		JOIN pg_database d ON d.oid = s.setdatabase
		WHERE d.datname = $1 AND s.setrole = 0
	`, dbname).Scan(&setconfig)
	if err != nil && err != pgx.ErrNoRows {
		return p, fmt.Errorf("DB設定の取得に失敗しました (%s): %w", dbname, err)
	}
	p.DBSettings = setconfig

	return p, nil
}

// ApplyPrivileges は記録した権限を dbname へ再適用する（フェーズB B7、設計案 6章B7）。
// p.HasACL が false の場合、新DBは既定状態のままでよいので ACL の再適用は行わない。
func ApplyPrivileges(ctx context.Context, conn *pgx.Conn, dbname string, p SourcePrivileges) error {
	dbIdent := pgx.Identifier{dbname}.Sanitize()

	if p.Owner != "" {
		sql := "ALTER DATABASE " + dbIdent + " OWNER TO " + pgx.Identifier{p.Owner}.Sanitize()
		if _, err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("オーナーの再適用に失敗しました (%s): %w", dbname, err)
		}
	}

	if p.HasACL {
		if _, err := conn.Exec(ctx, "REVOKE ALL ON DATABASE "+dbIdent+" FROM PUBLIC"); err != nil {
			return fmt.Errorf("ACL初期化に失敗しました (%s): %w", dbname, err)
		}
		for _, e := range p.ACL {
			grantee := "PUBLIC"
			if e.Grantee != "PUBLIC" {
				grantee = pgx.Identifier{e.Grantee}.Sanitize()
			}
			sql := fmt.Sprintf("GRANT %s ON DATABASE %s TO %s", e.PrivilegeType, dbIdent, grantee)
			if e.IsGrantable {
				sql += " WITH GRANT OPTION"
			}
			if _, err := conn.Exec(ctx, sql); err != nil {
				return fmt.Errorf("ACLの再適用に失敗しました (%s, grantee=%s, priv=%s): %w", dbname, e.Grantee, e.PrivilegeType, err)
			}
		}
	}

	for _, setting := range p.DBSettings {
		key, value, ok := strings.Cut(setting, "=")
		if !ok {
			return fmt.Errorf("DB設定の形式が不正です (%s): %q", dbname, setting)
		}
		sql := fmt.Sprintf("ALTER DATABASE %s SET %s TO %s",
			dbIdent, pgx.Identifier{key}.Sanitize(), quoteLiteral(value))
		if _, err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("DB設定の再適用に失敗しました (%s, %s): %w", dbname, setting, err)
		}
	}

	return nil
}

// quoteLiteral は SQL文字列リテラルとして安全な形にクォートする
// （ALTER DATABASE ... SET の値のように識別子でもプレースホルダでも表現できない箇所用）。
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
