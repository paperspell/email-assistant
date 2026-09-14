-- +goose Up
-- Handles of automation accounts whose comments are noise in a focused
-- mailbox — an AI code reviewer, a dependency updater. GitHub Apps carry a
-- "[bot]" suffix and need no listing; GitLab has no such convention.
ALTER TABLE accounts ADD COLUMN bot_handles TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE accounts DROP COLUMN bot_handles;
