-- +goose Up
-- Focus mode: a per-account description of what the owner wants to hear about,
-- and the names the owner goes by, so the classifier can tell mail addressed to
-- them from the rest. A busy work mailbox is the motivating case — a hundred
-- messages a day of which only the ones addressed to the owner matter.
ALTER TABLE accounts ADD COLUMN focus   TEXT NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN aliases TEXT NOT NULL DEFAULT '';
-- The daily digest lists everything not notified. In a focused mailbox that
-- is, by design, everything the owner said they do not want to see.
ALTER TABLE accounts ADD COLUMN digest_enabled INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE accounts DROP COLUMN digest_enabled;
ALTER TABLE accounts DROP COLUMN aliases;
ALTER TABLE accounts DROP COLUMN focus;
