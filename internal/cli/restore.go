package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/protocol"
)

// Where the copies are: in the server's bucket, and in moin cloud for a
// paid workspace. After each scheduled copy the server sends the same
// copy there, encrypted with your backup key, where it can't delete it.
// `moin backups` lists them in one table, `moin restore` puts one back.
//
// A copy in moin cloud comes back like this: moin cloud gives this laptop
// an address to download it from, for a few minutes, and the laptop hands
// it with your backup key to the server over SSH. The data goes from moin
// cloud's bucket to the server, never through this laptop.

// inBucket is the WHERE of a copy in the server's bucket.
const inBucket = "bucket"

// storedCopy is one copy of a database, and where it is: "bucket" or
// "moin cloud".
type storedCopy struct {
	ID    string    `json:"id"`
	Where string    `json:"where"`
	At    time.Time `json:"at"`
	Size  int64     `json:"size"`
	Plain bool      `json:"plain,omitempty"`
	// Database is the database it is of; `moin backups` lists the copies
	// under it.
	Database string `json:"-"`
}

// databaseBackups is how one database is backed up, as `moin backups`
// shows it: what the server says, with the copies in the bucket and in
// moin cloud, newest first.
type databaseBackups struct {
	protocol.DatabaseBackup
	Copies []storedCopy `json:"copies"`
}

// inCloud are the copies of app that server sent to moin cloud; from
// names another server, like one that is gone. None when this laptop is
// not signed in; when moin cloud does not answer, it is said on said.
func inCloud(ctx context.Context, server, from, app string, said io.Writer) []storedCopy {
	session, err := loadCloudSession()
	if err != nil {
		return nil
	}
	var list []struct {
		ID       string    `json:"id"`
		Database string    `json:"database"`
		MadeAt   time.Time `json:"made_at"`
		Size     int64     `json:"size"`
	}
	query := url.Values{"server": {serverName(cmp.Or(from, server))}, "app": {app}}.Encode()
	if _, err := session.call(ctx, http.MethodGet, "/api/cloud/copies?"+query, nil, &list); err != nil {
		printError(said, err)
		return nil
	}
	copies := []storedCopy{}
	for _, copy := range list {
		copies = append(copies, storedCopy{ID: copy.ID, Where: cloudName, At: copy.MadeAt, Size: copy.Size, Database: copy.Database})
	}
	return copies
}

// listBackups is how the databases of app on server are backed up, with
// the copies in the bucket and in moin cloud. from names another server
// whose copies to list, like one that is gone.
func listBackups(ctx context.Context, server, app, from string, said io.Writer) ([]databaseBackups, error) {
	databases, err := backupsFrom(ctx, server, app, from, said)
	if err != nil {
		return nil, err
	}
	list := []databaseBackups{}
	for _, database := range databases {
		entry := databaseBackups{DatabaseBackup: database, Copies: []storedCopy{}}
		for _, copy := range database.Copies {
			entry.Copies = append(entry.Copies, storedCopy{ID: copy.ID, Where: inBucket, At: copy.At, Size: copy.Size, Plain: copy.Plain, Database: database.Database})
		}
		entry.DatabaseBackup.Copies = nil
		list = append(list, entry)
	}
	for _, copy := range inCloud(ctx, server, from, app, said) {
		index := slices.IndexFunc(list, func(entry databaseBackups) bool { return entry.Database == copy.Database })
		if index < 0 {
			list = append(list, databaseBackups{DatabaseBackup: protocol.DatabaseBackup{Database: copy.Database}})
			index = len(list) - 1
		}
		list[index].Copies = append(list[index].Copies, copy)
	}
	for _, entry := range list {
		slices.SortStableFunc(entry.Copies, func(a, b storedCopy) int { return b.At.Compare(a.At) })
	}
	return list, nil
}

// restore is `moin restore [app]`: a database goes back to a copy, in the
// bucket or in moin cloud, and what it was before stays as an undo. On a
// terminal it shows the copies and asks which one; a script names it with
// --to. What it asks goes to stderr, so stdout holds only the answer of
// --json.
//
// When a server is gone, its copies come back on a new one with your
// backup key: `moin restore shop -s new --from prod --to <copy> --key`.
// A copy in moin cloud always asks for your backup key.
func restore(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("restore", "restore [app] [--to <copy | 2026-11-27T14:07 | undo-…>] [--database <name>] [--from <server>] [--key] [-s server] [-y] [--json]").withServer().withYes().withJSON()
	to := flags.String("to", "", "a copy, a time in UTC, or an undo: see `"+build.Name+" backups`")
	database := flags.String("database", "", "the database, when the app has several")
	from := flags.String("from", "", "restore a copy another server made, like one that is gone")
	withKey := flags.Bool("key", false, "open the copy with your backup key, which "+build.Name+" asks for: for a copy another server made")
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	prompts := terminal(os.Stderr)
	return onApp(ctx, flags, app, func(server string) error {
		name, chosen, err := pickCopy(ctx, server, app, *database, *from, *to, stdin, prompts)
		if err != nil {
			return err
		}
		question := fmt.Sprintf("Put the data of %s back to %s?", app, chosen.ID)
		if chosen.Where == cloudName {
			question = fmt.Sprintf("Put the data of %s back to the copy %s in %s, taken %s?", app, chosen.ID, chosen.Where, chosen.At.UTC().Format("Jan 2 15:04 UTC"))
		}
		if err := confirm(stdin, prompts, flags.yes(), question+" The app stops for a moment."); err != nil {
			return err
		}
		var restored daemon.Restored
		switch {
		case chosen.Where == cloudName:
			key, err := askBackupKey(stdin, prompts, "Your backup key:")
			if err != nil {
				return err
			}
			restored, err = restoreFromCloud(ctx, server, app, name, chosen, key)
			if err != nil {
				return err
			}
		default:
			var key io.Reader
			if *withKey {
				secret, err := askBackupKey(stdin, prompts, "Your backup key:")
				if err != nil {
					return err
				}
				key = strings.NewReader(secret.String() + "\n")
			}
			restored, err = restoreCopy(ctx, server, app, name, chosen.ID, *from, key, os.Stderr)
			if err != nil {
				return err
			}
		}
		if flags.json() {
			return printJSON(stdout, restored)
		}
		done(stdout, "%s of %s is back at %s", restored.Database, app, restored.To)
		hint(stdout, "Undo it with", build.Name+" restore "+app+" --to "+restored.Undo)
		return nil
	})
}

// restoreCopy puts a database of app back to the copy to in the bucket,
// on server. from names the server that made the copy, "" for this one.
// A key goes to the server on stdin, never on a command line: the server
// opens the copy with it and writes it nowhere.
func restoreCopy(ctx context.Context, server, app, database, to, from string, key io.Reader, said io.Writer) (daemon.Restored, error) {
	line := []string{"restore", app, "--to", to, "--database", database, "--from", from}
	if key != nil {
		line = append(line, "--key-stdin")
	}
	var restored daemon.Restored
	err := sshAnswer(ctx, server, line, key, said, &restored)
	return restored, err
}

// restoreFromCloud puts a database of app back to a copy in moin cloud.
// moin cloud gives an address to download it from; the address
// and your key go to the server on stdin, never on a command line. The
// server uses the key for this restore only.
func restoreFromCloud(ctx context.Context, server, app, database string, chosen storedCopy, key *age.X25519Identity) (daemon.Restored, error) {
	session, err := loadCloudSession()
	if err != nil {
		return daemon.Restored{}, err
	}
	var download struct {
		URL string `json:"url"`
	}
	if _, err := session.call(ctx, http.MethodPost, "/api/cloud/copies/"+url.PathEscape(chosen.ID)+"/download", nil, &download); err != nil {
		return daemon.Restored{}, err
	}
	body, err := json.Marshal(daemon.CloudRestore{App: app, Database: database, Copy: chosen.ID, URL: download.URL, Key: key.String()})
	if err != nil {
		return daemon.Restored{}, err
	}
	var restored daemon.Restored
	err = sshJSON(ctx, server, []string{"cloud-restore"}, bytes.NewReader(body), &restored)
	return restored, err
}

// pickCopy is the database and the copy to go back to. A copy named to is
// looked for in moin cloud first; one it doesn't keep, a time or an undo
// is the server's. Without to it shows every copy of the app's
// database on a terminal and asks which one; Enter takes the newest. An
// app with several databases is asked which one first.
func pickCopy(ctx context.Context, server, app, name, from, to string, stdin io.Reader, stdout io.Writer) (database string, chosen storedCopy, err error) {
	if to != "" {
		for _, copy := range inCloud(ctx, server, from, app, os.Stderr) {
			if copy.ID == to && (name == "" || copy.Database == name) {
				return copy.Database, copy, nil
			}
		}
		return name, storedCopy{ID: to, Where: inBucket}, nil
	}
	if !isTerminal(stdin) {
		return "", storedCopy{}, fmt.Errorf("say what to go back to with --to. See the copies with `%s backups %s`", build.Name, app)
	}
	databases, err := listBackups(ctx, server, app, from, os.Stderr)
	if err != nil {
		return "", storedCopy{}, err
	}
	backup, err := pickDatabase(app, name, databases, stdin, stdout)
	if err != nil {
		return "", storedCopy{}, err
	}
	if len(backup.Copies) == 0 {
		return "", storedCopy{}, fmt.Errorf("%s of %s has no copy to go back to yet", backup.Database, app)
	}
	printBackup(stdout, backup, time.Now())
	id, err := ask(stdin, stdout, "Go back to which copy?", backup.Copies[0].ID)
	if err != nil {
		return "", storedCopy{}, err
	}
	for _, copy := range backup.Copies {
		if copy.ID == id {
			return backup.Database, copy, nil
		}
	}
	return backup.Database, storedCopy{ID: id, Where: inBucket}, nil
}

// pickDatabase is the database named name, the app's only one, or the
// one you pick.
func pickDatabase(app, name string, databases []databaseBackups, stdin io.Reader, stdout io.Writer) (databaseBackups, error) {
	if len(databases) == 0 {
		return databaseBackups{}, fmt.Errorf("%s has no database %s can copy", app, build.Name)
	}
	if name == "" && len(databases) > 1 {
		for _, database := range databases {
			fmt.Fprintln(stdout, "  "+database.Database)
		}
		var err error
		name, err = ask(stdin, stdout, "Which database?", databases[0].Database)
		if err != nil {
			return databaseBackups{}, err
		}
	}
	for _, database := range databases {
		if database.Database == name || name == "" {
			return database, nil
		}
	}
	return databaseBackups{}, fmt.Errorf("%s has no database %s", app, name)
}

// hereCloudRestore is `moin here cloud-restore` on the server: the
// restore from a copy in moin cloud, with the address and the key as JSON
// on stdin.
func hereCloudRestore(ctx context.Context, dir string, stdin io.Reader, stdout io.Writer) error {
	var restore daemon.CloudRestore
	if err := json.NewDecoder(io.LimitReader(stdin, 64<<10)).Decode(&restore); err != nil {
		return fmt.Errorf("here cloud-restore reads the restore as JSON on stdin: %w", err)
	}
	value, err := daemon.CloudRestoreHere(ctx, dir, restore, os.Stderr)
	return respond(stdout, value, err)
}
