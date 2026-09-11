-- +goose Up
-- A digest that exceeds Telegram's message limit is sent as several messages.
-- Every part is recorded here so a reply to any of them (`/important <n>`)
-- resolves to the digest; digests.tg_message_id keeps pointing at the part
-- carrying the buttons, which is the one whose keyboard is removed afterwards.
CREATE TABLE digest_messages (
    digest_id     TEXT    NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
    part_no       INTEGER NOT NULL,             -- 1-based, in sending order
    tg_message_id INTEGER NOT NULL,
    PRIMARY KEY (digest_id, part_no)
);
CREATE UNIQUE INDEX idx_digest_messages_tg ON digest_messages (tg_message_id);

-- Digests sent before this migration were single messages: register each as
-- its own part 1 so replies to them keep resolving.
INSERT INTO digest_messages (digest_id, part_no, tg_message_id)
SELECT id, 1, tg_message_id FROM digests WHERE tg_message_id <> 0;

-- +goose Down
DROP TABLE digest_messages;
