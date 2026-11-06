package db

import "testing"

func TestQuoteLiteral(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"public", "'public'"},
		{"o'brien", "'o''brien'"},
		{"", "''"},
	}
	for _, c := range cases {
		if got := quoteLiteral(c.in); got != c.want {
			t.Errorf("quoteLiteral(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
