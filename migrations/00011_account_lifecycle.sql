-- +goose Up
ALTER TABLE accounts ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK(version > 0), ADD COLUMN archived boolean NOT NULL DEFAULT false;
-- +goose Down
ALTER TABLE accounts DROP COLUMN archived, DROP COLUMN version;
