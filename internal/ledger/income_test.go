package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIncomeLifecycle(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	in := CreateIncomeInput{OperationKey: "salary", AccountID: a.ID, Currency: "RON", Amount: "500", OccurredDate: "2026-09-27", Description: "Salary"}
	first, err := s.CreateIncome(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateIncome(ctx, in)
	if err != nil || retry != first {
		t.Fatalf("retry %+v %v", retry, err)
	}
	changed := in
	changed.Amount = "501"
	if _, err = s.CreateIncome(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("key binding %v", err)
	}
	in.OperationKey = "second"
	in.Amount = "100"
	if _, err = s.CreateIncome(ctx, in); err != nil {
		t.Fatal(err)
	}
	exp := expenseInput(a, c, p)
	for _, key := range []string{"expense-1", "expense-2"} {
		exp.OperationKey = key
		if _, err = s.CreateExpense(ctx, exp); err != nil {
			t.Fatal(err)
		}
	}
	assertBalance := func(want string) {
		t.Helper()
		d, err := s.Dashboard(ctx)
		if err != nil || len(d.Accounts) != 1 || d.Accounts[0].Balance != want || d.Spending[0].Amount != "860.00" {
			t.Fatalf("dashboard %+v %v want %s", d, err, want)
		}
	}
	assertBalance("740.00") // Income and expense aggregates must not multiply each other.
	in.OperationKey = "amend-salary"
	in.Amount = "600"
	amend := AmendIncomeInput{CreateIncomeInput: in, ID: first.ID, ExpectedVersion: 1}
	updated, err := s.AmendIncome(ctx, amend)
	if err != nil || updated.Version != 2 || updated.Amount != "600.00" {
		t.Fatalf("amend %+v %v", updated, err)
	}
	if replay, err := s.AmendIncome(ctx, amend); err != nil || replay != updated {
		t.Fatalf("amend replay %+v %v", replay, err)
	}
	amend.OperationKey = "stale"
	if _, err = s.AmendIncome(ctx, amend); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale %v", err)
	}
	assertBalance("840.00")
	v := VoidIncomeInput{OperationKey: "void-salary", ID: first.ID, ExpectedVersion: 2}
	voided, err := s.VoidIncome(ctx, v)
	if err != nil || voided.Status != "void" || voided.Version != 3 {
		t.Fatalf("void %+v %v", voided, err)
	}
	if replay, err := s.VoidIncome(ctx, v); err != nil || replay != voided {
		t.Fatalf("void replay %+v %v", replay, err)
	}
	amend.ExpectedVersion = 3
	amend.OperationKey = "amend-void"
	if _, err = s.AmendIncome(ctx, amend); !errors.Is(err, ErrConflict) {
		t.Fatalf("amend void %v", err)
	}
	assertBalance("240.00")
	history, err := s.Audit(ctx, first.ID)
	if err != nil || len(history) != 3 || history[0].Action != "income_created" || history[1].Action != "income_amended" || history[2].Action != "income_voided" {
		t.Fatalf("audit %+v %v", history, err)
	}
}

func TestIncomeValidationRollback(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	good := CreateIncomeInput{OperationKey: "valid", AccountID: a.ID, Currency: "RON", Amount: "10", OccurredDate: "2026-09-01"}
	invalid := []CreateIncomeInput{}
	for _, amount := range []string{"0", "-1", "1.001", "92233720368547758.08"} {
		x := good
		x.Amount = amount
		invalid = append(invalid, x)
	}
	for _, day := range []string{"2026-08-31", "2026-02-30", "0000-01-01"} {
		x := good
		x.OccurredDate = day
		invalid = append(invalid, x)
	}
	x := good
	x.Currency = "EUR"
	invalid = append(invalid, x)
	x = good
	x.Description = strings.Repeat("x", 2001)
	invalid = append(invalid, x)
	for i, in := range invalid {
		in.OperationKey = fmt.Sprintf("bad-%d", i)
		if _, err := s.CreateIncome(ctx, in); !errors.Is(err, ErrValidation) {
			t.Fatalf("invalid %+v: %v", in, err)
		}
		good.OperationKey = in.OperationKey
		if _, err := s.CreateIncome(ctx, good); err != nil {
			t.Fatalf("failed key not reusable %v", err)
		}
	}
	missing := good
	missing.OperationKey = "missing"
	missing.AccountID = "missing"
	if _, err := s.CreateIncome(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	page, err := s.QueryIncome(ctx, IncomeQuery{})
	if err != nil || page.TotalCount != int64(len(invalid)) {
		t.Fatalf("rollback %+v %v", page, err)
	}
	before := page.Incomes[0]
	badAmend := AmendIncomeInput{CreateIncomeInput: good, ID: before.ID, ExpectedVersion: 1}
	badAmend.OperationKey = "bad-amend"
	badAmend.Amount = "0"
	if _, err = s.AmendIncome(ctx, badAmend); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	hist, err := s.Audit(ctx, before.ID)
	if err != nil || len(hist) != 1 {
		t.Fatalf("failed amendment audit %+v %v", hist, err)
	}
	var n int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM operation_results WHERE operation_key='bad-amend'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed result persisted %d %v", n, err)
	}
}

func TestIncomeQuery(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	euro, err := s.CreateAccount(ctx, CreateAccountInput{OperationKey: "eur", Name: "Euro", Currency: "EUR", OpeningAmount: "0", OpeningDate: "2026-09-01"})
	if err != nil {
		t.Fatal(err)
	}
	var first Income
	for i := 0; i < 4; i++ {
		account := a
		if i == 3 {
			account = euro
		}
		in := CreateIncomeInput{OperationKey: fmt.Sprintf("income-%d", i), AccountID: account.ID, Currency: account.Currency, Amount: "25.01", OccurredDate: fmt.Sprintf("2026-09-%02d", 20+i)}
		v, err := s.CreateIncome(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = v
		}
	}
	page, err := s.QueryIncome(ctx, IncomeQuery{Limit: 1})
	if err != nil || page.TotalCount != 4 || len(page.Incomes) != 1 || len(page.Totals) != 2 || page.Totals[0].Amount != "25.01" || page.Totals[1].Amount != "75.03" {
		t.Fatalf("page %+v %v", page, err)
	}
	next, err := s.QueryIncome(ctx, IncomeQuery{Limit: 1, Offset: 1})
	if err != nil || len(next.Incomes) != 1 || next.Incomes[0].ID == page.Incomes[0].ID {
		t.Fatalf("next %+v %v", next, err)
	}
	empty, err := s.QueryIncome(ctx, IncomeQuery{Offset: 100})
	if err != nil || empty.TotalCount != 4 || len(empty.Incomes) != 0 || len(empty.Totals) != 2 {
		t.Fatalf("empty %+v %v", empty, err)
	}
	match, err := s.QueryIncome(ctx, IncomeQuery{AccountID: a.ID, Currency: "RON", FromDate: "2026-09-20", ToDate: "2026-09-20"})
	if err != nil || match.TotalCount != 1 || match.Incomes[0].ID != first.ID {
		t.Fatalf("inclusive %+v %v", match, err)
	}
	if _, err = s.VoidIncome(ctx, VoidIncomeInput{OperationKey: "void", ID: first.ID, ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"", "all", "void"} {
		r, err := s.QueryIncome(ctx, IncomeQuery{Status: status, ToDate: "2026-09-20"})
		count := int64(1)
		if status == "" {
			count = 0
		}
		if err != nil || r.TotalCount != count || len(r.Totals) != 0 {
			t.Fatalf("status %q %+v %v", status, r, err)
		}
	}
}

func TestIncomeQueryValidation(t *testing.T) {
	for _, in := range []IncomeQuery{{Limit: -1}, {Limit: 101}, {Offset: -1}, {Status: "pending"}, {Currency: "GBP"}, {FromDate: "bad"}, {ToDate: "2026-02-30"}, {FromDate: "2026-09-02", ToDate: "2026-09-01"}} {
		if _, err := (&Service{}).QueryIncome(context.Background(), in); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v %v", in, err)
		}
	}
}

func TestIncomeMoveAccountAndLargeTotals(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	b, err := s.CreateAccount(ctx, CreateAccountInput{OperationKey: "other", Name: "Other", Currency: "USD", OpeningAmount: "0", OpeningDate: "2026-09-01"})
	if err != nil {
		t.Fatal(err)
	}
	in := CreateIncomeInput{OperationKey: "large1", AccountID: a.ID, Currency: "RON", Amount: "92233720368547758.07", OccurredDate: "2026-09-01"}
	first, err := s.CreateIncome(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.OperationKey = "large2"
	if _, err = s.CreateIncome(ctx, in); err != nil {
		t.Fatal(err)
	}
	page, err := s.QueryIncome(ctx, IncomeQuery{})
	if err != nil || len(page.Totals) != 1 || page.Totals[0].Amount != "184467440737095516.14" {
		t.Fatalf("large totals %+v %v", page, err)
	}
	in.OperationKey = "move"
	in.AccountID = b.ID
	in.Currency = "USD"
	in.Amount = "1"
	if _, err = s.AmendIncome(ctx, AmendIncomeInput{CreateIncomeInput: in, ID: first.ID, ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	d, err := s.Dashboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	balances := map[string]string{}
	for _, account := range d.Accounts {
		balances[account.ID] = account.Balance
	}
	if balances[a.ID] != "92233720368548758.07" || balances[b.ID] != "1.00" {
		t.Fatalf("moved balance %+v", balances)
	}
}

func TestIncomeConcurrentAmendment(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	in := CreateIncomeInput{OperationKey: "salary", AccountID: a.ID, Currency: "RON", Amount: "10", OccurredDate: "2026-09-01"}
	first, err := s.CreateIncome(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			<-ready
			edit := in
			edit.OperationKey = fmt.Sprintf("concurrent-%d", i)
			edit.Amount = "20"
			_, err := s.AmendIncome(ctx, AmendIncomeInput{CreateIncomeInput: edit, ID: first.ID, ExpectedVersion: 1})
			results <- err
		}(i)
	}
	close(ready)
	wins, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			wins++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins %d conflicts %d", wins, conflicts)
	}
	d, err := s.Dashboard(ctx)
	if err != nil || d.Accounts[0].Balance != "1020.00" {
		t.Fatalf("balance %+v %v", d, err)
	}
	history, err := s.Audit(ctx, first.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("audit %+v %v", history, err)
	}
}
