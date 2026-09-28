package ledger

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestExpenseQueryValidation(t *testing.T) {
	for _, in := range []ExpenseQuery{{Limit: -1}, {Limit: 101}, {Offset: -1}, {Status: "pending"}, {Currency: "GBP"}, {FromDate: "bad"}, {ToDate: "2026-02-30"}, {FromDate: "2026-09-02", ToDate: "2026-09-01"}} {
		if _, err := (&Service{}).QueryExpenses(context.Background(), in); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v: %v", in, err)
		}
	}
}
func TestExpenseQueries(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	other, err := s.CreateCategory(ctx, CreateNamedInput{OperationKey: "other-cat", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	in := expenseInput(a, c, p)
	in.Allocations[1].CategoryID = other.ID
	in.Allocations[1].ProjectID = ""
	first, err := s.CreateExpense(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		in.OperationKey = fmt.Sprintf("extra-%d", i)
		in.OccurredDate = "2026-09-28"
		if _, err = s.CreateExpense(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	euro, err := s.CreateAccount(ctx, CreateAccountInput{OperationKey: "euro", Name: "Euro", Currency: "EUR", OpeningAmount: "0", OpeningDate: "2026-09-01"})
	if err != nil {
		t.Fatal(err)
	}
	in.OperationKey = "eur-exp"
	in.AccountID = euro.ID
	in.Currency = "EUR"
	if _, err = s.CreateExpense(ctx, in); err != nil {
		t.Fatal(err)
	}
	page, err := s.QueryExpenses(ctx, ExpenseQuery{Limit: 1})
	if err != nil || len(page.Expenses) != 1 || page.TotalCount != 5 || len(page.Totals) != 2 || page.Totals[0].Amount != "430.00" || page.Totals[1].Amount != "1720.00" {
		t.Fatalf("page: %+v %v", page, err)
	}
	next, err := s.QueryExpenses(ctx, ExpenseQuery{Limit: 1, Offset: 1})
	if err != nil || next.Expenses[0].ID == page.Expenses[0].ID {
		t.Fatalf("next: %+v %v", next, err)
	}
	empty, err := s.QueryExpenses(ctx, ExpenseQuery{Offset: 100})
	if err != nil || len(empty.Expenses) != 0 || empty.TotalCount != 5 || len(empty.Totals) != 2 {
		t.Fatalf("empty page: %+v %v", empty, err)
	}
	match, err := s.QueryExpenses(ctx, ExpenseQuery{AccountID: a.ID, Currency: "RON", FromDate: "2026-09-27", ToDate: "2026-09-27", CategoryID: c.ID, ProjectID: p.ID})
	if err != nil || match.TotalCount != 1 || match.Expenses[0].ID != first.ID || match.Totals[0].Amount != "430.00" || len(match.MatchingAllocationTotals) != 1 || match.MatchingAllocationTotals[0].Amount != "250.00" {
		t.Fatalf("filters: %+v %v", match, err)
	}
	noMatch, err := s.QueryExpenses(ctx, ExpenseQuery{CategoryID: other.ID, ProjectID: p.ID})
	if err != nil || noMatch.TotalCount != 0 || len(noMatch.Totals) != 0 {
		t.Fatalf("cross allocation: %+v %v", noMatch, err)
	}
	if _, err = s.VoidExpense(ctx, VoidExpenseInput{OperationKey: "void", ID: first.ID, ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"", "all", "void"} {
		result, err := s.QueryExpenses(ctx, ExpenseQuery{Status: status, ToDate: "2026-09-27"})
		if err != nil {
			t.Fatal(err)
		}
		expected := int64(1)
		if status == "" {
			expected = 0
		}
		if result.TotalCount != expected || len(result.Totals) != 0 || len(result.MatchingAllocationTotals) != 0 {
			t.Fatalf("status %s: %+v", status, result)
		}
	}
}
func TestExpenseQueryAggregateBeyondInt64(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	in := expenseInput(a, c, p)
	in.Amount = "92233720368547758.07"
	in.Allocations = []AllocationInput{{Amount: in.Amount, CategoryID: c.ID, ProjectID: p.ID}}
	for i := 0; i < 2; i++ {
		in.OperationKey = fmt.Sprintf("huge-%d", i)
		if _, err := s.CreateExpense(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.QueryExpenses(ctx, ExpenseQuery{Limit: 1})
	if err != nil || page.TotalCount != 2 || page.Totals[0].Amount != "184467440737095516.14" || page.MatchingAllocationTotals[0].Amount != "184467440737095516.14" {
		t.Fatalf("aggregate: %+v %v", page, err)
	}
}
