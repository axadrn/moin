-- +goose Up
-- compose.yml is plain Docker now: every setting lives only in moin. An
-- app whose live deploy took its domains or its instant address from the
-- old block of compose.yml keeps them: what that deploy served becomes the
-- app's setting in moin. Backups need nothing, moin saved that schedule
-- already.
CREATE TEMP TABLE live AS
SELECT deploys.app_id, deploys.detected
FROM deploys
WHERE deploys.status = 'succeeded' AND json_valid(deploys.detected)
  AND deploys.number = (SELECT MAX(newer.number) FROM deploys AS newer WHERE newer.app_id = deploys.app_id AND newer.status = 'succeeded');

-- The domains. Only the services whose domains the old block set change:
-- their rows become what the live deploy served for them. Domains entered
-- in moin for the other services stay as they are. A domain is unique per
-- app since 053, so a name another app has too never gets in the way; OR
-- IGNORE only drops a name the app has twice.
CREATE TEMP TABLE file_main AS
SELECT app_id, detected FROM live
WHERE json_array_length(detected, '$.config.domains') > 0
   OR EXISTS (SELECT 1 FROM json_each(detected, '$.config.service_domains') AS service
              WHERE service.key = json_extract(detected, '$.service'));

CREATE TEMP TABLE file_services AS
SELECT live.app_id, live.detected, service.key AS service
FROM live, json_each(live.detected, '$.config.service_domains') AS service
WHERE service.key != COALESCE(json_extract(live.detected, '$.service'), '');

DELETE FROM domains WHERE service = '' AND app_id IN (SELECT app_id FROM file_main);
DELETE FROM domains WHERE (app_id, service) IN (SELECT app_id, service FROM file_services);

INSERT OR IGNORE INTO domains (app_id, service, name, main)
SELECT file_main.app_id, '', lower(domain.value), lower(domain.value) = lower(COALESCE(json_extract(file_main.detected, '$.canonical'), ''))
FROM file_main JOIN apps ON apps.id = file_main.app_id, json_each(file_main.detected, '$.domains') AS domain
ORDER BY apps.created_at, domain.key;

INSERT OR IGNORE INTO domains (app_id, service, name)
SELECT file_services.app_id, file_services.service, lower(domain.value)
FROM file_services JOIN apps ON apps.id = file_services.app_id,
     json_each(file_services.detected, '$.service_domains."' || file_services.service || '"') AS domain
ORDER BY apps.created_at, file_services.service, domain.key;

-- The instant address: its provider, or off.
UPDATE apps SET instant = (SELECT json_extract(live.detected, '$.config.instant') FROM live WHERE live.app_id = apps.id)
WHERE id IN (SELECT app_id FROM live WHERE COALESCE(json_extract(detected, '$.config.instant'), '') != '');

DROP TABLE file_main;
DROP TABLE file_services;
DROP TABLE live;

-- +goose Down
-- The settings stay in moin: there is nothing to give back.
