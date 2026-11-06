package db

import "testing"

func TestConnInfoDSN(t *testing.T) {
	ci := ConnInfo{Host: "localhost", Port: 5432, User: "postgres", Password: "p@ss", Database: "joywatcher"}
	got := ci.DSN()
	want := "host='localhost' port=5432 user='postgres' dbname='joywatcher' password='p@ss'"
	if got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestConnInfoDSNNoPassword(t *testing.T) {
	ci := ConnInfo{Host: "localhost", Port: 5432, User: "postgres", Database: "joywatcher"}
	got := ci.DSN()
	want := "host='localhost' port=5432 user='postgres' dbname='joywatcher'"
	if got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestConnInfoDSNEscaping(t *testing.T) {
	ci := ConnInfo{Host: "localhost", Port: 5432, User: "postgres", Password: `p'\ss`, Database: "joywatcher"}
	got := ci.DSN()
	want := `host='localhost' port=5432 user='postgres' dbname='joywatcher' password='p\'\\ss'`
	if got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestConnInfoWithDatabase(t *testing.T) {
	ci := ConnInfo{Host: "localhost", Port: 5432, User: "postgres", Password: "secret", Database: "joywatcher"}
	got := ci.WithDatabase("joywatcher_new")

	if got.Database != "joywatcher_new" {
		t.Errorf("WithDatabase().Database = %q, want %q", got.Database, "joywatcher_new")
	}
	if got.Host != ci.Host || got.Port != ci.Port || got.User != ci.User || got.Password != ci.Password {
		t.Errorf("WithDatabase() changed fields other than Database: got %+v, original %+v", got, ci)
	}
	// 元の値が書き換わっていないこと（値コピーであること）を確認する。
	if ci.Database != "joywatcher" {
		t.Errorf("元の ConnInfo.Database が書き換わっている: %q", ci.Database)
	}
}
