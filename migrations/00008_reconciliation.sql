-- +goose Up
CREATE TABLE balance_observations (
 id text PRIMARY KEY,
 account_id text NOT NULL REFERENCES accounts(id),
 as_of_date date NOT NULL,
 actual_minor bigint NOT NULL,
 recorded_ledger_minor numeric NOT NULL,
 basis text NOT NULL CHECK(basis='posted_end_of_day'),
 notes text NOT NULL,
 status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','void')),
 version bigint NOT NULL CHECK(version>0)
);
CREATE TABLE balance_adjustments (
 id text PRIMARY KEY,
 observation_id text NOT NULL REFERENCES balance_observations(id),
 account_id text NOT NULL REFERENCES accounts(id),
 amount_minor bigint NOT NULL CHECK(amount_minor<>0),
 occurred_date date NOT NULL,
 reason text NOT NULL CHECK(length(trim(reason))>0),
 status text NOT NULL DEFAULT 'posted' CHECK(status IN ('posted','void')),
 version bigint NOT NULL CHECK(version>0)
);
CREATE UNIQUE INDEX balance_adjustments_active ON balance_adjustments(observation_id) WHERE status='posted';
CREATE INDEX balance_observations_account_date ON balance_observations(account_id,as_of_date DESC,id DESC);
CREATE INDEX balance_adjustments_account_date ON balance_adjustments(account_id,occurred_date DESC,id DESC);
-- +goose Down
DROP TABLE balance_adjustments;
DROP TABLE balance_observations;
