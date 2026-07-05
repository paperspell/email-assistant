-- +goose Up
-- Per-account first-run backfill window: how far back to scan for unread mail on
-- the very first poll. '0s' (default) disables it — the first run stays silent.
ALTER TABLE accounts ADD COLUMN backfill_window TEXT NOT NULL DEFAULT '0s';

-- +goose Down
ALTER TABLE accounts DROP COLUMN backfill_window;
