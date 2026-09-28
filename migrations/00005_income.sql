-- +goose Up
CREATE TABLE incomes(id text PRIMARY KEY, account_id text NOT NULL REFERENCES accounts(id), amount_minor bigint NOT NULL CHECK(amount_minor>0), occurred_date date NOT NULL, description text NOT NULL, version bigint NOT NULL CHECK(version>0), status text NOT NULL CHECK(status IN ('posted','void')));
CREATE INDEX incomes_account_date ON incomes(account_id,occurred_date);
CREATE INDEX incomes_date_id ON incomes(occurred_date DESC,id DESC);
-- +goose Down
DROP TABLE incomes;
