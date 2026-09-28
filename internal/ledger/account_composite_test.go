package ledger

import (
	"errors"
	"reflect"
	"testing"
)

func TestArchivedAccountCompositeWritesAreAtomic(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	b := transferAccount(t, s, ctx, "active", "RON")
	existing, err := s.CreateExpense(ctx, expenseInput(a, c, p))
	if err != nil {
		t.Fatal(err)
	}
	intent, err := s.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "intent", Title: "Planned purchase"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SetAccountArchived(ctx, SetAccountArchivedInput{OperationKey: "archive", ID: a.ID, ExpectedVersion: 1, Archived: true})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Dashboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	counts := func() [2]int {
		t.Helper()
		var n [2]int
		if err := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM audit_entries),(SELECT count(*) FROM operation_results)").Scan(&n[0], &n[1]); err != nil {
			t.Fatal(err)
		}
		return n
	}
	baseline := counts()
	cases := []struct {
		name string
		run  func() error
	}{
		{"expense bulk", func() error {
			_, e := s.BulkExpenses(ctx, BulkExpensesInput{OperationKey: "expense-batch", Items: []BulkExpenseItem{{Action: "create", Expense: intentDraft(b, c, p)}, {Action: "create", Expense: intentDraft(a, c, p)}}})
			return e
		}},
		{"mixed bulk", func() error {
			_, e := s.BulkTransactions(ctx, BulkTransactionsInput{OperationKey: "mixed-batch", Items: []BulkTransactionItem{{Kind: "income", Action: "create", Income: &IncomeDraft{AccountID: b.ID, Currency: "RON", Amount: "100", OccurredDate: "2026-09-27"}}, {Kind: "expense", Action: "create", Expense: intentDraft(a, c, p)}}})
			return e
		}},
		{"intent purchase", func() error {
			_, e := s.ConfirmIntentPurchase(ctx, ConfirmIntentPurchaseInput{OperationKey: "purchase", IntentID: intent.ID, ExpectedVersion: 1, NewExpense: intentDraft(a, c, p)})
			return e
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if e := tc.run(); !errors.Is(e, ErrValidation) {
				t.Fatalf("expected archive rejection: %v", e)
			}
			after, e := s.Dashboard(ctx)
			if e != nil || !reflect.DeepEqual(before, after) || counts() != baseline {
				t.Fatalf("partial write after rejection: %v", e)
			}
		})
	}
	// Rejected confirmation leaves the intent version unchanged; linking existing
	// history is permitted even though new spending on that account is forbidden.
	purchased, err := s.ConfirmIntentPurchase(ctx, ConfirmIntentPurchaseInput{OperationKey: "link-history", IntentID: intent.ID, ExpectedVersion: 1, ExpenseID: existing.ID, ExpectedExpenseVersion: 1})
	if err != nil || purchased.Status != "purchased" || purchased.Expense.ID != existing.ID {
		t.Fatalf("historical link: %+v %v", purchased, err)
	}
}
