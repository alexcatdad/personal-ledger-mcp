-- +goose Up
CREATE TABLE transfers (
 id text PRIMARY KEY,
 source_account_id text NOT NULL REFERENCES accounts(id),
 destination_account_id text NOT NULL REFERENCES accounts(id),
 source_minor bigint NOT NULL CHECK(source_minor>0),
 destination_minor bigint NOT NULL CHECK(destination_minor>0),
 occurred_date date NOT NULL,
 description text NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 status text NOT NULL CHECK(status IN ('posted','void')),
 CHECK(source_account_id<>destination_account_id)
);
CREATE INDEX transfers_source_date ON transfers(source_account_id,occurred_date);
CREATE INDEX transfers_destination_date ON transfers(destination_account_id,occurred_date);
CREATE INDEX transfers_date_id ON transfers(occurred_date DESC,id DESC);
-- +goose Down
DROP TABLE transfers;
