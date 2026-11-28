package protocol

// Detected is what the daemon learned about an app from its repo and its
// running containers.
type Detected struct {
	// Kind is "compose", "dockerfile", "go" or "railpack": what the repo
	// brought, and for railpack that moin built it with Railpack. Builder
	// says what Railpack found, like "Node 24.22.1, start: npm start".
	// Image is the image of the main service when it came from CI.
	// Domains are the domains the main service answers on; Instant its
	// instant address, "" when it has none.
	Kind      string     `json:"kind"`
	Builder   string     `json:"builder,omitempty"`
	Image     string     `json:"image,omitempty"`
	Domains   []string   `json:"domains,omitempty"`
	Instant   string     `json:"instant,omitempty"`
	Service   string     `json:"service"`
	Port      int        `json:"port"`
	Databases []Database `json:"databases"`
	// NoBackup is what looks like a database but moin cannot back up.
	NoBackup []Database `json:"no_backup,omitempty"`
}

// Database is a database of an app: a SQLite file the daemon found on one
// of the app's mounts, or, with Kind "postgres", a compose service that runs
// Postgres. Path names a SQLite file in the container; for Postgres it is
// postgres-<service>, and HostPath is empty. Older daemons send no Kind.
// Name is a database of a Postgres service besides the one POSTGRES_DB
// names, which backups copy too; its Path is postgres-<service>/<name>.
type Database struct {
	Path     string `json:"path"`
	HostPath string `json:"host_path"`
	// HostFolder is the folder of the server HostPath was found in: the
	// mount of the app. The daemon touches the file only through it, so a
	// link the app leaves there never leads out of it.
	HostFolder string `json:"host_folder,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Service    string `json:"service,omitempty"`
	Name       string `json:"name,omitempty"`
}

// DatabasePostgres is the Kind of a Postgres service.
const DatabasePostgres = "postgres"

// Postgres reports whether the database is a Postgres service.
func (d Database) Postgres() bool { return d.Kind == DatabasePostgres }

// PostgresKey is the name of a Postgres service's database in moin: in the
// bucket, the API and on every surface.
func PostgresKey(service string) string { return "postgres-" + service }
