package cli

import (
	"cmp"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/protocol"
)

// backupDownload is `moin backup download [app]`: one copy of a database
// comes to your laptop. The server sends it as it lies in the bucket,
// encrypted, over SSH. Your backup key opens it here, and the plain
// database lands in a file: a SQLite file, or the dump as it lies in the
// bucket, like a Postgres dump or a gzipped MySQL dump. A server without
// a bucket sends a fresh copy.
func backupDownload(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("backup download", "backup download [app] [--to <copy>] [--database <name>] [-o <file>] [--key-file <file>] [-s server]").withServer()
	to := flags.String("to", "", "the copy, see `"+build.Name+" backups`; default the newest")
	name := flags.String("database", "", "the database, when the app has several")
	out := flags.String("o", "", "the file to write, - for stdout; default like my-app-<copy>.db")
	keyFile := flags.String("key-file", "", "a file with your backup key; else it is asked, or read from stdin")
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	prompts := terminal(os.Stderr)
	return onApp(ctx, flags, app, func(server string) error {
		databases, err := appBackups(ctx, server, app, os.Stderr)
		if err != nil {
			return err
		}
		list := []databaseBackups{}
		for _, database := range databases {
			list = append(list, databaseBackups{DatabaseBackup: database})
		}
		if *name == "" && len(list) > 1 && !isTerminal(stdin) {
			return fmt.Errorf("%s has several databases: name one with --database", app)
		}
		backup, err := pickDatabase(app, *name, list, stdin, prompts)
		if err != nil {
			return err
		}
		copy, err := pickDownload(app, backup.DatabaseBackup, *to)
		if err != nil {
			return err
		}
		key, err := downloadKey(*keyFile, stdin, prompts)
		if err != nil {
			return err
		}
		file := cmp.Or(*out, downloadName(app, backup.Database, len(databases) > 1, copy, backup.Kind))
		what := "the copy " + copy + " of " + backup.Database
		if copy == "" {
			what = "a fresh copy of " + backup.Database
		}
		var size int64
		err = task(prompts, "Downloading "+what, func() error {
			size, err = downloadCopy(ctx, server, app, backup.Database, copy, backup.Kind, key, file, stdout)
			return err
		})
		if err != nil || file == "-" {
			return err
		}
		done(stdout, "Wrote %s of %s to %s, %s", backup.Database, app, file, human.Size(float64(size)))
		return nil
	})
}

// pickDownload is the copy to download: to when the bucket has it, else
// the newest. "" is a fresh copy, for a database without one yet.
func pickDownload(app string, database protocol.DatabaseBackup, to string) (string, error) {
	if to == "" {
		if len(database.Copies) == 0 {
			return "", nil
		}
		return database.Copies[0].ID, nil
	}
	if slices.ContainsFunc(database.Copies, func(copy protocol.Copy) bool { return copy.ID == to }) {
		return to, nil
	}
	return "", fmt.Errorf("the bucket has no copy %s of %s. Only copies in the bucket download: see them with `%s backups %s`", to, database.Database, build.Name, app)
}

// downloadKey is your backup key: from file, or asked without showing
// it, or one line from a pipe.
func downloadKey(file string, stdin io.Reader, prompts io.Writer) ([]age.Identity, error) {
	if file == "" {
		key, err := askBackupKey(stdin, prompts, "Your backup key:")
		if err != nil {
			return nil, err
		}
		return []age.Identity{key}, nil
	}
	opened, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	keys, err := age.ParseIdentities(opened)
	if err != nil {
		return nil, fmt.Errorf("%s holds no backup key: one starts with AGE-SECRET-KEY-1", file)
	}
	return keys, nil
}

// downloadName is the file a copy lands in by default, like
// my-app-20261127T140000Z-482913.db. An app with several databases has
// the database in it too.
func downloadName(app, database string, several bool, copy, kind string) string {
	name := app
	if several {
		base := strings.TrimSuffix(path.Base(database), path.Ext(database))
		if kind != "" {
			base = strings.ReplaceAll(database, "/", "-")
		}
		name += "-" + base
	}
	if copy == "" {
		copy = time.Now().UTC().Format("20060102T150405Z")
	}
	return name + "-" + copy + downloadExtension(kind)
}

// downloadExtension is what a plain copy of kind is: the name it has in
// the bucket, but a SQLite file unzipped, like .db, .dump or .sql.gz.
func downloadExtension(kind string) string {
	if kind == "" {
		return ".db"
	}
	return strings.TrimPrefix(protocol.CopyFile("", kind, true), "copies/")
}

// downloadCopy fetches a copy over SSH, the way `bucket confirm` does,
// opens it with key and writes it to file: whole or not at all. "-" is
// stdout. It answers the bytes written.
func downloadCopy(ctx context.Context, server, app, database, copy, kind string, key []age.Identity, file string, stdout io.Writer) (int64, error) {
	reader, writer := io.Pipe()
	defer reader.Close()
	go func() {
		line := hereLine("bucket", "key", "send", app, "--database", database, "--copy", copy)
		writer.CloseWithError(sshRun(ctx, server, line, nil, writer, os.Stderr))
	}()
	if file == "-" {
		return writePlain(reader, kind, key, raw(stdout))
	}
	part := file + ".part"
	target, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer os.Remove(part)
	size, err := writePlain(reader, kind, key, target)
	if err := cmp.Or(err, target.Close()); err != nil {
		return 0, err
	}
	return size, os.Rename(part, file)
}

// writePlain decrypts a copy with key and writes what it holds to w: a
// SQLite copy unzipped, a dump as it is. age checks every chunk on the
// way, so a copy cut short or changed fails.
func writePlain(encrypted io.Reader, kind string, key []age.Identity, w io.Writer) (int64, error) {
	plain, err := age.Decrypt(encrypted, key...)
	var wrong *age.NoIdentityMatchError
	if errors.As(err, &wrong) {
		return 0, errors.New("your backup key does not open this copy: it was made for another key")
	}
	if err != nil {
		return 0, fmt.Errorf("the copy is not encrypted as it should be: %w", err)
	}
	if kind == "" {
		if plain, err = gzip.NewReader(plain); err != nil {
			return 0, fmt.Errorf("what it holds is not a database: %w", err)
		}
	}
	return io.Copy(w, plain)
}
