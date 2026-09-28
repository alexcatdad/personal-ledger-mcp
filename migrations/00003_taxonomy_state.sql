-- +goose Up
ALTER TABLE categories ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0), ADD COLUMN archived boolean NOT NULL DEFAULT false;
ALTER TABLE projects ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0), ADD COLUMN archived boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE projects DROP COLUMN archived, DROP COLUMN version;
ALTER TABLE categories DROP COLUMN archived, DROP COLUMN version;
