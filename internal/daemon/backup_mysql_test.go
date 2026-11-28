package daemon

import "testing"

func TestMySQLDatabase(t *testing.T) {
	for _, test := range []struct {
		env  []string
		want string
	}{
		{[]string{"MYSQL_ROOT_PASSWORD=secret", "MYSQL_DATABASE=shop"}, "shop"},
		{[]string{"MARIADB_DATABASE=wiki", "MYSQL_DATABASE=shop"}, "wiki"},
		{[]string{"MYSQL_ROOT_PASSWORD=secret"}, ""},
	} {
		if got := mysqlDatabase(test.env); got != test.want {
			t.Errorf("mysqlDatabase(%v) = %q, want %q", test.env, got, test.want)
		}
	}
}

// TestMySQLError: an error keeps its number and loses what quotes the
// app's data.
func TestMySQLError(t *testing.T) {
	for _, test := range []struct{ stderr, want string }{
		{"ERROR 1062 (23000) at line 41: Duplicate entry 'ada@example.com' for key 'users.email'\n", "ERROR 1062 (23000) at line 41"},
		{"mysqldump: [Warning] something\nmysqldump: Got error: 1049: Unknown database 'shop' when selecting the database\n", "mysqldump: Got error: 1049"},
		{"mariadb-dump: Error 2013: Lost connection to server during query when dumping table `notes` at row: 3\n", "mariadb-dump: Error 2013"},
		{"ERROR 1045 (28000): Access denied for user 'root'@'localhost' (using password: YES)\n", "MySQL did not let root in: moin logs in with the MYSQL_ROOT_PASSWORD or MARIADB_ROOT_PASSWORD of the service"},
		{"sh: 1: mysql: not found\n", ""},
	} {
		if got := mysqlError(test.stderr); got != test.want {
			t.Errorf("mysqlError(%q) = %q, want %q", test.stderr, got, test.want)
		}
	}
}
