package service

import (
	"strings"
	"testing"
)

func TestSQLSummaryKeepsNoValues(t *testing.T) {
	cases := map[string]string{
		"UPDATE users SET email = 'anna@gmail.com' WHERE id = 5":                       "UPDATE on users",
		"insert into orders (note) values ('x; update secrets set y = 1')":              "INSERT into orders",
		"DELETE FROM sessions; DELETE FROM tokens":                                      "DELETE from sessions, DELETE from tokens",
		`update "line items" set qty = 2`:                                               "UPDATE on line items",
		"-- fix anna@gmail.com\nUPDATE users SET name = 'Anna' /* call 0170 123 */":   "UPDATE on users",
		"CREATE TABLE IF NOT EXISTS notes (id INTEGER)":                                 "CREATE table notes",
		"ALTER TABLE users ADD COLUMN phone TEXT":                                       "ALTER table users",
		"PRAGMA user_version = 3":                                                       "SQL",
		"DELETE FROM a; DELETE FROM b; DELETE FROM c; DELETE FROM d":                    "DELETE from a, DELETE from b, DELETE from c, more",
	}
	for sql, want := range cases {
		got := sqlSummary(sql)
		if got != want {
			t.Errorf("%q: got %q, want %q", sql, got, want)
		}
		for _, secret := range []string{"anna", "Anna", "0170", "secrets"} {
			if strings.Contains(got, secret) {
				t.Errorf("%q: %q keeps %q", sql, got, secret)
			}
		}
	}
}
