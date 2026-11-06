package db

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// DSN は libpq のキーワード/値形式（"host=... port=... ..."）の接続文字列を返す。
// 値はシングルクォートで囲み、バックスラッシュとシングルクォートをエスケープする。
func (c ConnInfo) DSN() string {
	parts := []string{
		"host=" + dsnQuote(c.Host),
		"port=" + strconv.Itoa(c.Port),
		"user=" + dsnQuote(c.User),
		"dbname=" + dsnQuote(c.Database),
	}
	if c.Password != "" {
		parts = append(parts, "password="+dsnQuote(c.Password))
	}
	return strings.Join(parts, " ")
}

// dsnQuote は libpq キーワード/値形式の値をクォートする。
func dsnQuote(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `'`, `\'`)
	return "'" + v + "'"
}

// WithDatabase は接続先DBのみを差し替えたコピーを返す。
func (c ConnInfo) WithDatabase(name string) ConnInfo {
	c2 := c
	c2.Database = name
	return c2
}

// Connect は接続を確立する。呼び出し側が Close する。
func Connect(ctx context.Context, ci ConnInfo) (*pgx.Conn, error) {
	conn, err := pgx.Connect(ctx, ci.DSN())
	if err != nil {
		return nil, fmt.Errorf("データベースへの接続に失敗しました (%s): %w", ci.Database, err)
	}
	return conn, nil
}
