package model

import "time"

// MetricsRow is one minute or one hour of a server or an app, as stored:
// At is the Unix second it starts, CPU in tenths of a percent, Memory and
// Disk in bytes, NetRx and NetTx in bytes per second, Load in hundredths.
type MetricsRow struct {
	Target string `db:"target"`
	At     int64  `db:"at"`
	CPU    int64  `db:"cpu"`
	Memory int64  `db:"memory"`
	Disk   int64  `db:"disk"`
	NetRx  int64  `db:"net_rx"`
	NetTx  int64  `db:"net_tx"`
	Load   int64  `db:"load"`
}

// MetricsApp names an app the way a daemon does: by its server and folder.
type MetricsApp struct {
	ID       string `db:"id"`
	ServerID string `db:"server_id"`
	Folder   string `db:"folder"`
}

// MetricsDeploy is a deploy that went live, for the marks on the charts.
type MetricsDeploy struct {
	Number     int       `db:"number"`
	FinishedAt time.Time `db:"finished_at"`
}
