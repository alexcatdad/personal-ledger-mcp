-- +goose Up
CREATE TABLE currencies(code text PRIMARY KEY, scale smallint NOT NULL CHECK(scale BETWEEN 0 AND 9));
INSERT INTO currencies VALUES ('RON',2),('EUR',2),('USD',2);
CREATE TABLE accounts(id text PRIMARY KEY, name text NOT NULL CHECK(length(name)>0), currency text NOT NULL REFERENCES currencies(code), scale smallint NOT NULL DEFAULT 2 CHECK(scale=2), opening_minor bigint NOT NULL, opening_date date NOT NULL);
CREATE TABLE categories(id text PRIMARY KEY, name text NOT NULL UNIQUE CHECK(length(name)>0));
CREATE TABLE projects(id text PRIMARY KEY, name text NOT NULL UNIQUE CHECK(length(name)>0));
CREATE TABLE expenses(id text PRIMARY KEY, account_id text NOT NULL REFERENCES accounts(id), amount_minor bigint NOT NULL CHECK(amount_minor>0), occurred_date date NOT NULL, description text NOT NULL, version bigint NOT NULL CHECK(version>0), status text NOT NULL CHECK(status IN ('posted','void')));
CREATE TABLE allocations(expense_id text NOT NULL REFERENCES expenses(id), ordinal integer NOT NULL, amount_minor bigint NOT NULL CHECK(amount_minor>0), category_id text NOT NULL REFERENCES categories(id), project_id text REFERENCES projects(id), PRIMARY KEY(expense_id,ordinal));
CREATE TABLE operation_results(operation_key text PRIMARY KEY, operation text NOT NULL, payload bytea NOT NULL, result jsonb NOT NULL);
CREATE TABLE audit_entries(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, entity_id text NOT NULL, action text NOT NULL, recorded_at timestamptz NOT NULL DEFAULT now(), snapshot jsonb NOT NULL);
CREATE INDEX expenses_account_date ON expenses(account_id,occurred_date);
CREATE INDEX audit_entity ON audit_entries(entity_id,id);
-- +goose Down
DROP TABLE audit_entries,operation_results,allocations,expenses,projects,categories,accounts,currencies;
