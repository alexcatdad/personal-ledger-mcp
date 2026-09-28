# Working on the personal ledger

- Read RUNBOOK.md and CONTRIBUTING.md before multi-command work; update runbooks and append important decisions to decisions.jsonl.
- Go services in internal/ledger own invariants. MCP/Huma are adapters; REST stays read-only. Never expose arbitrary SQL or shell tools.
- Commit state, audit and payload-bound operation-key results atomically. Preserve version checks and single-user write serialization.
- Use decimal strings and integer minor units, never floats for money. Keep expense currency/allocations distinct from account debits.
- SQL lives in internal/db/queries.sql; regenerate with make generate. Goose migrations are explicit; runtime grants and ownership are separate.
- UI uses TanStack Start and shadcn/Tailwind with generated types. Preserve read-only OLED-dark behavior and incomplete/estimated balances. Never persist tokens.
- Tailscale auth trusts only the exact configured loopback proxy, login and canonical Host/Origin; never add bearer fallback or expose the backend listener.
- Use TEST_DATABASE_URL=<disposable database> make verify. UI changes need focused browser verification. Never test against shared application data.
- No private CI, infrastructure or deployment is implied by this public repo. Keep credentials and deployment-specific records outside Git.
- Parallel tasks need explicit file ownership and final integration checks after all edits finish.
