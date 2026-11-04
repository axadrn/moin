package repository

import (
	"time"

	"github.com/axadrn/ops/controlplane/internal/model"
)

// MetricsRepository stores the metrics of servers and apps: a row per
// minute and one per hour. The control plane keeps the samples in memory and
// writes minutes in one batch a minute, to history.db.
type MetricsRepository interface {
	// AddMetricsMinutes writes minutes in one transaction; a minute that is
	// there already takes the new values.
	AddMetricsMinutes(rows []model.MetricsRow) error
	// RollUpMetrics averages the minutes from from to to into their hours.
	RollUpMetrics(from, to int64) error
	MetricsMinutes(target string, from, to int64) ([]model.MetricsRow, error)
	MetricsHours(target string, from, to int64) ([]model.MetricsRow, error)
	// PruneMetrics forgets the minutes before minutesBefore and the hours
	// before hoursBefore.
	PruneMetrics(minutesBefore, hoursBefore int64) error
	// MetricsApps are all apps, to find an app by its server and name.
	MetricsApps() ([]model.MetricsApp, error)
	// LiveDeploys are the deploys of an app that went live since a time,
	// oldest first.
	LiveDeploys(appID string, since time.Time) ([]model.MetricsDeploy, error)
}

func (r *appRepository) AddMetricsMinutes(rows []model.MetricsRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := r.history.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	statement, err := tx.Preparex(tx.Rebind(`
		INSERT OR REPLACE INTO metrics_minutes (target, minute, cpu, memory, disk, net_rx, net_tx, load)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`))
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, row := range rows {
		_, err = statement.Exec(row.Target, row.At, row.CPU, row.Memory, row.Disk, row.NetRx, row.NetTx, row.Load)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *appRepository) RollUpMetrics(from, to int64) error {
	_, err := r.history.Exec(`
		INSERT OR REPLACE INTO metrics_hours (target, hour, cpu, memory, disk, net_rx, net_tx, load)
		SELECT target, minute - minute % 3600, CAST(AVG(cpu) AS INTEGER), CAST(AVG(memory) AS INTEGER),
			CAST(AVG(disk) AS INTEGER), CAST(AVG(net_rx) AS INTEGER), CAST(AVG(net_tx) AS INTEGER), CAST(AVG(load) AS INTEGER)
		FROM metrics_minutes WHERE minute >= $1 AND minute < $2
		GROUP BY target, minute - minute % 3600`, from, to)
	return err
}

func (r *appRepository) MetricsMinutes(target string, from, to int64) ([]model.MetricsRow, error) {
	rows := []model.MetricsRow{}
	err := r.history.Select(&rows, `
		SELECT target, minute AS at, cpu, memory, disk, net_rx, net_tx, load FROM metrics_minutes
		WHERE target = $1 AND minute >= $2 AND minute < $3 ORDER BY minute`, target, from, to)
	return rows, err
}

func (r *appRepository) MetricsHours(target string, from, to int64) ([]model.MetricsRow, error) {
	rows := []model.MetricsRow{}
	err := r.history.Select(&rows, `
		SELECT target, hour AS at, cpu, memory, disk, net_rx, net_tx, load FROM metrics_hours
		WHERE target = $1 AND hour >= $2 AND hour < $3 ORDER BY hour`, target, from, to)
	return rows, err
}

func (r *appRepository) PruneMetrics(minutesBefore, hoursBefore int64) error {
	_, err := r.history.Exec(`DELETE FROM metrics_minutes WHERE minute < $1`, minutesBefore)
	if err != nil {
		return err
	}
	_, err = r.history.Exec(`DELETE FROM metrics_hours WHERE hour < $1`, hoursBefore)
	return err
}

func (r *appRepository) MetricsApps() ([]model.MetricsApp, error) {
	apps := []model.MetricsApp{}
	err := r.db.Select(&apps, `SELECT id, server_id, name FROM apps`)
	return apps, err
}

func (r *appRepository) LiveDeploys(appID string, since time.Time) ([]model.MetricsDeploy, error) {
	deploys := []model.MetricsDeploy{}
	err := r.db.Select(&deploys, `
		SELECT number, finished_at FROM deploys
		WHERE app_id = $1 AND status = 'succeeded' AND finished_at IS NOT NULL AND finished_at >= $2
		ORDER BY finished_at`, appID, since.UTC())
	return deploys, err
}
