-- +goose Up
CREATE TABLE buying_intents (
 id text PRIMARY KEY,
 created_at timestamptz NOT NULL DEFAULT now(),
 title text NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
 notes text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'open' CHECK(status IN ('open','purchased','cancelled')),
 version bigint NOT NULL DEFAULT 1 CHECK(version > 0),
 candidate_id text,
 outcome_note text NOT NULL DEFAULT '',
 expense_id text UNIQUE REFERENCES expenses(id),
 CHECK ((status = 'purchased') = (expense_id IS NOT NULL)),
 CHECK (candidate_id IS NULL OR status = 'purchased')
);
CREATE TABLE intent_candidates (
 id text PRIMARY KEY,
 created_at timestamptz NOT NULL DEFAULT now(),
 intent_id text NOT NULL REFERENCES buying_intents(id),
 reference text NOT NULL CHECK(length(reference) BETWEEN 1 AND 2000),
 notes text NOT NULL DEFAULT '',
 advertised_minor bigint CHECK(advertised_minor > 0),
 currency text CHECK(currency IN ('RON','EUR','USD')),
 CHECK ((advertised_minor IS NULL) = (currency IS NULL)),
 UNIQUE(intent_id,id)
);
ALTER TABLE buying_intents ADD FOREIGN KEY(id,candidate_id) REFERENCES intent_candidates(intent_id,id);
CREATE INDEX buying_intents_status_idx ON buying_intents(status,id);
-- +goose Down
ALTER TABLE buying_intents DROP CONSTRAINT buying_intents_id_candidate_id_fkey;
DROP TABLE intent_candidates;
DROP TABLE buying_intents;
