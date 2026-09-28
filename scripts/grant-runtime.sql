-- Run with psql as the migration owner after creating a LOGIN role separately.
-- Example: psql "$MIGRATION_DATABASE_URL" -v runtime_role=pa_mcp_runtime -f scripts/grant-runtime.sql
-- The role must not own the database/schema/tables or inherit a privileged role.
\set ON_ERROR_STOP on
GRANT CONNECT ON DATABASE :DBNAME TO :"runtime_role";
GRANT USAGE ON SCHEMA public TO :"runtime_role";
GRANT SELECT ON currencies,accounts,categories,projects,expenses,incomes,transfers,balance_observations,balance_adjustments,receivables,repayments,allocations,operation_results,audit_entries,buying_intents,intent_candidates TO :"runtime_role";
GRANT INSERT ON accounts,categories,projects,expenses,incomes,transfers,balance_observations,balance_adjustments,receivables,repayments,allocations,operation_results,audit_entries,buying_intents,intent_candidates TO :"runtime_role";
-- Lifecycle updates intentionally cannot change currency or opening balance.
GRANT UPDATE(name,version,archived) ON accounts TO :"runtime_role";
GRANT UPDATE ON expenses,incomes,transfers,balance_observations,balance_adjustments,receivables,repayments,categories,projects,buying_intents TO :"runtime_role";
GRANT DELETE ON allocations TO :"runtime_role";
GRANT USAGE,SELECT ON SEQUENCE audit_entries_id_seq TO :"runtime_role";
