package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/human"
)

// The copies in moin cloud: after each scheduled copy a server of a paid
// workspace sends the same copy there, encrypted with your key, where it
// can't delete it. `moin cloud copies prod` lists them. `moin cloud
// restore shop` puts one back: moin cloud gives this laptop an address to
// download it from, for five minutes, and the laptop hands it with your
// backup key to the server over SSH. The data goes from moin cloud's
// bucket to the server, never through this laptop.

// cloudCopy is one copy in moin cloud.
type cloudCopy struct {
	ID       string    `json:"id"`
	Server   string    `json:"server"`
	App      string    `json:"app"`
	Database string    `json:"database"`
	MadeAt   time.Time `json:"made_at"`
	Size     int64     `json:"size"`
}

// cloudCopiesOf are the copies in moin cloud, newest first: of one server
// and one app, or of all when they are "".
func cloudCopiesOf(ctx context.Context, session cloudSession, server, app string) ([]cloudCopy, error) {
	var list []cloudCopy
	query := url.Values{"server": {server}, "app": {app}}.Encode()
	_, err := cloudCall(ctx, http.MethodGet, "/api/cloud/copies?"+query, session.Token, nil, &list)
	return list, err
}

// cloudCopies is `moin cloud copies <server> [app]`.
func cloudCopies(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("cloud copies", "cloud copies <server> [app] [--json]").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 || flags.NArg() > 2 {
		return flags.wrong()
	}
	server, app := serverName(flags.Arg(0)), flags.Arg(1)
	session, err := loadCloudSession()
	if err != nil {
		return err
	}
	list, err := cloudCopiesOf(ctx, session, server, app)
	if err != nil {
		return err
	}
	if flags.json() {
		return printJSON(stdout, list)
	}
	if len(list) == 0 {
		fmt.Fprintf(stdout, "No copies of %s in %s cloud yet. With a paid plan, each scheduled copy goes there too.\n", server, build.Name)
		return nil
	}
	printCloudCopies(stdout, list)
	hint(stdout, "Put one back with", build.Name+" cloud restore <app> --to <copy>")
	return nil
}

// printCloudCopies shows copies as a table.
func printCloudCopies(stdout io.Writer, list []cloudCopy) {
	rows := [][]string{}
	now := time.Now()
	for _, copy := range list {
		rows = append(rows, []string{copy.ID, copy.App, copy.Database, copy.Server, human.Ago(copy.MadeAt, now), human.Size(float64(copy.Size))})
	}
	table(stdout, []string{"COPY", "APP", "DATABASE", "FROM", "TAKEN", "SIZE"}, rows)
}

// cloudRestore is `moin cloud restore [app]`: a database of the app goes
// back to a copy in moin cloud, from any server of the workspace, like
// one that is gone. On a terminal it shows the copies and asks which one;
// a script names it with --to. Your backup key opens it.
func cloudRestore(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("cloud restore", "cloud restore [app] [--to <copy>] [--database <name>] [-s server] [-y] [--json]").withServer().withYes().withJSON()
	to := flags.String("to", "", "the copy: see `"+build.Name+" cloud copies <server>`")
	database := flags.String("database", "", "only the copies of this database")
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	session, err := loadCloudSession()
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		chosen, err := pickCloudCopy(ctx, session, app, *database, *to, stdin, os.Stderr)
		if err != nil {
			return err
		}
		question := fmt.Sprintf("Put %s of %s on %s back to the copy %s from %s, taken %s? The app stops for a moment.",
			chosen.Database, app, server, chosen.ID, chosen.Server, chosen.MadeAt.UTC().Format("Jan 2 15:04 UTC"))
		if err := confirm(stdin, os.Stderr, flags.yes(), question); err != nil {
			return err
		}
		key, err := askBackupKey(stdin, os.Stderr, "Your backup key:")
		if err != nil {
			return err
		}
		var download struct {
			URL string `json:"url"`
		}
		if _, err := cloudCall(ctx, http.MethodPost, "/api/cloud/copies/"+url.PathEscape(chosen.ID)+"/download", session.Token, nil, &download); err != nil {
			return err
		}
		// The address and your key go to the server on stdin, never on a
		// command line. The server uses the key for this restore only.
		body, err := json.Marshal(daemon.CloudRestore{App: app, Database: chosen.Database, Copy: chosen.ID, URL: download.URL, Key: key.String()})
		if err != nil {
			return err
		}
		var restored daemon.Restored
		if err := sshJSON(ctx, server, []string{"cloud-restore"}, bytes.NewReader(body), &restored); err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, restored)
		}
		done(stdout, "%s of %s is back at the copy %s from %s cloud", restored.Database, app, chosen.ID, build.Name)
		hint(stdout, "Undo it with", build.Name+" restore "+app+" --to "+restored.Undo)
		return nil
	})
}

// pickCloudCopy is the copy of app named to, or the one you pick on a
// terminal. Enter takes the newest.
func pickCloudCopy(ctx context.Context, session cloudSession, app, database, to string, stdin io.Reader, stdout io.Writer) (cloudCopy, error) {
	all, err := cloudCopiesOf(ctx, session, "", app)
	if err != nil {
		return cloudCopy{}, err
	}
	list := []cloudCopy{}
	for _, copy := range all {
		if database == "" || copy.Database == database {
			list = append(list, copy)
		}
	}
	if len(list) == 0 {
		return cloudCopy{}, fmt.Errorf("%s cloud has no copy of %s yet", build.Name, app)
	}
	if to == "" {
		if !isTerminal(stdin) {
			return cloudCopy{}, fmt.Errorf("say which copy with --to. See them with `%s cloud copies <server> %s`", build.Name, app)
		}
		printCloudCopies(stdout, list)
		if to, err = ask(stdin, stdout, "Go back to which copy?", list[0].ID); err != nil {
			return cloudCopy{}, err
		}
	}
	for _, copy := range list {
		if copy.ID == to {
			return copy, nil
		}
	}
	return cloudCopy{}, fmt.Errorf("%s cloud has no copy %s of %s. See them with `%s cloud copies <server> %s`", build.Name, to, app, build.Name, app)
}

// hereCloudRestore is `moin here cloud-restore` on the server: the
// restore, with the address and the key as JSON on stdin.
func hereCloudRestore(ctx context.Context, dir string, stdin io.Reader, stdout io.Writer) error {
	var restore daemon.CloudRestore
	if err := json.NewDecoder(io.LimitReader(stdin, 64<<10)).Decode(&restore); err != nil {
		return fmt.Errorf("here cloud-restore reads the restore as JSON on stdin: %w", err)
	}
	value, err := daemon.CloudRestoreHere(ctx, dir, restore, os.Stderr)
	return respond(stdout, value, err)
}
