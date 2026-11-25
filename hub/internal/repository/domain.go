package repository

import (
	"slices"
	"strings"

	"github.com/axadrn/moin/hub/internal/model"
)

// DomainUsedError means another service of the app uses the domain
// already: a domain reaches one service of an app. Other apps may use it
// too; then whoever set it twice sorts it out.
type DomainUsedError struct {
	Name string
}

func (e *DomainUsedError) Error() string {
	return e.Name + " is already used by another service of this app"
}

// Domains are the domains of an app, oldest first.
func (r *Apps) Domains(appID string) ([]model.Domain, error) {
	domains := []model.Domain{}
	err := r.db.Select(&domains, `SELECT * FROM domains WHERE app_id = $1 ORDER BY id`, appID)
	return domains, err
}

// SetDomains makes names the domains of one service of an app, "" for its
// main service, and canonical their main one, "" for none. A domain that
// stays keeps its row; a name another service of the app uses is a
// DomainUsedError.
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
		_, err = tx.Exec(`DELETE FROM domains WHERE app_id = $1 AND name = $2`, appID, name)
		if err != nil {
			return err
		}
	}
	for _, name := range names {
		if slices.Contains(saved, name) {
			continue
		}
		_, err = tx.Exec(`INSERT INTO domains (app_id, service, name) VALUES ($1, $2, $3)`, appID, service, name)
		if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: domains.app_id, domains.name") {
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

// SetComposeDomains makes domains, from x-moin.domains in compose.yml, the
// app's domains, in one transaction. With domains the app has exactly
// those: every other row of it goes, also the ones entered in moin.
// Without domains only the rows from compose.yml go, and the ones entered
// in moin count again. A name on two services is a DomainUsedError.
func (r *Apps) SetComposeDomains(appID string, domains []model.Domain) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if len(domains) == 0 {
		_, err = tx.Exec(`DELETE FROM domains WHERE app_id = $1 AND from_compose = 1`, appID)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	_, err = tx.Exec(`DELETE FROM domains WHERE app_id = $1`, appID)
	if err != nil {
		return err
	}
	for _, domain := range domains {
		_, err = tx.Exec(`INSERT INTO domains (app_id, service, name, main, from_compose) VALUES ($1, $2, $3, $4, 1)`, appID, domain.Service, domain.Name, domain.Main)
		if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: domains.app_id, domains.name") {
			return &DomainUsedError{Name: domain.Name}
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
