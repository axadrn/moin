-- +goose Up
-- Hosted plans are for businesses: who of the workspace confirmed, before a
-- checkout, that it subscribes as a business and not as a consumer, and
-- when. Empty until someone does; a hub without billing never asks.
ALTER TABLE workspaces ADD COLUMN business_confirmed_by TEXT;
ALTER TABLE workspaces ADD COLUMN business_confirmed_at TIMESTAMP;

-- +goose Down
ALTER TABLE workspaces DROP COLUMN business_confirmed_at;
ALTER TABLE workspaces DROP COLUMN business_confirmed_by;
