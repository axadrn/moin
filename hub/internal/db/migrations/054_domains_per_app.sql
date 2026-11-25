-- +goose Up
-- A domain is unique per app, not in the whole hub: two apps may set the
-- same one, and whoever set it twice sorts it out. So no workspace can
-- block a domain for another one.
CREATE TABLE domains_new (
    id INTEGER PRIMARY KEY,
    app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    service TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL CHECK (name = lower(name)),
    main INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (app_id, name)
);
INSERT INTO domains_new (id, app_id, service, name, main, created_at)
SELECT id, app_id, service, name, main, created_at FROM domains;
DROP TABLE domains;
ALTER TABLE domains_new RENAME TO domains;
-- A service of an app has at most one main domain.
CREATE UNIQUE INDEX idx_domains_one_main ON domains(app_id, service) WHERE main = 1;

-- +goose Down
CREATE TABLE domains_old (
    id INTEGER PRIMARY KEY,
    app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    service TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL UNIQUE CHECK (name = lower(name)),
    main INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT OR IGNORE INTO domains_old (id, app_id, service, name, main, created_at)
SELECT id, app_id, service, name, main, created_at FROM domains ORDER BY id;
DROP TABLE domains;
ALTER TABLE domains_old RENAME TO domains;
CREATE INDEX idx_domains_app_id ON domains(app_id);
CREATE UNIQUE INDEX idx_domains_one_main ON domains(app_id, service) WHERE main = 1;
