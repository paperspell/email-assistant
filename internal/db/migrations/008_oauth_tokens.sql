-- +goose Up
ALTER TABLE accounts ADD COLUMN oauth_refresh_token TEXT NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN oauth_access_token  TEXT NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN oauth_token_expiry  DATETIME;

-- +goose Down
-- SQLite cannot drop columns before 3.35; for a forward-only deployment this is
-- a no-op. To roll back fully, recreate the accounts table without the token
-- columns.
