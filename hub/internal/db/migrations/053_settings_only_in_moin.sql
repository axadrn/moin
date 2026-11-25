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

-- The domains: the ones the main service served, its canonical one, and
-- the ones of the other services, in place of the ones saved in moin.
CREATE TEMP TABLE from_file AS
SELECT app_id, detected FROM live
WHERE json_array_length(detected, '$.config.domains') > 0
   OR (SELECT COUNT(*) FROM json_each(detected, '$.config.service_domains')) > 0;

DELETE FROM domains WHERE app_id IN (SELECT app_id FROM from_file);

INSERT OR IGNORE INTO domains (app_id, service, name, main)
SELECT from_file.app_id, '', lower(domain.value), lower(domain.value) = lower(COALESCE(json_extract(from_file.detected, '$.canonical'), ''))
FROM from_file, json_each(from_file.detected, '$.domains') AS domain
ORDER BY from_file.app_id, domain.key;

INSERT OR IGNORE INTO domains (app_id, service, name)
SELECT from_file.app_id, service.key, lower(domain.value)
FROM from_file, json_each(from_file.detected, '$.service_domains') AS service, json_each(service.value) AS domain
ORDER BY from_file.app_id, service.key, domain.key;

-- The instant address: its provider, or off.
UPDATE apps SET instant = (SELECT json_extract(live.detected, '$.config.instant') FROM live WHERE live.app_id = apps.id)
WHERE id IN (SELECT app_id FROM live WHERE COALESCE(json_extract(detected, '$.config.instant'), '') != '');

DROP TABLE from_file;
DROP TABLE live;

-- +goose Down
-- The settings stay in moin: there is nothing to give back.
