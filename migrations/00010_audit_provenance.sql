-- +goose Up
ALTER TABLE audit_entries ADD COLUMN source text NOT NULL DEFAULT 'unknown' CHECK(source IN ('unknown','application','mcp'));
ALTER TABLE audit_entries ADD COLUMN operation_key text NOT NULL DEFAULT '' CHECK(length(operation_key)<=200);
ALTER TABLE audit_entries ADD COLUMN request_id text NOT NULL DEFAULT '' CHECK(length(request_id)<=64);
ALTER TABLE audit_entries ADD COLUMN client_name text NOT NULL DEFAULT '' CHECK(length(client_name)<=200);
ALTER TABLE audit_entries ADD COLUMN client_version text NOT NULL DEFAULT '' CHECK(length(client_version)<=100);
CREATE INDEX audit_request_id ON audit_entries(request_id) WHERE request_id<>'';
-- +goose Down
DROP INDEX audit_request_id;
ALTER TABLE audit_entries DROP COLUMN source,DROP COLUMN operation_key,DROP COLUMN request_id,DROP COLUMN client_name,DROP COLUMN client_version;
