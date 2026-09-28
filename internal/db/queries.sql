-- name: GetOperation :one
SELECT operation, payload, result FROM operation_results WHERE operation_key=$1;
-- name: SaveOperation :exec
INSERT INTO operation_results(operation_key,operation,payload,result) VALUES($1,$2,$3,$4);
-- name: InsertAudit :exec
INSERT INTO audit_entries(entity_id,action,snapshot,source,operation_key,request_id,client_name,client_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: InsertAccount :exec
INSERT INTO accounts(id,name,currency,opening_minor,opening_date) VALUES($1,$2,$3,$4,$5::text::date);
-- name: InsertCategory :exec
INSERT INTO categories(id,name) VALUES($1,$2);
-- name: InsertProject :exec
INSERT INTO projects(id,name) VALUES($1,$2);
-- name: LockAccount :one
SELECT currency,opening_date::text,version,archived FROM accounts WHERE id=$1 FOR UPDATE;
-- name: UpsertExpense :exec
INSERT INTO expenses(id,account_id,amount_minor,occurred_date,description,version,status,currency,debit_minor,debit_state,conversion_evidence) VALUES(sqlc.arg(id),sqlc.arg(account_id),sqlc.arg(amount_minor),sqlc.arg(occurred_date)::text::date,sqlc.arg(description),sqlc.arg(version),'posted',sqlc.arg(currency),sqlc.narg(debit_minor),sqlc.arg(debit_state),sqlc.narg(conversion_evidence)) ON CONFLICT(id) DO UPDATE SET account_id=excluded.account_id,amount_minor=excluded.amount_minor,occurred_date=excluded.occurred_date,description=excluded.description,version=excluded.version,currency=excluded.currency,debit_minor=excluded.debit_minor,debit_state=excluded.debit_state,conversion_evidence=excluded.conversion_evidence;
-- name: DeleteAllocations :exec
DELETE FROM allocations WHERE expense_id=$1;
-- name: InsertAllocation :exec
INSERT INTO allocations(expense_id,ordinal,amount_minor,category_id,project_id) VALUES($1,$2,$3,$4,NULLIF($5::text,''));
-- name: LockExpense :one
SELECT version,status FROM expenses WHERE id=$1 FOR UPDATE;
-- name: VoidExpense :exec
UPDATE expenses SET status='void',version=version+1 WHERE id=$1;
-- name: ReadExpense :one
SELECT e.id,e.account_id,e.amount_minor,e.currency,a.currency AS debit_currency,e.debit_minor,e.debit_state,e.conversion_evidence,e.occurred_date::text AS occurred_date,e.description,e.status,e.version FROM expenses e JOIN accounts a ON a.id=e.account_id WHERE e.id=$1;
-- name: ReadAllocations :many
SELECT amount_minor,category_id,COALESCE(project_id,'')::text AS project_id FROM allocations WHERE expense_id=$1 ORDER BY ordinal;
-- name: AccountBalances :many
SELECT a.id,a.name,a.currency,a.opening_minor,a.opening_date::text AS opening_date,a.version,a.archived,
(a.opening_minor::numeric-COALESCE(e.amount,0)+COALESCE(i.amount,0)-COALESCE(t_out.amount,0)+COALESCE(t_in.amount,0)+COALESCE(r.amount,0)+COALESCE(ba.amount,0))::text AS balance,COALESCE(e.estimated_count,0)::bigint AS estimated_count,COALESCE(e.unresolved_count,0)::bigint AS unresolved_count
FROM accounts a
LEFT JOIN (SELECT account_id,sum(debit_minor) AS amount,count(*) FILTER(WHERE debit_state='estimated') AS estimated_count,count(*) FILTER(WHERE debit_state='unresolved') AS unresolved_count FROM expenses WHERE status='posted' GROUP BY account_id) e ON e.account_id=a.id
LEFT JOIN (SELECT account_id,sum(amount_minor) AS amount FROM incomes WHERE status='posted' GROUP BY account_id) i ON i.account_id=a.id
LEFT JOIN (SELECT source_account_id,sum(source_minor) AS amount FROM transfers WHERE status='posted' GROUP BY source_account_id) t_out ON t_out.source_account_id=a.id
LEFT JOIN (SELECT destination_account_id,sum(destination_minor) AS amount FROM transfers WHERE status='posted' GROUP BY destination_account_id) t_in ON t_in.destination_account_id=a.id
LEFT JOIN (SELECT account_id,sum(amount_minor) AS amount FROM repayments WHERE status='posted' GROUP BY account_id) r ON r.account_id=a.id
LEFT JOIN (SELECT account_id,sum(amount_minor) AS amount FROM balance_adjustments WHERE status='posted' GROUP BY account_id) ba ON ba.account_id=a.id
ORDER BY a.name,a.id;
-- name: Categories :many
SELECT id,name,version,archived FROM categories ORDER BY name,id;
-- name: Projects :many
SELECT id,name,version,archived FROM projects ORDER BY name,id;
-- name: RecentExpenseIDs :many
SELECT id FROM expenses ORDER BY occurred_date DESC,id DESC LIMIT 100;
-- name: ProjectSpending :many
SELECT p.id,p.name,e.currency,sum(l.amount_minor)::text AS amount FROM allocations l JOIN expenses e ON e.id=l.expense_id JOIN accounts a ON a.id=e.account_id JOIN projects p ON p.id=l.project_id WHERE e.status='posted' GROUP BY p.id,e.currency ORDER BY p.name,e.currency;
-- name: Audit :many
SELECT id,entity_id,action,to_char(recorded_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')::text AS recorded_at,snapshot,source,operation_key,request_id,client_name,client_version FROM audit_entries WHERE entity_id=$1 ORDER BY id;

-- name: FilteredExpenseIDs :many
SELECT e.id
FROM expenses e JOIN accounts a ON a.id=e.account_id
WHERE (sqlc.arg(account_id)::text='' OR e.account_id=sqlc.arg(account_id))
AND (sqlc.arg(currency)::text='' OR e.currency=sqlc.arg(currency))
AND (sqlc.arg(payment_state)::text='' OR e.debit_state=sqlc.arg(payment_state) OR (sqlc.arg(payment_state)::text='needs_attention' AND e.debit_state IN ('estimated','unresolved')))
AND (sqlc.arg(from_date)::text='' OR e.occurred_date>=NULLIF(sqlc.arg(from_date)::text,'')::date)
AND (sqlc.arg(to_date)::text='' OR e.occurred_date<=NULLIF(sqlc.arg(to_date)::text,'')::date)
AND (sqlc.arg(status)::text='all' OR e.status=sqlc.arg(status))
AND EXISTS(SELECT 1 FROM allocations l WHERE l.expense_id=e.id
 AND (sqlc.arg(category_id)::text='' OR l.category_id=sqlc.arg(category_id))
 AND (sqlc.arg(project_id)::text='' OR l.project_id=sqlc.arg(project_id)))
ORDER BY e.occurred_date DESC,e.id DESC LIMIT sqlc.arg(page_limit)::bigint OFFSET sqlc.arg(page_offset)::bigint;

-- name: FilteredExpenseCount :one
SELECT count(*)
FROM expenses e JOIN accounts a ON a.id=e.account_id
WHERE (sqlc.arg(account_id)::text='' OR e.account_id=sqlc.arg(account_id))
AND (sqlc.arg(currency)::text='' OR e.currency=sqlc.arg(currency))
AND (sqlc.arg(payment_state)::text='' OR e.debit_state=sqlc.arg(payment_state) OR (sqlc.arg(payment_state)::text='needs_attention' AND e.debit_state IN ('estimated','unresolved')))
AND (sqlc.arg(from_date)::text='' OR e.occurred_date>=NULLIF(sqlc.arg(from_date)::text,'')::date)
AND (sqlc.arg(to_date)::text='' OR e.occurred_date<=NULLIF(sqlc.arg(to_date)::text,'')::date)
AND (sqlc.arg(status)::text='all' OR e.status=sqlc.arg(status))
AND EXISTS(SELECT 1 FROM allocations l WHERE l.expense_id=e.id
 AND (sqlc.arg(category_id)::text='' OR l.category_id=sqlc.arg(category_id))
 AND (sqlc.arg(project_id)::text='' OR l.project_id=sqlc.arg(project_id)));

-- name: FilteredExpenseTotals :many
SELECT e.currency,sum(e.amount_minor)::text AS amount
FROM expenses e JOIN accounts a ON a.id=e.account_id
WHERE (sqlc.arg(account_id)::text='' OR e.account_id=sqlc.arg(account_id))
AND (sqlc.arg(currency)::text='' OR e.currency=sqlc.arg(currency))
AND (sqlc.arg(payment_state)::text='' OR e.debit_state=sqlc.arg(payment_state) OR (sqlc.arg(payment_state)::text='needs_attention' AND e.debit_state IN ('estimated','unresolved')))
AND (sqlc.arg(from_date)::text='' OR e.occurred_date>=NULLIF(sqlc.arg(from_date)::text,'')::date)
AND (sqlc.arg(to_date)::text='' OR e.occurred_date<=NULLIF(sqlc.arg(to_date)::text,'')::date)
AND (sqlc.arg(status)::text='all' OR e.status=sqlc.arg(status))
AND EXISTS(SELECT 1 FROM allocations l WHERE l.expense_id=e.id
 AND (sqlc.arg(category_id)::text='' OR l.category_id=sqlc.arg(category_id))
 AND (sqlc.arg(project_id)::text='' OR l.project_id=sqlc.arg(project_id)))
AND e.status='posted' GROUP BY e.currency ORDER BY e.currency;

-- name: FilteredAllocationTotals :many
SELECT l.category_id,COALESCE(l.project_id,'')::text AS project_id,e.currency,sum(l.amount_minor)::text AS amount
FROM expenses e JOIN accounts a ON a.id=e.account_id JOIN allocations l ON l.expense_id=e.id
WHERE (sqlc.arg(account_id)::text='' OR e.account_id=sqlc.arg(account_id))
AND (sqlc.arg(currency)::text='' OR e.currency=sqlc.arg(currency))
AND (sqlc.arg(payment_state)::text='' OR e.debit_state=sqlc.arg(payment_state) OR (sqlc.arg(payment_state)::text='needs_attention' AND e.debit_state IN ('estimated','unresolved')))
AND (sqlc.arg(from_date)::text='' OR e.occurred_date>=NULLIF(sqlc.arg(from_date)::text,'')::date)
AND (sqlc.arg(to_date)::text='' OR e.occurred_date<=NULLIF(sqlc.arg(to_date)::text,'')::date)
AND (sqlc.arg(status)::text='all' OR e.status=sqlc.arg(status))
AND EXISTS(SELECT 1 FROM allocations l WHERE l.expense_id=e.id
 AND (sqlc.arg(category_id)::text='' OR l.category_id=sqlc.arg(category_id))
 AND (sqlc.arg(project_id)::text='' OR l.project_id=sqlc.arg(project_id)))
AND e.status='posted'
AND (sqlc.arg(category_id)::text='' OR l.category_id=sqlc.arg(category_id))
AND (sqlc.arg(project_id)::text='' OR l.project_id=sqlc.arg(project_id))
GROUP BY l.category_id,l.project_id,e.currency ORDER BY l.category_id,l.project_id,e.currency;

-- name: LockCategory :one
SELECT id,name,version,archived FROM categories WHERE id=$1 FOR UPDATE;
-- name: LockProject :one
SELECT id,name,version,archived FROM projects WHERE id=$1 FOR UPDATE;
-- name: UpdateCategory :exec
UPDATE categories SET name=$2,archived=$3,version=version+1 WHERE id=$1;
-- name: UpdateProject :exec
UPDATE projects SET name=$2,archived=$3,version=version+1 WHERE id=$1;

-- name: InsertBuyingIntent :exec
INSERT INTO buying_intents(id,title,notes) VALUES($1,$2,$3);
-- name: ReadBuyingIntent :one
SELECT id,title,notes,status,version,coalesce(candidate_id,'')::text AS candidate_id,outcome_note,coalesce(expense_id,'')::text AS expense_id FROM buying_intents WHERE id=$1;
-- name: LockBuyingIntent :one
SELECT version,status FROM buying_intents WHERE id=$1 FOR UPDATE;
-- name: InsertIntentCandidate :exec
INSERT INTO intent_candidates(id,intent_id,reference,notes,advertised_minor,currency) VALUES($1,$2,$3,$4,$5,$6);
-- name: ListIntentCandidates :many
SELECT id,reference,notes,advertised_minor,coalesce(currency,'')::text AS currency FROM intent_candidates WHERE intent_id=$1 ORDER BY created_at,id;
-- name: UpdateBuyingIntent :exec
UPDATE buying_intents SET title=$2,notes=$3,version=version+1 WHERE id=$1;
-- name: BumpBuyingIntent :exec
UPDATE buying_intents SET version=version+1 WHERE id=$1;
-- name: CancelBuyingIntent :exec
UPDATE buying_intents SET status='cancelled',outcome_note=$2,version=version+1 WHERE id=$1;
-- name: PurchaseBuyingIntent :exec
UPDATE buying_intents SET status='purchased',outcome_note=$2,candidate_id=nullif($3,''),expense_id=$4,version=version+1 WHERE id=$1;
-- name: CountBuyingIntents :one
SELECT count(*) FROM buying_intents WHERE (sqlc.arg(status)::text='all' OR status=sqlc.arg(status)::text) AND (sqlc.arg(search)::text='' OR strpos(lower(title),lower(sqlc.arg(search)::text))>0 OR strpos(lower(notes),lower(sqlc.arg(search)::text))>0);
-- name: ListBuyingIntentIDs :many
SELECT id FROM buying_intents WHERE (sqlc.arg(status)::text='all' OR status=sqlc.arg(status)::text) AND (sqlc.arg(search)::text='' OR strpos(lower(title),lower(sqlc.arg(search)::text))>0 OR strpos(lower(notes),lower(sqlc.arg(search)::text))>0) ORDER BY created_at DESC,id DESC LIMIT sqlc.arg(page_limit)::bigint OFFSET sqlc.arg(page_offset)::bigint;

-- name: UpsertIncome :exec
INSERT INTO incomes(id,account_id,amount_minor,occurred_date,description,version,status) VALUES($1,$2,$3,$4::text::date,$5,$6,'posted') ON CONFLICT(id) DO UPDATE SET account_id=excluded.account_id,amount_minor=excluded.amount_minor,occurred_date=excluded.occurred_date,description=excluded.description,version=excluded.version;
-- name: LockIncome :one
SELECT version,status FROM incomes WHERE id=$1 FOR UPDATE;
-- name: VoidIncome :exec
UPDATE incomes SET status='void',version=version+1 WHERE id=$1;
-- name: ReadIncome :one
SELECT i.id,i.account_id,i.amount_minor,a.currency,i.occurred_date::text AS occurred_date,i.description,i.status,i.version FROM incomes i JOIN accounts a ON a.id=i.account_id WHERE i.id=$1;

-- name: FilteredIncomeIDs :many
SELECT i.id
FROM incomes i JOIN accounts a ON a.id=i.account_id
WHERE (sqlc.arg(account_id)::text='' OR i.account_id=sqlc.arg(account_id))
AND (sqlc.arg(currency)::text='' OR a.currency=sqlc.arg(currency))
AND (sqlc.arg(from_date)::text='' OR i.occurred_date>=NULLIF(sqlc.arg(from_date)::text,'')::date)
AND (sqlc.arg(to_date)::text='' OR i.occurred_date<=NULLIF(sqlc.arg(to_date)::text,'')::date)
AND (sqlc.arg(status)::text='all' OR i.status=sqlc.arg(status))
ORDER BY i.occurred_date DESC,i.id DESC LIMIT sqlc.arg(page_limit)::bigint OFFSET sqlc.arg(page_offset)::bigint;

-- name: FilteredIncomeCount :one
SELECT count(*)
FROM incomes i JOIN accounts a ON a.id=i.account_id
WHERE (sqlc.arg(account_id)::text='' OR i.account_id=sqlc.arg(account_id))
AND (sqlc.arg(currency)::text='' OR a.currency=sqlc.arg(currency))
AND (sqlc.arg(from_date)::text='' OR i.occurred_date>=NULLIF(sqlc.arg(from_date)::text,'')::date)
AND (sqlc.arg(to_date)::text='' OR i.occurred_date<=NULLIF(sqlc.arg(to_date)::text,'')::date)
AND (sqlc.arg(status)::text='all' OR i.status=sqlc.arg(status));

-- name: FilteredIncomeTotals :many
SELECT a.currency,sum(i.amount_minor)::text AS amount
FROM incomes i JOIN accounts a ON a.id=i.account_id
WHERE (sqlc.arg(account_id)::text='' OR i.account_id=sqlc.arg(account_id))
AND (sqlc.arg(currency)::text='' OR a.currency=sqlc.arg(currency))
AND (sqlc.arg(from_date)::text='' OR i.occurred_date>=NULLIF(sqlc.arg(from_date)::text,'')::date)
AND (sqlc.arg(to_date)::text='' OR i.occurred_date<=NULLIF(sqlc.arg(to_date)::text,'')::date)
AND (sqlc.arg(status)::text='all' OR i.status=sqlc.arg(status))
AND i.status='posted' GROUP BY a.currency ORDER BY a.currency;

-- name: UpsertTransfer :exec
INSERT INTO transfers(id,source_account_id,destination_account_id,source_minor,destination_minor,occurred_date,description,version,status) VALUES($1,$2,$3,$4,$5,$6::text::date,$7,$8,'posted') ON CONFLICT(id) DO UPDATE SET source_account_id=excluded.source_account_id,destination_account_id=excluded.destination_account_id,source_minor=excluded.source_minor,destination_minor=excluded.destination_minor,occurred_date=excluded.occurred_date,description=excluded.description,version=excluded.version;
-- name: LockTransfer :one
SELECT version,status FROM transfers WHERE id=$1 FOR UPDATE;
-- name: VoidTransfer :exec
UPDATE transfers SET status='void',version=version+1 WHERE id=$1;
-- name: ReadTransfer :one
SELECT t.id,t.source_account_id,t.destination_account_id,t.source_minor,t.destination_minor,s.currency AS source_currency,d.currency AS destination_currency,t.occurred_date::text AS occurred_date,t.description,t.status,t.version FROM transfers t JOIN accounts s ON s.id=t.source_account_id JOIN accounts d ON d.id=t.destination_account_id WHERE t.id=$1;

-- name: FilteredTransferCount :one
SELECT count(*)
FROM transfers t
WHERE (sqlc.arg(account_id)::text='' OR t.source_account_id=sqlc.arg(account_id) OR t.destination_account_id=sqlc.arg(account_id))
AND (sqlc.arg(from_date)::text='' OR t.occurred_date>=NULLIF(sqlc.arg(from_date)::text,'')::date)
AND (sqlc.arg(to_date)::text='' OR t.occurred_date<=NULLIF(sqlc.arg(to_date)::text,'')::date)
AND (sqlc.arg(status)::text='all' OR t.status=sqlc.arg(status));

-- name: FilteredTransferIDs :many
SELECT t.id
FROM transfers t
WHERE (sqlc.arg(account_id)::text='' OR t.source_account_id=sqlc.arg(account_id) OR t.destination_account_id=sqlc.arg(account_id))
AND (sqlc.arg(from_date)::text='' OR t.occurred_date>=NULLIF(sqlc.arg(from_date)::text,'')::date)
AND (sqlc.arg(to_date)::text='' OR t.occurred_date<=NULLIF(sqlc.arg(to_date)::text,'')::date)
AND (sqlc.arg(status)::text='all' OR t.status=sqlc.arg(status))
ORDER BY t.occurred_date DESC,t.id DESC LIMIT sqlc.arg(page_limit)::bigint OFFSET sqlc.arg(page_offset)::bigint;

-- name: UpsertReceivable :exec
INSERT INTO receivables(id,debtor,amount_minor,currency,due_date,notes,version,status) VALUES(sqlc.arg(id),sqlc.arg(debtor),sqlc.arg(amount_minor),sqlc.arg(currency),NULLIF(sqlc.arg(due_date)::text,'')::date,sqlc.arg(notes),sqlc.arg(version),'active') ON CONFLICT(id) DO UPDATE SET debtor=EXCLUDED.debtor,amount_minor=EXCLUDED.amount_minor,currency=EXCLUDED.currency,due_date=EXCLUDED.due_date,notes=EXCLUDED.notes,version=EXCLUDED.version;
-- name: LockReceivable :one
SELECT version,status FROM receivables WHERE id=$1 FOR UPDATE;
-- name: VoidReceivable :exec
UPDATE receivables SET status='void',version=version+1 WHERE id=$1;
-- name: BumpReceivable :exec
UPDATE receivables SET version=version+1 WHERE id=$1;
-- name: ReadReceivable :one
SELECT d.id,d.debtor,d.amount_minor,d.currency,COALESCE(d.due_date::text,'')::text AS due_date,d.notes,d.version,CASE WHEN d.status='void' THEN 'void' WHEN COALESCE(r.amount,0)=d.amount_minor THEN 'settled' ELSE 'open' END::text AS status,COALESCE(r.amount,0)::text AS repaid FROM receivables d LEFT JOIN (SELECT receivable_id,sum(amount_minor) amount FROM repayments WHERE status='posted' GROUP BY receivable_id) r ON r.receivable_id=d.id WHERE d.id=$1;
-- name: UpsertRepayment :exec
INSERT INTO repayments(id,receivable_id,account_id,amount_minor,occurred_date,description,version,status) VALUES(sqlc.arg(id),sqlc.arg(receivable_id),sqlc.arg(account_id),sqlc.arg(amount_minor),sqlc.arg(occurred_date)::text::date,sqlc.arg(description),sqlc.arg(version),'posted') ON CONFLICT(id) DO UPDATE SET account_id=EXCLUDED.account_id,amount_minor=EXCLUDED.amount_minor,occurred_date=EXCLUDED.occurred_date,description=EXCLUDED.description,version=EXCLUDED.version;
-- name: LockRepayment :one
SELECT version,status,receivable_id,amount_minor FROM repayments WHERE id=$1 FOR UPDATE;
-- name: VoidRepayment :exec
UPDATE repayments SET status='void',version=version+1 WHERE id=$1;
-- name: ReadRepayment :one
SELECT r.id,r.receivable_id,r.account_id,a.currency,r.amount_minor,r.occurred_date::text AS occurred_date,r.description,r.status,r.version FROM repayments r JOIN accounts a ON a.id=r.account_id WHERE r.id=$1;

-- name: FilteredReceivableIDs :many
SELECT d.id FROM receivables d LEFT JOIN (SELECT receivable_id,sum(amount_minor) amount FROM repayments WHERE status='posted' GROUP BY receivable_id) r ON r.receivable_id=d.id
WHERE (sqlc.arg(currency)::text='' OR d.currency=sqlc.arg(currency)) AND (sqlc.arg(debtor)::text='' OR d.debtor=sqlc.arg(debtor)) AND (sqlc.arg(status)::text='all' OR CASE WHEN d.status='void' THEN 'void' WHEN COALESCE(r.amount,0)=d.amount_minor THEN 'settled' ELSE 'open' END=sqlc.arg(status)) ORDER BY d.created_at DESC,d.id DESC LIMIT sqlc.arg(page_limit)::bigint OFFSET sqlc.arg(page_offset)::bigint;

-- name: FilteredReceivableCount :one
SELECT count(*) FROM receivables d LEFT JOIN (SELECT receivable_id,sum(amount_minor) amount FROM repayments WHERE status='posted' GROUP BY receivable_id) r ON r.receivable_id=d.id
WHERE (sqlc.arg(currency)::text='' OR d.currency=sqlc.arg(currency)) AND (sqlc.arg(debtor)::text='' OR d.debtor=sqlc.arg(debtor)) AND (sqlc.arg(status)::text='all' OR CASE WHEN d.status='void' THEN 'void' WHEN COALESCE(r.amount,0)=d.amount_minor THEN 'settled' ELSE 'open' END=sqlc.arg(status));

-- name: FilteredReceivableTotals :many
SELECT d.currency,sum(d.amount_minor::numeric-COALESCE(r.amount,0))::text AS amount FROM receivables d LEFT JOIN (SELECT receivable_id,sum(amount_minor) amount FROM repayments WHERE status='posted' GROUP BY receivable_id) r ON r.receivable_id=d.id
WHERE (sqlc.arg(currency)::text='' OR d.currency=sqlc.arg(currency)) AND (sqlc.arg(debtor)::text='' OR d.debtor=sqlc.arg(debtor)) AND (sqlc.arg(status)::text='all' OR CASE WHEN d.status='void' THEN 'void' WHEN COALESCE(r.amount,0)=d.amount_minor THEN 'settled' ELSE 'open' END=sqlc.arg(status)) AND d.status='active' GROUP BY d.currency ORDER BY d.currency;

-- name: FilteredRepaymentIDs :many
SELECT r.id FROM repayments r WHERE (sqlc.arg(receivable_id)::text='' OR r.receivable_id=sqlc.arg(receivable_id)) AND (sqlc.arg(account_id)::text='' OR r.account_id=sqlc.arg(account_id)) AND (sqlc.arg(status)::text='all' OR r.status=sqlc.arg(status)) ORDER BY r.occurred_date DESC,r.id DESC LIMIT sqlc.arg(page_limit)::bigint OFFSET sqlc.arg(page_offset)::bigint;

-- name: FilteredRepaymentCount :one
SELECT count(*) FROM repayments r WHERE (sqlc.arg(receivable_id)::text='' OR r.receivable_id=sqlc.arg(receivable_id)) AND (sqlc.arg(account_id)::text='' OR r.account_id=sqlc.arg(account_id)) AND (sqlc.arg(status)::text='all' OR r.status=sqlc.arg(status));

-- name: AccountBalanceAsOf :one
SELECT (a.opening_minor::numeric
-COALESCE((SELECT sum(debit_minor) FROM expenses WHERE account_id=a.id AND status='posted' AND occurred_date<=sqlc.arg(as_of_date)::text::date),0)
+COALESCE((SELECT sum(amount_minor) FROM incomes WHERE account_id=a.id AND status='posted' AND occurred_date<=sqlc.arg(as_of_date)::text::date),0)
-COALESCE((SELECT sum(source_minor) FROM transfers WHERE source_account_id=a.id AND status='posted' AND occurred_date<=sqlc.arg(as_of_date)::text::date),0)
+COALESCE((SELECT sum(destination_minor) FROM transfers WHERE destination_account_id=a.id AND status='posted' AND occurred_date<=sqlc.arg(as_of_date)::text::date),0)
+COALESCE((SELECT sum(amount_minor) FROM repayments WHERE account_id=a.id AND status='posted' AND occurred_date<=sqlc.arg(as_of_date)::text::date),0)
+COALESCE((SELECT sum(amount_minor) FROM balance_adjustments WHERE account_id=a.id AND status='posted' AND occurred_date<=sqlc.arg(as_of_date)::text::date),0))::text AS balance
FROM accounts a WHERE a.id=sqlc.arg(account_id);

-- name: ReadBalanceObservation :one
SELECT o.id,o.account_id,a.currency,o.as_of_date::text AS as_of_date,o.actual_minor,o.recorded_ledger_minor::text AS recorded_ledger_minor,o.basis,o.notes,o.status,o.version FROM balance_observations o JOIN accounts a ON a.id=o.account_id WHERE o.id=$1;
-- name: LockBalanceObservation :one
SELECT version,status FROM balance_observations WHERE id=$1 FOR UPDATE;
-- name: HasPostedBalanceAdjustment :one
SELECT EXISTS(SELECT 1 FROM balance_adjustments WHERE observation_id=$1 AND status='posted')::boolean AS present;
-- name: UpsertBalanceObservation :exec
INSERT INTO balance_observations(id,account_id,as_of_date,actual_minor,recorded_ledger_minor,basis,notes,version) VALUES(sqlc.arg(id),sqlc.arg(account_id),sqlc.arg(as_of_date)::text::date,sqlc.arg(actual_minor),sqlc.arg(recorded_ledger_minor)::text::numeric,sqlc.arg(basis),sqlc.arg(notes),sqlc.arg(version)) ON CONFLICT(id) DO UPDATE SET account_id=excluded.account_id,as_of_date=excluded.as_of_date,actual_minor=excluded.actual_minor,recorded_ledger_minor=excluded.recorded_ledger_minor,basis=excluded.basis,notes=excluded.notes,version=excluded.version;
-- name: VoidBalanceObservation :exec
UPDATE balance_observations SET status='void',version=version+1 WHERE id=$1;
-- name: BumpBalanceObservation :exec
UPDATE balance_observations SET version=version+1 WHERE id=$1;
-- name: ReadBalanceAdjustment :one
SELECT b.id,b.observation_id,b.account_id,a.currency,b.amount_minor,b.occurred_date::text AS occurred_date,b.reason,b.status,b.version FROM balance_adjustments b JOIN accounts a ON a.id=b.account_id WHERE b.id=$1;
-- name: InsertBalanceAdjustment :exec
INSERT INTO balance_adjustments(id,observation_id,account_id,amount_minor,occurred_date,reason,version) VALUES(sqlc.arg(id),sqlc.arg(observation_id),sqlc.arg(account_id),sqlc.arg(amount_minor),sqlc.arg(occurred_date)::text::date,sqlc.arg(reason),1);
-- name: LockBalanceAdjustment :one
SELECT version,status FROM balance_adjustments WHERE id=$1 FOR UPDATE;
-- name: VoidBalanceAdjustment :exec
UPDATE balance_adjustments SET status='void',version=version+1 WHERE id=$1;
-- name: FilteredBalanceObservationCount :one
SELECT count(*) FROM balance_observations WHERE (sqlc.arg(account_id)::text='' OR account_id=sqlc.arg(account_id));
-- name: FilteredBalanceObservationIDs :many
SELECT id FROM balance_observations WHERE (sqlc.arg(account_id)::text='' OR account_id=sqlc.arg(account_id)) ORDER BY as_of_date DESC,id DESC LIMIT sqlc.arg(page_limit)::bigint OFFSET sqlc.arg(page_offset)::bigint;
-- name: FilteredBalanceAdjustmentCount :one
SELECT count(*) FROM balance_adjustments WHERE (sqlc.arg(account_id)::text='' OR account_id=sqlc.arg(account_id)) AND (sqlc.arg(observation_id)::text='' OR observation_id=sqlc.arg(observation_id)) AND (sqlc.arg(status)::text='all' OR status=sqlc.arg(status));
-- name: FilteredBalanceAdjustmentIDs :many
SELECT id FROM balance_adjustments WHERE (sqlc.arg(account_id)::text='' OR account_id=sqlc.arg(account_id)) AND (sqlc.arg(observation_id)::text='' OR observation_id=sqlc.arg(observation_id)) AND (sqlc.arg(status)::text='all' OR status=sqlc.arg(status)) ORDER BY occurred_date DESC,id DESC LIMIT sqlc.arg(page_limit)::bigint OFFSET sqlc.arg(page_offset)::bigint;

-- name: ExpenseBalanceQualityAsOf :one
SELECT count(*) FILTER(WHERE debit_state='estimated') AS estimated_count,count(*) FILTER(WHERE debit_state='unresolved') AS unresolved_count FROM expenses WHERE account_id=sqlc.arg(account_id) AND status='posted' AND occurred_date<=sqlc.arg(as_of_date)::text::date;

-- name: MonthlyIncomeTotals :many
SELECT a.currency,sum(i.amount_minor)::text AS amount FROM incomes i JOIN accounts a ON a.id=i.account_id WHERE i.status='posted' AND i.occurred_date>=sqlc.arg(from_date)::text::date AND i.occurred_date<=sqlc.arg(to_date)::text::date AND (sqlc.arg(account_id)::text='' OR i.account_id=sqlc.arg(account_id)) GROUP BY a.currency ORDER BY a.currency;
-- name: MonthlySpendingTotals :many
SELECT e.currency,sum(e.amount_minor)::text AS amount FROM expenses e WHERE e.status='posted' AND e.occurred_date>=sqlc.arg(from_date)::text::date AND e.occurred_date<=sqlc.arg(to_date)::text::date AND (sqlc.arg(account_id)::text='' OR e.account_id=sqlc.arg(account_id)) GROUP BY e.currency ORDER BY e.currency;
-- name: MonthlyCategoryTotals :many
SELECT c.id,c.name,e.currency,sum(l.amount_minor)::text AS amount FROM allocations l JOIN expenses e ON e.id=l.expense_id JOIN categories c ON c.id=l.category_id WHERE e.status='posted' AND e.occurred_date>=sqlc.arg(from_date)::text::date AND e.occurred_date<=sqlc.arg(to_date)::text::date AND (sqlc.arg(account_id)::text='' OR e.account_id=sqlc.arg(account_id)) GROUP BY c.id,c.name,e.currency ORDER BY c.name,c.id,e.currency;
-- name: MonthlyProjectTotals :many
SELECT COALESCE(p.id,'')::text AS id,COALESCE(p.name,'')::text AS name,e.currency,sum(l.amount_minor)::text AS amount FROM allocations l JOIN expenses e ON e.id=l.expense_id LEFT JOIN projects p ON p.id=l.project_id WHERE e.status='posted' AND e.occurred_date>=sqlc.arg(from_date)::text::date AND e.occurred_date<=sqlc.arg(to_date)::text::date AND (sqlc.arg(account_id)::text='' OR e.account_id=sqlc.arg(account_id)) GROUP BY p.id,p.name,e.currency ORDER BY p.name NULLS FIRST,p.id,e.currency;
-- name: MonthlyAccountMovements :many
WITH movement AS (
 SELECT account_id,'expense'::text AS kind,debit_minor::numeric AS amount,debit_state FROM expenses WHERE status='posted' AND occurred_date>=sqlc.arg(from_date)::text::date AND occurred_date<=sqlc.arg(to_date)::text::date
 UNION ALL SELECT account_id,'income',amount_minor::numeric,'exact' FROM incomes WHERE status='posted' AND occurred_date>=sqlc.arg(from_date)::text::date AND occurred_date<=sqlc.arg(to_date)::text::date
 UNION ALL SELECT source_account_id,'transfer_out',source_minor::numeric,'exact' FROM transfers WHERE status='posted' AND occurred_date>=sqlc.arg(from_date)::text::date AND occurred_date<=sqlc.arg(to_date)::text::date
 UNION ALL SELECT destination_account_id,'transfer_in',destination_minor::numeric,'exact' FROM transfers WHERE status='posted' AND occurred_date>=sqlc.arg(from_date)::text::date AND occurred_date<=sqlc.arg(to_date)::text::date
 UNION ALL SELECT account_id,'repayment',amount_minor::numeric,'exact' FROM repayments WHERE status='posted' AND occurred_date>=sqlc.arg(from_date)::text::date AND occurred_date<=sqlc.arg(to_date)::text::date
 UNION ALL SELECT account_id,'adjustment',amount_minor::numeric,'exact' FROM balance_adjustments WHERE status='posted' AND occurred_date>=sqlc.arg(from_date)::text::date AND occurred_date<=sqlc.arg(to_date)::text::date
)
SELECT a.id,a.name,a.currency,
 COALESCE(sum(m.amount) FILTER(WHERE m.kind='expense'),0)::text AS expense_debits,
 COALESCE(sum(m.amount) FILTER(WHERE m.kind='income'),0)::text AS income,
 COALESCE(sum(m.amount) FILTER(WHERE m.kind='transfer_in'),0)::text AS transfer_in,
 COALESCE(sum(m.amount) FILTER(WHERE m.kind='transfer_out'),0)::text AS transfer_out,
 COALESCE(sum(m.amount) FILTER(WHERE m.kind='repayment'),0)::text AS repayments,
 COALESCE(sum(m.amount) FILTER(WHERE m.kind='adjustment'),0)::text AS adjustments,
 COALESCE(sum(CASE WHEN m.kind IN ('expense','transfer_out') THEN -m.amount ELSE m.amount END),0)::text AS net_change,
 count(*) FILTER(WHERE m.debit_state='estimated') AS estimated_count,
 count(*) FILTER(WHERE m.debit_state='unresolved') AS unresolved_count
 FROM accounts a LEFT JOIN movement m ON m.account_id=a.id
 WHERE a.opening_date<=sqlc.arg(to_date)::text::date AND (sqlc.arg(account_id)::text='' OR a.id=sqlc.arg(account_id))
 GROUP BY a.id,a.name,a.currency ORDER BY a.name,a.id;

-- name: ReadProject :one
SELECT id,name,version,archived FROM projects WHERE id=$1;

-- name: UpdateAccountLifecycle :exec
UPDATE accounts SET name=$2,archived=$3,version=version+1 WHERE id=$1;
