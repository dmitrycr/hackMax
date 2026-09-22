-- +goose Up
CREATE TABLE app_metadata (
    key text PRIMARY KEY,
    value text NOT NULL
);
INSERT INTO app_metadata (key, value) VALUES ('stage', 'scaffold');

-- +goose Down
DROP TABLE app_metadata;

