-- +goose Up
-- Match the deterministic newest-first ordering used by bounded expense pages.
CREATE INDEX expenses_date_id ON expenses(occurred_date DESC,id DESC);

-- +goose Down
DROP INDEX expenses_date_id;
