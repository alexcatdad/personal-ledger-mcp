package ledger

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestBulkExpensesAtomicErrorsAndRetry(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	existing, e := s.CreateExpense(ctx, expenseInput(a, c, p))
	if e != nil {
		t.Fatal(e)
	}
	bad := intentDraft(a, c, p)
	bad.Amount = "500"
	in := BulkExpensesInput{OperationKey: "bulk", Items: []BulkExpenseItem{
		{Action: "create", Expense: intentDraft(a, c, p)},
		{Action: "create", Expense: bad},
		{Action: "void", ID: existing.ID, ExpectedVersion: 99},
		{Action: "void", ID: "missing", ExpectedVersion: 1},
	}}
	var beforeAudit int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM audit_entries").Scan(&beforeAudit); e != nil {
		t.Fatal(e)
	}
	out, e := s.BulkExpenses(ctx, in)
	var validation *BulkValidationError
	if !errors.As(e, &validation) || !errors.Is(e, ErrValidation) || len(validation.Issues) != 3 || len(out.Expenses) != 0 {
		t.Fatalf("errors: %+v %v", out, e)
	}
	for i, code := range []string{"validation", "conflict", "not_found"} {
		if validation.Issues[i].Index != i+1 || validation.Issues[i].Code != code {
			t.Fatalf("issue %+v", validation.Issues[i])
		}
	}
	var audits, operations int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM audit_entries").Scan(&audits); e != nil {
		t.Fatal(e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM operation_results WHERE operation_key='bulk'").Scan(&operations); e != nil {
		t.Fatal(e)
	}
	d, e := s.Dashboard(ctx)
	if e != nil || audits != beforeAudit || operations != 0 || len(d.Expenses) != 1 || d.Accounts[0].Balance != "570.00" {
		t.Fatalf("rollback audits=%d operations=%d dashboard=%+v err=%v", audits, operations, d, e)
	}
	// A rejected key remains reusable with a corrected complete payload.
	in.Items = []BulkExpenseItem{{Action: "create", Expense: intentDraft(a, c, p)}, {Action: "void", ID: existing.ID, ExpectedVersion: 1}}
	out, e = s.BulkExpenses(ctx, in)
	if e != nil || len(out.Expenses) != 2 || out.Expenses[1].Status != "void" {
		t.Fatalf("corrected %+v %v", out, e)
	}
	retry, e := s.BulkExpenses(ctx, in)
	if e != nil || !reflect.DeepEqual(retry, out) {
		t.Fatalf("retry %+v %v", retry, e)
	}
	in.Items = in.Items[:1]
	if _, e = s.BulkExpenses(ctx, in); !errors.Is(e, ErrConflict) {
		t.Fatalf("payload mismatch %v", e)
	}
	d, e = s.Dashboard(ctx)
	if e != nil || d.Accounts[0].Balance != "570.00" || len(d.Expenses) != 2 {
		t.Fatalf("retry balance %+v %v", d, e)
	}
}
func TestBulkExpensesMixedConcurrentRetry(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	first, e := s.CreateExpense(ctx, expenseInput(a, c, p))
	if e != nil {
		t.Fatal(e)
	}
	secondIn := expenseInput(a, c, p)
	secondIn.OperationKey = "second"
	second, e := s.CreateExpense(ctx, secondIn)
	if e != nil {
		t.Fatal(e)
	}
	amended := intentDraft(a, c, p)
	amended.Amount = "300"
	amended.Allocations = []AllocationInput{{CategoryID: c.ID, ProjectID: p.ID, Remainder: true}}
	in := BulkExpensesInput{OperationKey: "mixed", Items: []BulkExpenseItem{{Action: "create", Expense: intentDraft(a, c, p)}, {Action: "amend", ID: first.ID, ExpectedVersion: 1, Expense: amended}, {Action: "void", ID: second.ID, ExpectedVersion: 1}}}
	var wg sync.WaitGroup
	results := make([]BulkExpensesResult, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = s.BulkExpenses(ctx, in) }(i)
	}
	wg.Wait()
	for i := range results {
		if errs[i] != nil || !reflect.DeepEqual(results[i], results[0]) {
			t.Fatalf("concurrent retry %d %+v %v", i, results[i], errs[i])
		}
	}
	out := results[0].Expenses
	if len(out) != 3 || out[1].ID != first.ID || out[1].Version != 2 || out[1].Amount != "300.00" || out[2].Status != "void" {
		t.Fatalf("mixed %+v", out)
	}
	d, e := s.Dashboard(ctx)
	if e != nil || len(d.Expenses) != 3 || d.Accounts[0].Balance != "270.00" {
		t.Fatalf("balance %+v %v", d, e)
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM audit_entries WHERE action LIKE 'expense_%'").Scan(&count); e != nil || count != 5 {
		t.Fatalf("audit %d %v", count, e)
	}
}
func TestBulkExpensesShapes(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	for _, items := range [][]BulkExpenseItem{nil, make([]BulkExpenseItem, 101)} {
		if _, e := s.BulkExpenses(ctx, BulkExpensesInput{OperationKey: "bound", Items: items}); !errors.Is(e, ErrValidation) {
			t.Fatalf("bound %v", e)
		}
	}
	in := BulkExpensesInput{OperationKey: "shape", Items: []BulkExpenseItem{
		{Action: "other"}, {Action: "create"}, {Action: "create", Expense: intentDraft(a, c, p), ExpectedVersion: 1},
		{Action: "amend", Expense: intentDraft(a, c, p), ID: "one"},
		{Action: "void", ID: "two", ExpectedVersion: 1, Expense: intentDraft(a, c, p)},
		{Action: "void", ID: "two", ExpectedVersion: 1},
	}}
	_, e := s.BulkExpenses(ctx, in)
	var validation *BulkValidationError
	if !errors.As(e, &validation) || len(validation.Issues) != len(in.Items) {
		t.Fatalf("shape %v", e)
	}
	for i, issue := range validation.Issues {
		if issue.Index != i || issue.Code != "validation" {
			t.Fatalf("issue %+v", issue)
		}
	}
}
