package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

// sqlCommand is `moin sql [app] "<query>"`: one query on a database of
// the app, the rows as a table. `moin sql [app] --tables` lists its
// tables. It reads only. With --write the statement may change the data:
// moin shows it, asks, takes a copy of the database, then runs it in one
// transaction and says how to undo it. What it asks goes to stderr, so
// stdout holds only the rows, or the answer of --json.
func sqlCommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("sql", `sql [app] "<query>" [--db <name>] [--limit 200] [--write] [-s server] [-y] [--json], or sql [app] --tables`).withServer().withYes().withJSON()
	tables := flags.Bool("tables", false, "list the tables of the database")
	database := flags.String("db", "", "the database, when the app has several")
	limit := flags.Int("limit", daemon.SQLLimit, "the most rows to show")
	write := flags.Bool("write", false, "let the statement change the data: "+build.Name+" asks first and takes a copy")
	noCopy := flags.Bool("no-copy", false, "with --write: no copy of the database before it")
	if err := flags.parse(args); err != nil {
		return err
	}
	others := 1
	if *tables {
		others = 0
	}
	if !*tables && flags.NArg() == 1 && checkApp(flags.Arg(0)) == nil {
		// One word is an app, not a query.
		return fmt.Errorf("say what to ask %s: `%s sql %s \"SELECT …\"`, or its tables with --tables", flags.Arg(0), build.Name, flags.Arg(0))
	}
	app, rest, err := appArgs(flags, others)
	if err != nil {
		return err
	}
	line := []string{"sql", app, "--limit", strconv.Itoa(*limit)}
	query := ""
	switch {
	case *tables:
		line = append(line, "--tables")
	case *write:
		query = rest[0]
		line = append(line, "--write")
		if *noCopy {
			line = append(line, "--no-copy")
		}
	default:
		query = rest[0]
	}
	prompts := terminal(os.Stderr)
	return onApp(ctx, flags, app, func(server string) error {
		if *write && !*tables {
			fmt.Fprintln(prompts, "  "+strings.ReplaceAll(strings.TrimSpace(query), "\n", "\n  "))
			question := fmt.Sprintf("Run this on %s? %s takes a copy of the database first.", app, build.Name)
			if *noCopy {
				question = fmt.Sprintf("Run this on %s, without a copy of the database first?", app)
			}
			if err := confirm(stdin, prompts, flags.yes(), question); err != nil {
				return err
			}
		}
		result, err := askSQL(ctx, server, line, *database, query)
		if err == nil && len(result.Choose) > 0 {
			// The app has several databases, and nothing ran yet.
			var chosen string
			if chosen, err = chooseDatabase(app, result.Choose, stdin, prompts); err != nil {
				return err
			}
			result, err = askSQL(ctx, server, line, chosen, query)
		}
		if err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, result)
		}
		if *write && !*tables {
			done(stdout, "%s changed in %s of %s", rowCount(result.Affected), result.Database, app)
			if result.Copy != "" {
				hint(stdout, "Undo it with", build.Name+" restore "+app+" --to "+daemon.BeforeSQL)
			}
			return nil
		}
		printRows(stdout, result)
		return nil
	})
}

// askSQL runs one `here sql` on the server, with the query on stdin: a
// query has quotes and line breaks no command line should carry.
func askSQL(ctx context.Context, server string, line []string, database, query string) (daemon.SQLResult, error) {
	if database != "" {
		line = append(line[:len(line):len(line)], "--db", database)
	}
	var result daemon.SQLResult
	err := sshAnswer(ctx, server, line, strings.NewReader(query), os.Stderr, &result)
	return result, err
}

// chooseDatabase asks which of the app's databases, on a terminal.
// Without one it names the flag.
func chooseDatabase(app string, databases []string, stdin io.Reader, stdout io.Writer) (string, error) {
	if !isTerminal(stdin) {
		return "", fmt.Errorf("%s has several databases: %s. Name one with --db, like --db %s", app, strings.Join(databases, ", "), databases[0])
	}
	return pick(stdin, stdout, "Which database?", databases, databases[0])
}

// cellWidth is how wide a value shows in the table. --json has all of it.
const cellWidth = 60

// printRows writes the rows of a query as a table: NULL muted, a long
// value cut, a line break in a value as a space.
func printRows(stdout io.Writer, result daemon.SQLResult) {
	if len(result.Rows) == 0 {
		fmt.Fprintln(stdout, style.Muted.Render("No rows."))
		return
	}
	rows := [][]string{}
	for _, row := range result.Rows {
		cells := make([]string, len(result.Columns))
		for index := range cells {
			switch {
			case index >= len(row):
			case row[index] == nil:
				cells[index] = style.Muted.Render("NULL")
			default:
				cells[index] = clip(strings.Join(strings.Fields(*row[index]), " "), cellWidth)
			}
		}
		rows = append(rows, cells)
	}
	table(stdout, result.Columns, rows)
	if result.More {
		fmt.Fprintln(stdout, style.Muted.Render("Only the first "+rowCount(int64(len(result.Rows)))+". More with --limit, all of every value with --json."))
	}
}

// rowCount is a number of rows in words: 1 row, 3 rows.
func rowCount(count int64) string {
	if count == 1 {
		return "1 row"
	}
	return strconv.FormatInt(count, 10) + " rows"
}

// hereSQL is `here sql <app>` on the server, with the query on stdin.
func hereSQL(ctx context.Context, dir string, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("here sql", "here sql <app> [--db <name>] [--limit 200] [--tables] [--write] [--no-copy]")
	request := daemon.SQL{}
	flags.StringVar(&request.Database, "db", "", "the database")
	flags.IntVar(&request.Limit, "limit", daemon.SQLLimit, "the most rows")
	flags.BoolVar(&request.Tables, "tables", false, "the tables")
	flags.BoolVar(&request.Write, "write", false, "may change the data")
	flags.BoolVar(&request.NoCopy, "no-copy", false, "no copy before a write")
	var err error
	if request.App, err = hereApp(flags, args); err != nil {
		return err
	}
	query, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return err
	}
	request.Query = string(query)
	value, err := daemon.SQLHere(ctx, dir, request, os.Stderr)
	return respond(stdout, value, err)
}
