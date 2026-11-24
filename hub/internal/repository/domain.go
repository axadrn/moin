package repository

import (
	"slices"
	"strings"

	"github.com/axadrn/moin/hub/internal/model"
)

// DomainUsedError means another app, or another service of the app,
// uses the domain already: a domain reaches one service in the whole hub.
type DomainUsedError struct {
	Name string
}

func (e *DomainUsedError) Error() string {
	return e.Name + " is already used by another app or service"
}

// Domains are the domains of an app, oldest first.
func (r *Apps) Domains(appID string) ([]model.Domain, error) {
	domains := []model.Domain{}
	err := r.db.Select(&domains, `SELECT * FROM domains WHERE app_id = $1 ORDER BY id`, appID)
	return domains, err
}

// SetDomains makes names the domains of one service of an app, "" for its
// main service, and canonical their main one, "" for none. A domain that
// stays keeps its row; a name in use elsewhere is a DomainUsedError.
func (r *Apps) SetDomains(appID, service string, names []string, canonical string) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	saved := []string{}
	err = tx.Select(&saved, `SELECT name FROM domains WHERE app_id = $1 AND service = $2`, appID, service)
	if err != nil {
		return err
	}
	for _, name := range saved {
		if slices.Contains(names, name) {
			continue
		}
		_, err = tx.Exec(`DELETE FROM domains WHERE name = $1`, name)
		if err != nil {
			return err
		}
	}
	for _, name := range names {
		if slices.Contains(saved, name) {
			continue
		}
		_, err = tx.Exec(`INSERT INTO domains (app_id, service, name) VALUES ($1, $2, $3)`, appID, service, name)
		if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: domains.name") {
			return &DomainUsedError{Name: name}
		}
		if err != nil {
			return err
		}
	}
	// Two steps: a service never has two main domains, not even for a
	// moment.
	_, err = tx.Exec(`UPDATE domains SET main = 0 WHERE app_id = $1 AND service = $2`, appID, service)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE domains SET main = 1 WHERE app_id = $1 AND service = $2 AND name = $3`, appID, service, canonical)
	if err != nil {
		return err
	}
	return tx.Commit()
}
