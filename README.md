# Personal ledger

Agent-managed personal finance on PostgreSQL. MCP performs explicit domain operations; the browser reports the same financial state through a read-only Huma API.

## Current implementation

The application implements accounts with dated opening balances, category/project allocations, expenses, income, transfers, existing debts and repayments, buying intents, and atomic structured imports. Mutations use durable retry keys, version-checked corrections/voids and audit history. RON, EUR and USD are supported. Opening balances, transfers and principal repayments remain separate from income and spending. Amounts cross the API as exact decimal strings.

This is an early single-user application. Budget limits, shared finances, automatic exchange-rate retrieval and direct cloud-client authentication are outside the current scope. The repository contains no hosted service or bank connection. See [RUNBOOK.md](RUNBOOK.md) for setup and [CONTRIBUTING.md](CONTRIBUTING.md) for checks. Licensed under [MIT](LICENSE).

## Local development

See [CONTRIBUTING.md](CONTRIBUTING.md) for quality gates and review conventions. Follow [RUNBOOK.md](RUNBOOK.md) for the isolated PostgreSQL container, environment setup, migrations, checks, and build. Requires Go 1.27.1, Node 26.10.0 with npm, and PostgreSQL 16.

```sh
export DATABASE_URL='postgres://pa_mcp:local-development-only@127.0.0.1:55439/pa_mcp_test?sslmode=disable'
export PA_MCP_TOKEN="$(openssl rand -hex 32)"
make migrate
make frontend build
./bin/pa-mcp
```

Open `http://127.0.0.1:8080` and enter your configured token. The dashboard retains it only for the current page session. Local Codex can connect to `http://127.0.0.1:8080/mcp` with an `Authorization: Bearer …` header. Never put the token in a URL, source code, or frontend build variables.

## MCP tools

- `create_account`, `create_category`, `create_project`
- `rename_category`, `rename_project`, `archive_category`, `archive_project`
- `create_expense`, `amend_expense`, `void_expense`, `bulk_expenses`, `bulk_transactions`
- `get_dashboard`, `get_audit`, `query_expenses`, `query_monthly_report`

`query_monthly_report` / `GET /api/reports/monthly` require `month` (`YYYY-MM`) and accept an optional `account_id`. The report includes income and original-currency spending totals, category/project spending allocations, and native-currency account movements. Transfers, debt principal repayments and adjustments remain separate. Unknown account debits are excluded from the numeric net subtotal and explicitly mark that month's movement total incomplete; estimated debits are also counted. Opening balances are excluded. All sections share one database snapshot.
- `create_income`, `amend_income`, `void_income`, `query_income`
- `create_transfer`, `amend_transfer`, `void_transfer`, `query_transfers`
- `create_receivable`, `amend_receivable`, `void_receivable`, `query_receivables`
- `create_repayment`, `amend_repayment`, `void_repayment`, `query_repayments`
- `record_balance_observation`, `amend_balance_observation`, `void_balance_observation`, `query_balance_observations`
- `create_balance_adjustment`, `void_balance_adjustment`, `query_balance_adjustments`
- `create_buying_intent`, `update_buying_intent`, `add_intent_candidate`, `cancel_buying_intent`, `confirm_intent_purchase`, `query_buying_intents`

Every mutation requires an `operation_key`. Reuse that key and the same payload after a timeout; use a new key for a separate intended operation. Amend/void require the current `expected_version`. Backend errors require refreshing or correcting the payload, never silently replacing an unrelated record.

Local bearer mode protects data with the configured token. Deployed Tailscale mode protects UI, API, MCP and OpenAPI using the allowed Serve identity; `/auth/session` identifies the active access mode. `/healthz` exposes only database availability. See deploy/README.md for the trusted proxy boundary.

## Deployment boundary

One image serves Go, MCP, API, and static dashboard assets. Run `/app/pa-mcp migrate` explicitly with a migration identity before starting the image with its runtime identity. Do not grant DDL, table ownership, or audit-update/delete privileges to the runtime role. Keep the app private. The homelab uses Tailscale Serve identity authentication; bearer mode is retained for isolated development. Build and deploy privately using your own infrastructure; no private runner, registry or production deployment is connected to this public repository.

## Expense queries

`query_expenses` / `GET /api/expenses` accept inclusive `from_date`/`to_date`, exact account/currency/category/project filters, `status` (`posted` by default, `void`, or `all`), `limit` (default 50, maximum 100), and `offset`. Combined category/project filters must match the same allocation. Results include record IDs and versions for explicit reconciliation or correction.

Payment totals cover complete matching expenses across the full filter scope, independently of pagination. Matching allocation totals describe the selected category/project lines. A 430 RON payment with a 250 RON Suspension line therefore contributes 430 RON to payment totals and 250 RON to Suspension allocation totals. Voids remain inspectable but never contribute to financial totals. Currencies are never added together.

Offset pagination reads current state; mutations between page requests can change page membership. Refresh from offset zero after edits when reconciling a statement.

## Categories and projects

Agents can create, rename, and archive categories/projects through explicit MCP tools. Rename/archive require `operation_key`, `id`, and `expected_version`; rename also takes `name`. Query current dashboard state before editing. Archived entities remain visible with their version and archive state so historical names and reports still resolve.

New expenses cannot use archived taxonomy entries. Correcting an existing expense can retain archived references that it already contains. Renaming an archived entry is permitted, and does not reactivate it. Unarchiving is not implemented.

## Buying intents

`create_buying_intent` records the goal and context without moving money. `add_intent_candidate` preserves an ad/reference and optional paired advertised amount/currency. References are plain data; the backend never fetches them.

`update_buying_intent` replaces an open intent's title and notes using its current version. Both fields must be supplied; an empty notes string clears previous notes. Candidate offers remain intact, and closed decisions cannot be edited.

`query_buying_intents` defaults to open intents for assistant check-ins. Optional `search` matches title or notes using a case-insensitive literal substring (maximum 200 characters after trimming); `%` and `_` are ordinary text. Search combines with status and keeps total counts independent of pagination. `GET /api/buying-intents` exposes the same read model, with status and bounded pagination. The dashboard shows recent intents and keeps offer prices separate from actual expense amounts. Scheduling belongs to the assistant client.

`confirm_intent_purchase` requires the current intent version and exactly one explicit new expense draft or existing posted expense ID with its current version. It links the expense and marks the intent purchased atomically. Linking never debits twice, and a single expense cannot be linked to multiple intents. `cancel_buying_intent` records the outcome without affecting balances. Retry all mutations with the original operation key and payload.

A later expense correction/void is reflected in queried linked expense state. The purchased intent remains the historical decision; voiding a financial record does not silently erase the purchase context or reopen an intent.

## Explicit split remainder

An expense allocation may use `"remainder": true` with its amount omitted. Exactly one line may request this; the backend subtracts every explicit line from the total, regardless of order. For example, a 430 RON expense with a 250 RON line resolves the remainder to 180 RON. The result must be positive. Missing amounts without the flag, multiple remainders, or specifying both amount and remainder are rejected.

This also applies to expense corrections and new expense drafts in purchase confirmations. Responses, audit snapshots and stored allocations contain resolved amounts; the remainder instruction does not persist as an automatically changing formula.

## Structured bulk expenses

`bulk_expenses` accepts one `operation_key` and 1–100 ordered `items`. Each item specifies:

- `action: "create"` and an `expense` draft.
- `action: "amend"`, the existing `id`, its `expected_version`, and a replacement `expense` draft.
- `action: "void"`, the existing `id` and its `expected_version`.

Drafts have the same fields as a new expense except for the operation key, which belongs to the entire batch. Existing IDs cannot appear twice in a batch. Success returns `expenses` in input order, including their IDs and current versions.

Any item failure rolls back all expenses, audit entries and retry state for the batch. MCP returns a tool error containing JSON `issues` with zero-based `index`, `code` and `message` for each failing item. Unexpected infrastructure failures abort immediately. Correct rejected items and resubmit; an unsuccessful batch does not reserve its key. Once committed, reuse the original key and identical payload after a timeout; a changed payload under that key is rejected.

The agent parses statements and reconciles existing records using queries. The server never parses documents or guesses duplicates. This operation covers expenses; use `bulk_transactions` below for mixed expense, income and transfer batches. Atomicity applies to each batch, not to a whole statement split across several batches.

## Income

`create_income` records a positive amount, account, currency, date and description with an `operation_key`. Currency must match the receiving account, and the date cannot precede the account's opening date. `amend_income` replaces those fields using the record ID and `expected_version`; `void_income` removes its financial effect while preserving history. Both require retry keys.

`query_income` and `GET /api/income` support account/currency filters, inclusive `from_date`/`to_date`, `status` (posted by default, void or all), limit (50 by default, maximum 100) and offset. Posted income totals cover the entire filter scope, independently of pagination, and remain separated by currency. Opening balances are excluded. Account balances include posted income and subtract posted expenses.

Transfers between your own accounts and repayment of loan principal must not be recorded as income. Transfers and principal repayments use their dedicated operations below. Mixed imports can use `bulk_transactions` below.

## Transfers and cash withdrawals

`create_transfer` records two legs atomically: `source_account_id`, `destination_account_id`, `source_currency`, `destination_currency`, `source_amount`, `destination_amount`, `date`, `description`, and one `operation_key`. Accounts must differ. Both amounts are positive decimal strings and must match for same-currency principal transfers. The date must be at or after both account opening dates.

For a cross-currency transfer, supply the actual amount debited and credited. No rate provider or conversion estimate is used. Record separately charged fees as expenses; do not include them in the principal twice.

`amend_transfer` replaces both legs using the record ID and expected version. `void_transfer` reverses both balance effects while preserving the audit history. Both use retry keys. `query_transfers` and `GET /api/transfers` accept an `account_id` matching either leg, inclusive dates, status and bounded pagination. Transfers never contribute to spending or income totals.

A bank withdrawal is a transfer into a cash account in that currency. Later cash purchases are expenses paid from the cash account. The dashboard shows recent posted transfers with both currencies and amounts.

## Mixed statement batches

Use `bulk_transactions` when an interpreted statement contains expenses, income, transfers and principal repayments. It accepts the same batch-level `operation_key` and 1–100 item bound as `bulk_expenses`.

Each item has `kind` (`expense`, `income`, `transfer`, or `repayment`) and `action` (`create`, `amend`, or `void`). Creates and amendments require exactly one matching `expense`, `income`, `transfer`, or `repayment` draft, without a nested operation key. Amendments and voids require `id` and `expected_version`; voids carry no draft. A kind/ID target cannot occur twice in one batch. Repayment items additionally require `receivable_id` and `expected_receivable_version` for every action; their drafts contain `account_id`, `amount`, `date` and `description`. Multiple repayments against one debt must use successive expected debt versions in input order. The `adjustment` kind supports `void` only and can precede a real transaction replacement in the same atomic batch.

Successful `results` retain input order and contain `kind` plus the resulting domain record. Repayment results include the updated `repayment` and `receivable`. Any rejected item rolls back all transaction kinds, audit entries and retry state. Indexed error and retry behavior is the same as for expense batches. This records ledger data only; it never initiates a bank payment or transfer.

## Receivables and repayments

`create_receivable` records an existing debt with `debtor`, `amount`, `currency`, optional `due_date`, `notes` and an `operation_key`. It does not move money. `amend_receivable` and `void_receivable` require the ID and `expected_version`. Principal cannot be reduced below recorded repayments; changing currency requires no posted repayments. A debt with posted repayments cannot be voided until those repayments are explicitly corrected or voided.

`create_repayment` records a positive principal amount received into an account using `receivable_id`, `expected_receivable_version`, `account_id`, `amount`, `date`, `description` and an operation key. Debt and account currencies must match. The account credit and debt reduction commit atomically, without income or expense. Overpayment and cross-currency repayment are not supported.

`amend_repayment` additionally requires its own ID and `expected_version`; it cannot move the payment to another debt. `void_repayment` requires both record IDs and versions. Every repayment change increments the parent debt version and records both audit histories. Results return both the updated `repayment` and `receivable`; use those returned versions for the next operation.

`query_receivables` / `GET /api/receivables` filter status (open by default, settled, void or all), currency and exact debtor. Full-filter `outstanding_totals` stay separated by currency. `query_repayments` / `GET /api/repayments` filter debt, account and posted/void/all status. Both use limit (default 50, maximum 100) and offset.

A debt becomes settled when posted repayments equal its principal, and reopens if a repayment is reduced or voided. Due dates are stored context for the assistant; the backend does not schedule reminders. Newly issued loans, write-offs and overpayment rules remain undecided.

## Balance reconciliation

`record_balance_observation` accepts an actual balance, account/currency, `as_of_date`, notes and explicit `basis: "posted_end_of_day"`. This provisional basis includes posted movements through that date and excludes later/voided movements; it does not compare intraday or pending bank balances. Recording an observation does not change money.

`query_balance_observations` / `GET /api/balance-observations` return the ledger snapshot saved when the observation was recorded or amended, plus the current ledger balance and difference at the same date. Earlier snapshots remain in the audit history. Later corrections can change the current comparison. Amend/void observation actions require its current version and no active linked adjustment.

Only when explicitly requested for an unexplained difference, `create_balance_adjustment` accepts the observation ID/version, the exact current `expected_ledger_balance` string from a fresh query, the signed difference as `amount`, and a reason. A stale comparison is rejected. Adjustments change account balances but are not income or spending. A matching observation never needs a zero adjustment.

`void_balance_adjustment` preserves history and removes the adjustment effect. If its cause is later identified, submit a mixed batch containing `kind: "adjustment", action: "void"` with the adjustment ID/version and the actual expense/income/transfer operation. Both changes commit together, avoiding double counting. Adjustment items support void only in mixed batches.

All mutations are serialized for this single-user ledger so a comparison cannot race another application write during adjustment creation. Normal reporting reads remain concurrent. The current implementation does not make automatic adjustments or infer the cause of a discrepancy.

## Cross-currency expenses

Expenses and allocations retain their original `amount` and `currency`. Account balances use a separate `payment` in the paying account's currency. Same-currency payments derive their exact debit automatically. For cross-currency payments, supply one of:

- `{"currency":"RON","state":"exact","amount":"51.23"}` for the actual bank debit.
- `{"currency":"RON","state":"estimated","evidence":{"source":"User supplied quote","rate":"5.123","effective_at":"2026-09-27T12:00:00Z","retrieved_at":"2026-09-27T12:01:00Z"}}` for an explicitly sourced estimate. The rate means account-currency units per original-currency unit. Positive decimal arithmetic rounds halves upward to two decimals; an optional supplied amount must match that result.
- Omit payment, or supply `state:"unresolved"` with the account currency and no amount/evidence, when the debit is unknown.

For example, a 10 EUR purchase with an exact 51.23 RON bank charge remains 10 EUR of spending and debits the RON account by 51.23. An unresolved debit does not invent a numeric account movement; the balance is marked `incomplete`. Estimated debits are included and marked `estimated`. Counts show how many posted expenses require attention. Income and repayment currency rules remain unchanged.

Amendments replace the complete expense and payment together. Supply payment explicitly when keeping a cross-currency debit; resolving it later updates the same expense and audit history. Expense batches and buying-intent purchases use the same validation. Reports and currency filters use the original expense currency.

Reconciliation exposes the same quality for the as-of date. A zero difference is matched only when the ledger balance is exact; adjustments are rejected while relevant conversions are estimated or unresolved. Resolve the payment first. No automatic provider retrieval is enabled yet; supplied estimates must have valid source and effective/retrieval timestamps, and must not be represented as actual bank charges.

## Assistant check-ins

Use `query_expenses` with `payment_state:"needs_attention"` to retrieve posted expenses with estimated or unresolved account debits. The narrower values `exact`, `estimated` and `unresolved` are also supported; omit the filter for all payment states. HTTP uses `/api/expenses?payment_state=needs_attention`. Existing account/date/currency/category/project/status filters and pagination still apply, and all totals span the complete filter scope. Resolving a payment removes it from this query automatically.

For a daily conversation, the assistant can combine this query with open buying intents and open receivables, ask about the returned records, and submit explicit corrections using their versions. The backend does not schedule the check-in, interpret a bank statement or guess a purchase/repayment outcome.

## Audit provenance

New audit rows include `source` (`mcp` or `application`), `operation_key`, a server-generated `request_id`, and informational `client_name`/`client_version` when supplied by the MCP protocol. All audit rows in an atomic batch share its request ID and operation key. Replaying an operation does not append audit rows. Client labels are bounded and are not authenticated identities; authorization uses the configured authentication mode (Serve identity on the homelab, bearer token in isolated development). Credentials and raw request headers are not recorded.

Historical rows created before provenance support remain `source:"unknown"` with empty metadata. Financial snapshots retain their existing shape. Read provenance through `get_audit` or `/api/audit/{id}`.

The receivable overview now supports open/settled/void/all filtering and pagination. Expand a debt to load its repayment history, including voided entries, account, date, amount and revision. These are principal repayments, separate from income.

## Project spending reports

Use `query_project_report` with `project_id` and optional inclusive `from_date`, `to_date`, `limit` and `offset`, or read `GET /api/projects/{id}/report`. The report contains project metadata, original-currency `spending_totals`, `category_totals`, and a paginated `history`. Totals cover the full selected date range, including when a page is empty. Archived projects remain readable.

Project spending counts only allocations assigned to that project. History contains complete purchases, which may also include other projects; `history.totals` are whole-purchase totals and must not be substituted for `spending_totals`. The read-only Project report panel displays that distinction and provides project/date filters and pagination. Income/net cost are outside the agreed v1 project scope.

## Account maintenance

Use `rename_account` and `set_account_archived` with the current account `expected_version` and a fresh operation key. `set_account_archived` requires an explicit `archived` boolean; false reopens the account. Names and archive state are audited. Currency and opening balance cannot be changed. Read current account state before editing, especially after replaying an old create operation that predates account revisions.

Archived accounts remain in balance and history reports. New spending, income, transfers, repayments and balance adjustments are rejected even when backdated. Corrections may retain an existing archived account association, and records may be voided; a correction cannot assign a different archived account. Transfer associations are checked per source/destination leg. Balance observations remain possible because they do not move money. The dashboard labels archived accounts and continues to show their balances.
