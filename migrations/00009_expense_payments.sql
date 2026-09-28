-- +goose Up
ALTER TABLE expenses ADD COLUMN currency text REFERENCES currencies(code);
ALTER TABLE expenses ADD COLUMN debit_minor bigint;
ALTER TABLE expenses ADD COLUMN debit_state text;
ALTER TABLE expenses ADD COLUMN conversion_evidence jsonb;
UPDATE expenses e SET currency=a.currency,debit_minor=e.amount_minor,debit_state='exact' FROM accounts a WHERE a.id=e.account_id;
ALTER TABLE expenses ALTER COLUMN currency SET NOT NULL;
ALTER TABLE expenses ALTER COLUMN debit_state SET NOT NULL;
ALTER TABLE expenses ADD CONSTRAINT expense_debit_state CHECK (
 (debit_state='unresolved' AND debit_minor IS NULL AND conversion_evidence IS NULL)
 OR (debit_state='exact' AND debit_minor IS NOT NULL AND debit_minor>0 AND conversion_evidence IS NULL)
 OR (debit_state='estimated' AND debit_minor IS NOT NULL AND debit_minor>0 AND conversion_evidence IS NOT NULL)
);
-- +goose Down
-- Downgrading would discard original currency and debit evidence. Refuse once
-- any expense cannot be represented by the old same-currency model.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM expenses e JOIN accounts a ON a.id=e.account_id WHERE e.currency<>a.currency OR e.debit_state<>'exact' OR e.debit_minor<>e.amount_minor) THEN
  RAISE EXCEPTION 'Cannot downgrade: cross-currency expense data exists';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE expenses DROP CONSTRAINT expense_debit_state;
ALTER TABLE expenses DROP COLUMN conversion_evidence,DROP COLUMN debit_state,DROP COLUMN debit_minor,DROP COLUMN currency;
