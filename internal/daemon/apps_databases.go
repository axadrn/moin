package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/axadrn/moin/protocol"
)

// noBackupText is what moin says about a database it cannot back up.
const noBackupText = "moin cannot back up this database"

// scanDatabases finds what a release keeps its data in. The first list is
// what moin backs up: every SQLite file on a mounted folder and every
// Postgres service. The second is what looks like a database but moin
// cannot back up.
func (a *apps) scanDatabases(ctx context.Context, composeFile string) ([]protocol.Database, []protocol.Database, error) {
	body, err := os.ReadFile(composeFile)
	if err != nil {
		return nil, nil, err
	}
	var config composeConfig
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, nil, err
	}
	databases, noBackup := []protocol.Database{}, []protocol.Database{}
	names := slices.Sorted(maps.Keys(config.Services))
	for _, name := range names {
		for _, mount := range config.Services[name].Volumes {
			folder := a.mountFolder(ctx, config, mount)
			if folder != "" {
				databases = addSQLiteFiles(databases, folder, mount.Target)
			}
		}
	}
	for _, name := range names {
		service := config.Services[name]
		if postgresImage(service.Image) {
			databases = append(databases, protocol.Database{Path: protocol.PostgresKey(name), Kind: protocol.DatabasePostgres, Service: name})
			continue
		}
		kind := a.otherDatabase(ctx, config, service)
		if kind != "" {
			noBackup = append(noBackup, protocol.Database{Path: kind + "-" + name, Kind: kind, Service: name})
		}
	}
	return databases, noBackup, nil
}

// mountFolder is the folder on the server behind a mount, "" when there
// is none yet: a named volume exists only after the first start.
func (a *apps) mountFolder(ctx context.Context, config composeConfig, mount composeMount) string {
	switch mount.Type {
	case "bind":
		return mount.Source
	case "volume":
		name := config.Volumes[mount.Source].Name
		if name == "" {
			return ""
		}
		folder, err := a.output(ctx, "docker", "volume", "inspect", "-f", "{{.Mountpoint}}", name)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(folder)
	}
	return ""
}

// addSQLiteFiles adds the SQLite files under folder, once each. target is
// where the app sees that folder.
func addSQLiteFiles(databases []protocol.Database, folder, target string) []protocol.Database {
	for _, file := range sqliteFiles(folder) {
		known := slices.ContainsFunc(databases, func(d protocol.Database) bool { return d.HostPath == file })
		if known {
			continue
		}
		relative, _ := filepath.Rel(folder, file)
		databases = append(databases, protocol.Database{Path: filepath.Join(target, relative), HostPath: file})
	}
	return databases
}

// otherDatabase names the kind of database a service runs when moin
// cannot back it up, "" when it runs none of those: a known image, or a
// Postgres data folder in a mount of an image moin does not know.
func (a *apps) otherDatabase(ctx context.Context, config composeConfig, service composeService) string {
	switch plainImage(service.Image) {
	case "mysql", "mariadb":
		return "mysql"
	case "mongo":
		return "mongo"
	}
	for _, mount := range service.Volumes {
		if hasPostgresData(a.mountFolder(ctx, config, mount)) {
			return "postgres"
		}
	}
	return ""
}

// hasPostgresData says a Postgres keeps its data in folder, at most two
// folders down: Postgres writes the file PG_VERSION there.
func hasPostgresData(folder string) bool {
	if folder == "" {
		return false
	}
	for _, pattern := range []string{"PG_VERSION", "*/PG_VERSION", "*/*/PG_VERSION"} {
		found, _ := filepath.Glob(filepath.Join(folder, pattern))
		if len(found) > 0 {
			return true
		}
	}
	return false
}

// plainImage is an image without its tag, digest and Docker Hub's prefixes.
func plainImage(image string) string {
	image, _, _ = strings.Cut(image, "@")
	// A colon before the last slash is a registry port, not a tag.
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		image = image[:lastColon]
	}
	image = strings.TrimPrefix(image, "docker.io/")
	image = strings.TrimPrefix(image, "index.docker.io/")
	return strings.TrimPrefix(image, "library/")
}

// postgresImage reports whether an image runs Postgres: the official one
// or a known image built on it, from Docker Hub, with any tag or digest.
func postgresImage(image string) bool {
	name := plainImage(image)
	switch name {
	case "postgres", "postgis/postgis", "pgvector/pgvector":
		return true
	}
	return strings.HasPrefix(name, "timescale/timescaledb")
}

// sqliteFiles returns the files under dir, three levels deep, that are
// SQLite databases: the magic string and a valid page size in the 100 byte
// header. What moin leaves next to a database does not count (backupFile).
func sqliteFiles(dir string) []string {
	files := []string{}
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if backupFile(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if relative, _ := filepath.Rel(dir, path); strings.Count(relative, string(filepath.Separator)) >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() && isSQLite(path) {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func isSQLite(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	header := make([]byte, 100)
	_, err = io.ReadFull(file, header)
	if err != nil || !bytes.Equal(header[:16], []byte("SQLite format 3\x00")) {
		return false
	}
	// The page size is a power of two from 512 to 32768, or 1 for 65536.
	size := int(header[16])<<8 | int(header[17])
	return size == 1 || (size >= 512 && size <= 32768 && size&(size-1) == 0)
}

// liveDatabases are the databases of an app's live deploy.
func (a *apps) liveDatabases(app string) []protocol.Database {
	detected, _ := a.live(app)
	return detected.Databases
}

// findNewDatabases looks again in an app's live deploy: an app may create
// its SQLite file long after it started. It only adds: a folder that is
// gone for a moment must not end the backups of what was in it. It says
// whether something changed.
func (a *apps) findNewDatabases(ctx context.Context, app string) (protocol.Detected, bool) {
	detected, file := a.live(app)
	if file == "" {
		return detected, false
	}
	databases, noBackup, err := a.scanDatabases(ctx, a.composeFile(app))
	if err != nil {
		return detected, false
	}
	changed := !slices.Equal(detected.NoBackup, noBackup)
	detected.NoBackup = noBackup
	for _, database := range databases {
		if !slices.Contains(detected.Databases, database) {
			detected.Databases = append(detected.Databases, database)
			changed = true
		}
	}
	if changed {
		if err := writeState(file, detected); err != nil {
			return detected, false
		}
	}
	return detected, changed
}
