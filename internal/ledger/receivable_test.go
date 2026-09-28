package ledger

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestReceivableLifecycle(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	in := CreateReceivableInput{OperationKey: "debt", Debtor: "Alex", Amount: "500", Currency: "RON", DueDate: "2026-10-01", Notes: "Existing debt"}
	d, e := s.CreateReceivable(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := s.CreateReceivable(ctx, in)
	if e != nil || !reflect.DeepEqual(d, retry) {
		t.Fatalf("retry %+v %v", retry, e)
	}
	assertBalance := func(want string) {
		t.Helper()
		v, e := s.Dashboard(ctx)
		if e != nil || v.Accounts[0].Balance != want || len(v.Expenses) != 0 || len(v.Spending) != 0 {
			t.Fatalf("dashboard %+v %v", v, e)
		}
		ip, e := s.QueryIncome(ctx, IncomeQuery{})
		if e != nil || ip.TotalCount != 0 {
			t.Fatalf("income %+v %v", ip, e)
		}
	}
	assertBalance("1000.00")
	pin := CreateRepaymentInput{OperationKey: "repay", ReceivableID: d.ID, ExpectedReceivableVersion: 1, AccountID: a.ID, Amount: "200", OccurredDate: "2026-09-27", Description: "Cash received"}
	p, e := s.CreateRepayment(ctx, pin)
	if e != nil || p.Receivable.RemainingAmount != "300.00" || p.Receivable.Version != 2 {
		t.Fatalf("repay %+v %v", p, e)
	}
	pr, e := s.CreateRepayment(ctx, pin)
	if e != nil || !reflect.DeepEqual(p, pr) {
		t.Fatalf("repay retry %+v %v", pr, e)
	}
	assertBalance("1200.00")
	if _, e = s.VoidReceivable(ctx, VoidReceivableInput{OperationKey: "void-too-soon", ID: d.ID, ExpectedVersion: 2}); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	bad := AmendReceivableInput{CreateReceivableInput: in, ID: d.ID, ExpectedVersion: 2}
	bad.OperationKey = "underpaid"
	bad.Amount = "199"
	if _, e = s.AmendReceivable(ctx, bad); !errors.Is(e, ErrValidation) {
		t.Fatal(e)
	}
	bad.Amount = "500"
	bad.Currency = "EUR"
	if _, e = s.AmendReceivable(ctx, bad); !errors.Is(e, ErrValidation) {
		t.Fatal(e)
	}
	amend := AmendRepaymentInput{CreateRepaymentInput: pin, ID: p.Repayment.ID, ExpectedVersion: 1}
	amend.OperationKey = "amend-payment"
	amend.ExpectedReceivableVersion = 2
	amend.Amount = "500"
	p, e = s.AmendRepayment(ctx, amend)
	if e != nil || p.Receivable.Status != "settled" || p.Receivable.RemainingAmount != "0.00" {
		t.Fatalf("settle %+v %v", p, e)
	}
	assertBalance("1500.00")
	page, e := s.QueryReceivables(ctx, ReceivableQuery{})
	if e != nil || page.TotalCount != 0 {
		t.Fatalf("open %+v %v", page, e)
	}
	page, e = s.QueryReceivables(ctx, ReceivableQuery{Status: "settled"})
	if e != nil || page.TotalCount != 1 {
		t.Fatalf("settled %+v %v", page, e)
	}
	p, e = s.VoidRepayment(ctx, VoidRepaymentInput{OperationKey: "void-pay", ID: p.Repayment.ID, ReceivableID: d.ID, ExpectedVersion: 2, ExpectedReceivableVersion: 3})
	if e != nil || p.Receivable.Status != "open" || p.Receivable.RemainingAmount != "500.00" {
		t.Fatalf("void %+v %v", p, e)
	}
	assertBalance("1000.00")
	bad.ExpectedVersion = 4
	bad.OperationKey = "change-currency"
	d, e = s.AmendReceivable(ctx, bad)
	if e != nil {
		t.Fatal(e)
	}
	rp, e := s.QueryRepayments(ctx, RepaymentQuery{ReceivableID: d.ID, AccountID: a.ID, Status: "void"})
	if e != nil || rp.TotalCount != 1 || rp.Repayments[0].Currency != "RON" {
		t.Fatalf("historical currency %+v %v", rp, e)
	}
	d, e = s.VoidReceivable(ctx, VoidReceivableInput{OperationKey: "void-debt", ID: d.ID, ExpectedVersion: 5})
	if e != nil || d.RemainingAmount != "0.00" || d.Status != "void" {
		t.Fatalf("voiddebt %+v %v", d, e)
	}
}
func TestRepaymentValidationAndConcurrency(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	d, e := s.CreateReceivable(ctx, CreateReceivableInput{OperationKey: "debt", Debtor: "Debtor", Amount: "100", Currency: "RON"})
	if e != nil {
		t.Fatal(e)
	}
	base := CreateRepaymentInput{OperationKey: "payment", ReceivableID: d.ID, ExpectedReceivableVersion: 1, AccountID: a.ID, Amount: "100", OccurredDate: "2026-09-27"}
	other, e := s.CreateAccount(ctx, CreateAccountInput{OperationKey: "eur", Name: "EUR", Currency: "EUR", OpeningAmount: "0", OpeningDate: "2026-09-01"})
	if e != nil {
		t.Fatal(e)
	}
	for _, test := range []struct {
		name string
		edit func(*CreateRepaymentInput)
	}{{"overpay", func(x *CreateRepaymentInput) { x.Amount = "100.01" }}, {"negative", func(x *CreateRepaymentInput) { x.Amount = "-1" }}, {"zero", func(x *CreateRepaymentInput) { x.Amount = "0" }}, {"precision", func(x *CreateRepaymentInput) { x.Amount = "1.001" }}, {"date", func(x *CreateRepaymentInput) { x.OccurredDate = "2026-08-31" }}, {"invalid-date", func(x *CreateRepaymentInput) { x.OccurredDate = "2026-02-30" }}, {"currency", func(x *CreateRepaymentInput) { x.AccountID = other.ID }}} {
		t.Run(test.name, func(t *testing.T) {
			x := base
			x.OperationKey = test.name
			test.edit(&x)
			if _, e := s.CreateRepayment(ctx, x); !errors.Is(e, ErrValidation) {
				t.Fatalf("%v", e)
			}
		})
	}
	page, e := s.QueryRepayments(ctx, RepaymentQuery{})
	if e != nil || page.TotalCount != 0 {
		t.Fatalf("rollback %+v %v", page, e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, key := range []string{"race-a", "race-b"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			x := base
			x.OperationKey = key
			_, e := s.CreateRepayment(ctx, x)
			errs <- e
		}(key)
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for e := range errs {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("%d %d", success, conflict)
	}
	page, e = s.QueryRepayments(ctx, RepaymentQuery{})
	if e != nil || page.TotalCount != 1 {
		t.Fatalf("race %+v %v", page, e)
	}
}
func TestReceivableQueries(t *testing.T) {
	s, ctx := testService(t)
	for _, x := range []CreateReceivableInput{{OperationKey: "a", Debtor: "A", Amount: "10", Currency: "RON"}, {OperationKey: "b", Debtor: "A", Amount: "20", Currency: "RON"}, {OperationKey: "c", Debtor: "B", Amount: "30", Currency: "EUR"}} {
		if _, e := s.CreateReceivable(ctx, x); e != nil {
			t.Fatal(e)
		}
	}
	page, e := s.QueryReceivables(ctx, ReceivableQuery{Limit: 1, Offset: 1, Debtor: "A"})
	if e != nil || page.TotalCount != 2 || len(page.Receivables) != 1 || !reflect.DeepEqual(page.OutstandingTotals, []CurrencyTotal{{Currency: "RON", Amount: "30.00"}}) {
		t.Fatalf("page %+v %v", page, e)
	}
	page, e = s.QueryReceivables(ctx, ReceivableQuery{Currency: "EUR", Offset: 9})
	if e != nil || page.TotalCount != 1 || len(page.Receivables) != 0 || page.OutstandingTotals[0].Amount != "30.00" {
		t.Fatalf("currency %+v %v", page, e)
	}
	for _, q := range []ReceivableQuery{{Limit: 101}, {Offset: -1}, {Status: "posted"}, {Currency: "XYZ"}} {
		if _, e := s.QueryReceivables(ctx, q); !errors.Is(e, ErrValidation) {
			t.Fatal(e)
		}
	}
	for _, q := range []RepaymentQuery{{Limit: 101}, {Offset: -1}, {Status: "open"}} {
		if _, e := s.QueryRepayments(ctx, q); !errors.Is(e, ErrValidation) {
			t.Fatal(e)
		}
	}
}

func TestRepaymentCorrectionMovesAccountAndPreservesDebt(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	b, e := s.CreateAccount(ctx, CreateAccountInput{OperationKey: "second", Name: "Other", Currency: "RON", OpeningAmount: "0", OpeningDate: "2026-09-01"})
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.CreateReceivable(ctx, CreateReceivableInput{OperationKey: "debt", Debtor: "D", Amount: "100", Currency: "RON"})
	if e != nil {
		t.Fatal(e)
	}
	d2, e := s.CreateReceivable(ctx, CreateReceivableInput{OperationKey: "other-debt", Debtor: "E", Amount: "100", Currency: "RON"})
	if e != nil {
		t.Fatal(e)
	}
	in := CreateRepaymentInput{OperationKey: "first", ReceivableID: d.ID, ExpectedReceivableVersion: 1, AccountID: a.ID, Amount: "40", OccurredDate: "2026-09-27"}
	p, e := s.CreateRepayment(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	correction := AmendRepaymentInput{CreateRepaymentInput: in, ID: p.Repayment.ID, ExpectedVersion: 1}
	correction.OperationKey = "move"
	correction.AccountID = b.ID
	correction.ExpectedReceivableVersion = 2
	correction.ReceivableID = d2.ID
	correction.ExpectedReceivableVersion = 1
	if _, e = s.AmendRepayment(ctx, correction); !errors.Is(e, ErrValidation) {
		t.Fatalf("wrong debt %v", e)
	}
	correction.ReceivableID = d.ID
	correction.ExpectedReceivableVersion = 1
	if _, e = s.AmendRepayment(ctx, correction); !errors.Is(e, ErrConflict) {
		t.Fatalf("stale debt %v", e)
	}
	correction.ExpectedReceivableVersion = 2
	p, e = s.AmendRepayment(ctx, correction)
	if e != nil || p.Receivable.RepaidAmount != "40.00" {
		t.Fatalf("move %+v %v", p, e)
	}
	final := in
	final.OperationKey = "final"
	final.ExpectedReceivableVersion = 3
	final.Amount = "60"
	p, e = s.CreateRepayment(ctx, final)
	if e != nil || p.Receivable.Status != "settled" || p.Receivable.Version != 4 {
		t.Fatalf("second payment %+v %v", p, e)
	}
	snapshot, e := s.Dashboard(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range snapshot.Accounts {
		if v.ID == a.ID && v.Balance != "1060.00" || v.ID == b.ID && v.Balance != "40.00" {
			t.Fatalf("balance %+v", v)
		}
	}
}
