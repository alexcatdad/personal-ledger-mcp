-- +goose Up
CREATE TABLE receivables (
 id text PRIMARY KEY,
 debtor text NOT NULL,
 amount_minor bigint NOT NULL CHECK(amount_minor>0),
 currency text NOT NULL REFERENCES currencies(code),
 due_date date,
 notes text NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 status text NOT NULL CHECK(status IN ('active','void')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE repayments (
 id text PRIMARY KEY,
 receivable_id text NOT NULL REFERENCES receivables(id),
 account_id text NOT NULL REFERENCES accounts(id),
 amount_minor bigint NOT NULL CHECK(amount_minor>0),
 occurred_date date NOT NULL,
 description text NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 status text NOT NULL CHECK(status IN ('posted','void'))
);
CREATE INDEX repayments_receivable ON repayments(receivable_id);
CREATE INDEX repayments_account ON repayments(account_id);
CREATE INDEX repayments_date_id ON repayments(occurred_date DESC,id DESC);
CREATE INDEX receivables_created ON receivables(created_at DESC,id DESC);
-- +goose Down
DROP TABLE repayments;
DROP TABLE receivables;
