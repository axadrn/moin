package daemon

import "testing"

// TestClickhouseError: an error keeps its number and its name and loses
// what quotes the app's data.
func TestClickhouseError(t *testing.T) {
	for _, test := range []struct{ stderr, want string }{
		{"Received exception from server (version 24.12.6):\nCode: 643. DB::Exception: Received from localhost:9000. DB::Exception: Couldn't open zip archive 'bad.zip'. (CANNOT_UNPACK_ARCHIVE)\n(query: RESTORE ALL FROM File('bad.zip'))\n", "ClickHouse error 643 (CANNOT_UNPACK_ARCHIVE)"},
		{"Code: 608. DB::Exception: Received from localhost:9000. DB::Exception: Cannot restore the table shop.`ada@example.com` because it already contains some data. (CANNOT_RESTORE_TABLE)\n", "ClickHouse error 608 (CANNOT_RESTORE_TABLE)"},
		{"Code: 516. DB::Exception: Received from localhost:9000. DB::Exception: app: Authentication failed: password is incorrect, or there is no user with such name.. (AUTHENTICATION_FAILED)\n", "ClickHouse did not let moin in: moin logs in with the CLICKHOUSE_USER and CLICKHOUSE_PASSWORD of the service"},
		{"sh: clickhouse-client: not found\n", ""},
	} {
		if got := clickhouseError(test.stderr); got != test.want {
			t.Errorf("clickhouseError(%q) = %q, want %q", test.stderr, got, test.want)
		}
	}
	if got := clickhouseQuoted("a`b\\c"); got != "`a\\`b\\\\c`" {
		t.Errorf("clickhouseQuoted = %s", got)
	}
}
