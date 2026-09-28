package ledger

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func mixedItems(a, b Account, c, p Named) []BulkTransactionItem {
	return []BulkTransactionItem{
		{Kind: "expense", Action: "create", Expense: intentDraft(a, c, p)},
		{Kind: "income", Action: "create", Income: &IncomeDraft{AccountID: a.ID, Currency: a.Currency, Amount: "200", OccurredDate: "2026-09-27"}},
		{Kind: "transfer", Action: "create", Transfer: &TransferDraft{SourceAccountID: a.ID, DestinationAccountID: b.ID, SourceCurrency: a.Currency, DestinationCurrency: b.Currency, SourceAmount: "100", DestinationAmount: "100", OccurredDate: "2026-09-27"}},
	}
}
func TestBulkTransactionsLifecycleAndConcurrentReplay(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	b := transferAccount(t, s, ctx, "destination", "RON")
	in := BulkTransactionsInput{OperationKey: "mixed-create", Items: mixedItems(a, b, c, p)}
	var wg sync.WaitGroup
	results := make([]BulkTransactionsResult, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = s.BulkTransactions(ctx, in) }(i)
	}
	wg.Wait()
	for i := range results {
		if errs[i] != nil || !reflect.DeepEqual(results[i], results[0]) {
			t.Fatalf("replay %d %+v %v", i, results[i], errs[i])
		}
	}
	created := results[0].Results
	if len(created) != 3 || created[0].Expense == nil || created[1].Income == nil || created[2].Transfer == nil {
		t.Fatalf("results %+v", created)
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "670.00", b.ID: "1100.00"})
	in.Items[1].Income.Amount = "201"
	if _, e := s.BulkTransactions(ctx, in); !errors.Is(e, ErrConflict) {
		t.Fatalf("changed payload %v", e)
	}
	in.OperationKey = "mixed-amend"
	in.Items[0].ID = created[0].Expense.ID
	in.Items[1].ID = created[1].Income.ID
	in.Items[2].ID = created[2].Transfer.ID
	for i := range in.Items {
		in.Items[i].Action = "amend"
		in.Items[i].ExpectedVersion = 1
	}
	in.Items[0].Expense.Amount = "300"
	in.Items[0].Expense.Allocations = []AllocationInput{{CategoryID: c.ID, Remainder: true}}
	in.Items[1].Income.Amount = "400"
	in.Items[2].Transfer.SourceAmount = "50"
	in.Items[2].Transfer.DestinationAmount = "50"
	amended, e := s.BulkTransactions(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if amended.Results[0].Expense.Version != 2 || amended.Results[1].Income.Version != 2 || amended.Results[2].Transfer.Version != 2 {
		t.Fatalf("versions %+v", amended)
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "1050.00", b.ID: "1050.00"})
	in.OperationKey = "mixed-void"
	for i := range in.Items {
		in.Items[i].Action = "void"
		in.Items[i].ExpectedVersion = 2
		in.Items[i].Expense = nil
		in.Items[i].Income = nil
		in.Items[i].Transfer = nil
	}
	out, e := s.BulkTransactions(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if out.Results[0].Expense.Status != "void" || out.Results[1].Income.Status != "void" || out.Results[2].Transfer.Status != "void" {
		t.Fatalf("void %+v", out)
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "1000.00", b.ID: "1000.00"})
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM audit_entries WHERE action LIKE 'expense_%' OR action LIKE 'income_%' OR action LIKE 'transfer_%'").Scan(&count); e != nil || count != 9 {
		t.Fatalf("audit %d %v", count, e)
	}
}
func TestBulkTransactionsRollbackAndIndexedIssues(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	b := transferAccount(t, s, ctx, "destination", "RON")
	existing, e := s.BulkTransactions(ctx, BulkTransactionsInput{OperationKey: "existing", Items: mixedItems(a, b, c, p)})
	if e != nil {
		t.Fatal(e)
	}
	in := BulkTransactionsInput{OperationKey: "rollback", Items: mixedItems(a, b, c, p)}
	in.Items = append(in.Items,
		BulkTransactionItem{Kind: "expense", Action: "void", ID: existing.Results[0].Expense.ID, ExpectedVersion: 99},
		BulkTransactionItem{Kind: "income", Action: "void", ID: existing.Results[1].Income.ID, ExpectedVersion: 99},
		BulkTransactionItem{Kind: "transfer", Action: "void", ID: existing.Results[2].Transfer.ID, ExpectedVersion: 99},
		BulkTransactionItem{Kind: "income", Action: "void", ID: "missing", ExpectedVersion: 1},
		BulkTransactionItem{Kind: "income", Action: "create", Income: &IncomeDraft{Amount: "invalid"}},
	)
	out, e := s.BulkTransactions(ctx, in)
	var issues *BulkValidationError
	if !errors.As(e, &issues) || len(issues.Issues) != 5 || len(out.Results) != 0 {
		t.Fatalf("issues %+v %v", out, e)
	}
	for i, code := range []string{"conflict", "conflict", "conflict", "not_found", "validation"} {
		if issues.Issues[i].Index != i+3 || issues.Issues[i].Code != code {
			t.Fatalf("issue %+v", issues.Issues[i])
		}
	}
	var expenses, incomes, transfers, audits, keys int
	e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM expenses),(SELECT count(*) FROM incomes),(SELECT count(*) FROM transfers),(SELECT count(*) FROM audit_entries WHERE action LIKE 'expense_%' OR action LIKE 'income_%' OR action LIKE 'transfer_%'),(SELECT count(*) FROM operation_results WHERE operation_key='rollback')").Scan(&expenses, &incomes, &transfers, &audits, &keys)
	if e != nil || expenses != 1 || incomes != 1 || transfers != 1 || audits != 3 || keys != 0 {
		t.Fatalf("rollback %d %d %d %d %d %v", expenses, incomes, transfers, audits, keys, e)
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "670.00", b.ID: "1100.00"})
	// Successful corrections and a void must also roll back if a later item fails.
	updates := mixedItems(a, b, c, p)
	updates[0].Action, updates[0].ID, updates[0].ExpectedVersion = "amend", existing.Results[0].Expense.ID, 1
	updates[0].Expense.Description = "must roll back"
	updates[1].Action, updates[1].ID, updates[1].ExpectedVersion = "amend", existing.Results[1].Income.ID, 1
	updates[1].Income.Amount = "999"
	updates[2] = BulkTransactionItem{Kind: "transfer", Action: "void", ID: existing.Results[2].Transfer.ID, ExpectedVersion: 1}
	updates = append(updates, BulkTransactionItem{Kind: "income", Action: "create", Income: &IncomeDraft{Amount: "invalid"}})
	if _, err := s.BulkTransactions(ctx, BulkTransactionsInput{OperationKey: "rollback-updates", Items: updates}); !errors.As(err, &issues) {
		t.Fatalf("update rollback %v", err)
	}
	var unchanged bool
	if err := s.pool.QueryRow(ctx, "SELECT (SELECT version=1 AND description <> 'must roll back' FROM expenses LIMIT 1) AND (SELECT version=1 AND amount_minor=20000 FROM incomes LIMIT 1) AND (SELECT version=1 AND status='posted' FROM transfers LIMIT 1) AND NOT EXISTS (SELECT 1 FROM operation_results WHERE operation_key='rollback-updates')").Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("update rollback state %v %v", unchanged, err)
	}
	in.Items = in.Items[:3]
	if _, e = s.BulkTransactions(ctx, in); e != nil {
		t.Fatalf("corrected key %v", e)
	}
}
func TestBulkTransactionsShapes(t *testing.T) {
	s, ctx := testService(t)
	for _, items := range [][]BulkTransactionItem{nil, make([]BulkTransactionItem, 101)} {
		if _, e := s.BulkTransactions(ctx, BulkTransactionsInput{OperationKey: "bound", Items: items}); !errors.Is(e, ErrValidation) {
			t.Fatalf("bound %v", e)
		}
	}
	items := []BulkTransactionItem{
		{Kind: "unknown", Action: "create"},
		{Kind: "income", Action: "create", Expense: &ExpenseDraft{}},
		{Kind: "income", Action: "create", Income: &IncomeDraft{}, Transfer: &TransferDraft{}},
		{Kind: "transfer", Action: "create", Transfer: &TransferDraft{}, ID: "id"},
		{Kind: "expense", Action: "amend", Expense: &ExpenseDraft{}, ID: "id"},
		{Kind: "income", Action: "void", Income: &IncomeDraft{}, ID: "id", ExpectedVersion: 1},
		{Kind: "expense", Action: "void", ID: "duplicate", ExpectedVersion: 1},
		{Kind: "expense", Action: "void", ID: "duplicate", ExpectedVersion: 1},
		{Kind: "income", Action: "void", ID: "duplicate", ExpectedVersion: 1},
	}
	_, e := s.BulkTransactions(ctx, BulkTransactionsInput{OperationKey: "shapes", Items: items})
	var issues *BulkValidationError
	if !errors.As(e, &issues) || len(issues.Issues) != len(items) {
		t.Fatalf("shapes %v", e)
	}
	for i, issue := range issues.Issues {
		want := "validation"
		if i == 6 || i == 8 {
			want = "not_found"
		}
		if issue.Index != i || issue.Code != want {
			t.Fatalf("issue %+v want %s", issue, want)
		}
	}
}
