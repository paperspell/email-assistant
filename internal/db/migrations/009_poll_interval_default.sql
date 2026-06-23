-- +goose Up

-- The default scan interval moved from 1m to 10m (poll.default_interval). Bump
-- boxes still on the old 1m default to the new default. Boxes a user explicitly
-- set to some other value are left untouched.
UPDATE accounts SET poll_interval = '10m' WHERE poll_interval = '1m';

-- The poll_interval column DEFAULT in 007 is cosmetic — the app always supplies
-- a value via Upsert — so it is left as-is. SQLite cannot ALTER a column default
-- in place without recreating the table; not worth it for a forward-only path.

-- +goose Down
-- No-op: forward-only. We do not revert backfilled intervals.
