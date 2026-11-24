-- +goose Up
-- A domain is a name visitors reach an app on, saved in moin. name is
-- the domain, lowercase, and only one app in the whole hub may use it.
-- service is the compose service it reaches, empty for the app's main
-- service. main marks the one the others of that service redirect to,
-- the canonical one. Domains that x-moin sets in compose.yml are not
-- here: the live deploy has them.
CREATE TABLE domains (
    id INTEGER PRIMARY KEY,
    app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    service TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL UNIQUE CHECK (name = lower(name)),
    main INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_domains_app_id ON domains(app_id);
-- A service of an app has at most one main domain.
CREATE UNIQUE INDEX idx_domains_one_main ON domains(app_id, service) WHERE main = 1;

-- The domains move out of their JSON columns on apps. When two apps had
-- the same domain, the older app keeps it.
INSERT OR IGNORE INTO domains (app_id, service, name, main)
SELECT apps.id, '', lower(domain.value), lower(domain.value) = lower(apps.canonical)
FROM apps, json_each(apps.domains) AS domain
ORDER BY apps.created_at, domain.key;

INSERT OR IGNORE INTO domains (app_id, service, name)
SELECT apps.id, service.key, lower(domain.value)
FROM apps, json_each(apps.service_domains) AS service, json_each(service.value) AS domain
ORDER BY apps.created_at, service.key, domain.key;

ALTER TABLE apps DROP COLUMN domains;
ALTER TABLE apps DROP COLUMN canonical;
ALTER TABLE apps DROP COLUMN service_domains;

-- +goose Down
ALTER TABLE apps ADD COLUMN service_domains TEXT NOT NULL DEFAULT '{}';
ALTER TABLE apps ADD COLUMN canonical TEXT NOT NULL DEFAULT '';
ALTER TABLE apps ADD COLUMN domains TEXT NOT NULL DEFAULT '[]';
DROP TABLE domains;
