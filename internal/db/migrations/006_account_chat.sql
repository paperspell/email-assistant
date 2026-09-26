-- +goose Up
-- Each account may notify its own Telegram chat, so one installation can serve
-- several people. 0 means the installation's main chat, which is what every
-- existing account keeps.
ALTER TABLE accounts ADD COLUMN telegram_chat_id INTEGER NOT NULL DEFAULT 0;

-- Telegram message ids are unique only within a chat: two chats will both have
-- a message 1234. A digest and its parts must therefore be keyed by chat as
-- well, or a reply in one chat could resolve to another chat's digest.
ALTER TABLE digests ADD COLUMN tg_chat_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE digest_messages ADD COLUMN tg_chat_id INTEGER NOT NULL DEFAULT 0;
DROP INDEX idx_digest_messages_tg;
CREATE UNIQUE INDEX idx_digest_messages_tg ON digest_messages (tg_chat_id, tg_message_id);

-- +goose Down
DROP INDEX idx_digest_messages_tg;
CREATE UNIQUE INDEX idx_digest_messages_tg ON digest_messages (tg_message_id);
ALTER TABLE digest_messages DROP COLUMN tg_chat_id;
ALTER TABLE digests DROP COLUMN tg_chat_id;
ALTER TABLE accounts DROP COLUMN telegram_chat_id;
