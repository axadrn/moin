-- +goose Up
-- The app's password has one word in every layer: password. The column
-- keeps the htpasswd line, empty when the password is off. The deploys
-- moin knows say password, and so do the activity events.
ALTER TABLE apps RENAME COLUMN protect TO password;

UPDATE deploys SET detected = json_set(json_remove(detected, '$.protected'), '$.password', json('true'))
WHERE json_valid(detected) AND json_extract(detected, '$.protected') = 1;

UPDATE activity_events SET action = 'app.password_set' WHERE action = 'app.protected';
UPDATE activity_events SET action = 'app.password_removed' WHERE action = 'app.unprotected';

-- +goose Down
UPDATE activity_events SET action = 'app.unprotected' WHERE action = 'app.password_removed';
UPDATE activity_events SET action = 'app.protected' WHERE action = 'app.password_set';

UPDATE deploys SET detected = json_set(json_remove(detected, '$.password'), '$.protected', json('true'))
WHERE json_valid(detected) AND json_extract(detected, '$.password') = 1;

ALTER TABLE apps RENAME COLUMN password TO protect;
