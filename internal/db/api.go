// Package db は PostgreSQL へのアクセスを集約する。
// SQL文字列の組み立て、pgx 経由の実行、pg_dump/pg_restore の外部プロセス実行を担い、
// 洗い替えの手順そのもの（オーケストレーション）は washover パッケージが持つ。
package db

import (
	"time"
)

// ConnInfo は接続先の指定。
type ConnInfo struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// TableInfo は1テーブルのメタ情報。
// LastPtime は「使用しなくなったテーブル」の判別に使う（設計案 5.5節）。
type TableInfo struct {
	Name      string
	HasPtime  bool
	EstRows   int64      // pg_class.reltuples による推定行数
	LastPtime *time.Time // MAX(ptime)。ptime列なし/空テーブルなら nil
	SizeBytes int64      // pg_total_relation_size()
}

// ACLEntry は aclexplode() の1行に対応する。
type ACLEntry struct {
	Grantee       string // "PUBLIC" または ロール名
	PrivilegeType string // CONNECT / TEMPORARY / CREATE 等
	IsGrantable   bool
}

// SourcePrivileges は source DB の権限情報。
// pg_dump はDBレベルのGRANTを出力しないため、フェーズAで記録しフェーズBで再適用する。
type SourcePrivileges struct {
	Owner      string     `json:"owner"`
	HasACL     bool       `json:"has_acl"` // datacl IS NOT NULL。false なら再適用不要
	ACL        []ACLEntry `json:"acl"`
	DBSettings []string   `json:"db_settings"` // pg_db_role_setting.setconfig
}
