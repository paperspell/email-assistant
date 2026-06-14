-- +goose Up

CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL
);

-- +goose Down

DROP TABLE IF EXISTS settings;
