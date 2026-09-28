package ledger

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func transferAccount(t *testing.T, s *Service, ctx context.Context, key, currency string) Account {
	t.Helper()
	a, e := s.CreateAccount(ctx, CreateAccountInput{OperationKey: key, Name: key, Currency: currency, OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func transferInput(a, b Account) CreateTransferInput {
	return CreateTransferInput{OperationKey: "transfer", SourceAccountID: a.ID, DestinationAccountID: b.ID, SourceCurrency: a.Currency, DestinationCurrency: b.Currency, SourceAmount: "100", DestinationAmount: "100", OccurredDate: "2026-09-27"}
}
func transferBalances(t *testing.T, s *Service, ctx context.Context, want map[string]string) {
	t.Helper()
	d, e := s.Dashboard(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range d.Accounts {
		if w, ok := want[a.ID]; ok && a.Balance != w {
			t.Fatalf("%s balance %s want %s", a.ID, a.Balance, w)
		}
	}
}
func TestTransferLifecycle(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	b := transferAccount(t, s, ctx, "cash", "RON")
	euro := transferAccount(t, s, ctx, "euro", "EUR")
	in := transferInput(a, b)
	first, e := s.CreateTransfer(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if r, e := s.CreateTransfer(ctx, in); e != nil || r != first {
		t.Fatalf("retry %+v %v", r, e)
	}
	bad := in
	bad.SourceAmount = "101"
	if _, e := s.CreateTransfer(ctx, bad); !errors.Is(e, ErrConflict) {
		t.Fatalf("key binding %v", e)
	}
	// Two outgoing legs, one incoming leg, incomes and expenses must aggregate independently.
	other := transferInput(a, euro)
	other.OperationKey = "fx"
	other.SourceAmount = "250"
	other.DestinationAmount = "50"
	fx, e := s.CreateTransfer(ctx, other)
	if e != nil {
		t.Fatal(e)
	}
	back := transferInput(b, a)
	back.OperationKey = "return"
	back.SourceAmount = "30"
	back.DestinationAmount = "30"
	if _, e = s.CreateTransfer(ctx, back); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		inc := CreateIncomeInput{OperationKey: fmt.Sprint("income", i), AccountID: a.ID, Currency: "RON", Amount: "50", OccurredDate: in.OccurredDate}
		if _, e = s.CreateIncome(ctx, inc); e != nil {
			t.Fatal(e)
		}
		exp := expenseInput(a, c, p)
		exp.OperationKey = fmt.Sprint("expense", i)
		if _, e = s.CreateExpense(ctx, exp); e != nil {
			t.Fatal(e)
		}
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "-80.00", b.ID: "1070.00", euro.ID: "1050.00"})
	d, e := s.Dashboard(ctx)
	if e != nil || d.Spending[0].Amount != "860.00" {
		t.Fatalf("totals %+v %v", d, e)
	}
	income, e := s.QueryIncome(ctx, IncomeQuery{})
	if e != nil || len(income.Totals) != 1 || income.Totals[0].Amount != "100.00" {
		t.Fatalf("income totals %+v %v", income, e)
	}
	in.OperationKey = "amend"
	in.SourceAmount = "120"
	in.DestinationAmount = "120"
	amendment := AmendTransferInput{CreateTransferInput: in, ID: first.ID, ExpectedVersion: 1}
	updated, e := s.AmendTransfer(ctx, amendment)
	if e != nil || updated.Version != 2 {
		t.Fatalf("amend %+v %v", updated, e)
	}
	if r, e := s.AmendTransfer(ctx, amendment); e != nil || r != updated {
		t.Fatalf("amend replay %+v %v", r, e)
	}
	amendment.OperationKey = "stale"
	if _, e = s.AmendTransfer(ctx, amendment); !errors.Is(e, ErrConflict) {
		t.Fatalf("stale %v", e)
	}
	v := VoidTransferInput{OperationKey: "void", ID: first.ID, ExpectedVersion: 2}
	voided, e := s.VoidTransfer(ctx, v)
	if e != nil || voided.Version != 3 || voided.Status != "void" {
		t.Fatalf("void %+v %v", voided, e)
	}
	if r, e := s.VoidTransfer(ctx, v); e != nil || r != voided {
		t.Fatalf("void replay %+v %v", r, e)
	}
	amendment.ExpectedVersion = 3
	if _, e = s.AmendTransfer(ctx, amendment); !errors.Is(e, ErrConflict) {
		t.Fatalf("amend void %v", e)
	}
	if _, e = s.VoidTransfer(ctx, VoidTransferInput{OperationKey: "void-fx", ID: fx.ID, ExpectedVersion: 1}); e != nil {
		t.Fatal(e)
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "270.00", b.ID: "970.00", euro.ID: "1000.00"})
	audit, e := s.Audit(ctx, first.ID)
	if e != nil || len(audit) != 3 || audit[0].Action != "transfer_created" || audit[1].Action != "transfer_amended" || audit[2].Action != "transfer_voided" {
		t.Fatalf("audit %+v %v", audit, e)
	}
}
func TestTransferValidationRollback(t *testing.T) {
	s, ctx := testService(t)
	a := transferAccount(t, s, ctx, "a", "RON")
	b := transferAccount(t, s, ctx, "b", "RON")
	good := transferInput(a, b)
	mutations := []func(*CreateTransferInput){func(i *CreateTransferInput) { i.SourceAmount = "0" }, func(i *CreateTransferInput) { i.DestinationAmount = "-1" }, func(i *CreateTransferInput) { i.SourceAmount = "1.001" }, func(i *CreateTransferInput) { i.DestinationAmount = "92233720368547758.08" }, func(i *CreateTransferInput) { i.DestinationAmount = "99" }, func(i *CreateTransferInput) { i.SourceCurrency = "EUR" }, func(i *CreateTransferInput) { i.DestinationCurrency = "USD" }, func(i *CreateTransferInput) { i.DestinationAccountID = a.ID }, func(i *CreateTransferInput) { i.OccurredDate = "2026-08-31" }, func(i *CreateTransferInput) { i.OccurredDate = "2026-02-30" }}
	for n, change := range mutations {
		in := good
		change(&in)
		in.OperationKey = fmt.Sprint(n)
		if _, e := s.CreateTransfer(ctx, in); !errors.Is(e, ErrValidation) {
			t.Fatalf("invalid %d: %v", n, e)
		}
	}
	missing := good
	missing.DestinationAccountID = "missing"
	if _, e := s.CreateTransfer(ctx, missing); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.CreateTransfer(ctx, good); e != nil {
		t.Fatal(e)
	} // Failed operation key did not persist.
	late, e := s.CreateAccount(ctx, CreateAccountInput{OperationKey: "late", Name: "Late", Currency: "RON", OpeningAmount: "0", OpeningDate: "2026-09-28"})
	if e != nil {
		t.Fatal(e)
	}
	lateInput := good
	lateInput.OperationKey = "late-transfer"
	lateInput.DestinationAccountID = late.ID
	if _, e = s.CreateTransfer(ctx, lateInput); !errors.Is(e, ErrValidation) {
		t.Fatalf("destination opening date: %v", e)
	}
	rows, e := s.QueryTransfers(ctx, TransferQuery{})
	if e != nil || len(rows.Transfers) != 1 {
		t.Fatalf("rollback rows %+v %v", rows, e)
	}
	amendment := AmendTransferInput{CreateTransferInput: good, ID: rows.Transfers[0].ID, ExpectedVersion: 1}
	amendment.OperationKey = "bad-amend"
	amendment.DestinationCurrency = "EUR"
	if _, e = s.AmendTransfer(ctx, amendment); !errors.Is(e, ErrValidation) {
		t.Fatalf("amend rollback %v", e)
	}
	history, e := s.Audit(ctx, amendment.ID)
	if e != nil || len(history) != 1 {
		t.Fatalf("failed writes audit %+v %v", history, e)
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "900.00", b.ID: "1100.00"})
}
func TestTransferQueryAndConcurrency(t *testing.T) {
	s, ctx := testService(t)
	a := transferAccount(t, s, ctx, "a", "RON")
	b := transferAccount(t, s, ctx, "b", "RON")
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			in := transferInput(a, b)
			if n%2 == 1 {
				in = transferInput(b, a)
			}
			in.OperationKey = fmt.Sprint("parallel", n)
			_, e := s.CreateTransfer(ctx, in)
			errs <- e
		}(n)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "1000.00", b.ID: "1000.00"})
	page, e := s.QueryTransfers(ctx, TransferQuery{AccountID: a.ID, FromDate: "2026-09-27", ToDate: "2026-09-27", Limit: 3, Offset: 2})
	if e != nil || page.TotalCount != 20 || len(page.Transfers) != 3 {
		t.Fatalf("page %+v %v", page, e)
	}
	incoming, e := s.QueryTransfers(ctx, TransferQuery{AccountID: b.ID, Limit: 3, Offset: 2})
	if e != nil || incoming.TotalCount != 20 || incoming.Transfers[0] != page.Transfers[0] {
		t.Fatalf("both legs %+v %v", incoming, e)
	}
	row := page.Transfers[0]
	if _, e = s.VoidTransfer(ctx, VoidTransferInput{OperationKey: "void", ID: row.ID, ExpectedVersion: 1}); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		q TransferQuery
		n int64
	}{{TransferQuery{}, 19}, {TransferQuery{Status: "all"}, 20}, {TransferQuery{Status: "void"}, 1}, {TransferQuery{ToDate: "2026-09-26"}, 0}, {TransferQuery{AccountID: "absent"}, 0}, {TransferQuery{Offset: 100}, 19}} {
		r, e := s.QueryTransfers(ctx, tc.q)
		if e != nil || r.TotalCount != tc.n {
			t.Fatalf("query %+v: %+v %v", tc.q, r, e)
		}
	}
	for _, q := range []TransferQuery{{Limit: 101}, {Offset: -1}, {Status: "pending"}, {FromDate: "2026-09-28", ToDate: "2026-09-27"}, {FromDate: "no"}} {
		if _, e = s.QueryTransfers(ctx, q); !errors.Is(e, ErrValidation) {
			t.Fatalf("invalid %+v %v", q, e)
		}
	}
}
