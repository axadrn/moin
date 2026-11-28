-- When a session signed in, and when it was last used, to the minute:
-- /cloud/settings lists them, so you can sign one out. 0 is not known.
ALTER TABLE sessions ADD COLUMN created INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN last_used INTEGER NOT NULL DEFAULT 0;
