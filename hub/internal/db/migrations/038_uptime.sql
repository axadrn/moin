-- +goose Up
-- One word per thing: uptime visits a domain every minute; "check" is
-- only the daily test that a backup restores.
ALTER TABLE uptime_checks RENAME TO domain_uptime;

-- +goose Down
ALTER TABLE domain_uptime RENAME TO uptime_checks;
