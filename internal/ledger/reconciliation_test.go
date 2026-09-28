package ledger

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestReconciliationLifecycle(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	in := RecordBalanceObservationInput{OperationKey: "observe", AccountID: a.ID, Currency: "RON", AsOfDate: "2026-09-27", ActualBalance: "950", Basis: "posted_end_of_day", Notes: "Statement balance"}
	o, e := s.RecordBalanceObservation(ctx, in)
	if e != nil || o.Difference != "-50.00" || o.CurrentLedgerBalance != "1000.00" || o.Status != "unmatched" {
		t.Fatalf("observation %+v %v", o, e)
	}
	replay, e := s.RecordBalanceObservation(ctx, in)
	if e != nil || !reflect.DeepEqual(o, replay) {
		t.Fatalf("retry %+v %v", replay, e)
	}
	d, e := s.Dashboard(ctx)
	if e != nil || d.Accounts[0].Balance != "1000.00" {
		t.Fatal(d, e)
	}
	adjust := CreateBalanceAdjustmentInput{OperationKey: "adjust", ObservationID: o.ID, ExpectedObservationVersion: 1, ExpectedLedgerBalance: "999.00", Amount: "-50", Reason: "Unknown cash spending"}
	if _, e = s.CreateBalanceAdjustment(ctx, adjust); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	adjust.ExpectedLedgerBalance = "1000.00"
	adjust.Amount = "-49"
	if _, e = s.CreateBalanceAdjustment(ctx, adjust); !errors.Is(e, ErrValidation) {
		t.Fatal(e)
	}
	adjust.Amount = "-50"
	r, e := s.CreateBalanceAdjustment(ctx, adjust)
	if e != nil || r.Observation.Status != "matched" || r.Observation.Version != 2 {
		t.Fatalf("adjust %+v %v", r, e)
	}
	rr, e := s.CreateBalanceAdjustment(ctx, adjust)
	if e != nil || !reflect.DeepEqual(rr, r) {
		t.Fatal(rr, e)
	}
	d, e = s.Dashboard(ctx)
	if e != nil || d.Accounts[0].Balance != "950.00" || len(d.Spending) != 0 {
		t.Fatal(d, e)
	}
	ip, e := s.QueryIncome(ctx, IncomeQuery{})
	if e != nil || ip.TotalCount != 0 {
		t.Fatal(ip, e)
	}
	if _, e = s.VoidBalanceObservation(ctx, VoidBalanceObservationInput{OperationKey: "void-observation", ID: o.ID, ExpectedVersion: 2}); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	amend := AmendBalanceObservationInput{RecordBalanceObservationInput: in, ID: o.ID, ExpectedVersion: 2}
	amend.OperationKey = "amend"
	amend.ActualBalance = "960"
	if _, e = s.AmendBalanceObservation(ctx, amend); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	v, e := s.VoidBalanceAdjustment(ctx, VoidBalanceAdjustmentInput{OperationKey: "void-adjust", ID: r.Adjustment.ID, ExpectedVersion: 1})
	if e != nil || v.Observation.Difference != "-50.00" || v.Observation.Version != 3 || v.Adjustment.Status != "void" {
		t.Fatal(v, e)
	}
	amend.ExpectedVersion = 3
	o, e = s.AmendBalanceObservation(ctx, amend)
	if e != nil || o.ActualBalance != "960.00" || o.Version != 4 {
		t.Fatal(o, e)
	}
	o, e = s.VoidBalanceObservation(ctx, VoidBalanceObservationInput{OperationKey: "void-observation", ID: o.ID, ExpectedVersion: 4})
	if e != nil || o.Status != "void" {
		t.Fatal(o, e)
	}
	page, e := s.QueryBalanceAdjustments(ctx, BalanceAdjustmentQuery{Status: "all", Limit: 1})
	if e != nil || page.TotalCount != 1 || len(page.Adjustments) != 1 {
		t.Fatal(page, e)
	}
	obs, e := s.QueryBalanceObservations(ctx, BalanceObservationQuery{Limit: 1, Offset: 1})
	if e != nil || obs.TotalCount != 1 || len(obs.Observations) != 0 {
		t.Fatal(obs, e)
	}
	audit, e := s.Audit(ctx, r.Adjustment.ID)
	if e != nil || len(audit) != 2 {
		t.Fatal(audit, e)
	}
}
func TestReconciliationAsOfAllMovements(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	b, e := s.CreateAccount(ctx, CreateAccountInput{OperationKey: "second", Name: "Other", Currency: "RON", OpeningAmount: "0", OpeningDate: "2026-09-01"})
	if e != nil {
		t.Fatal(e)
	}
	exp := expenseInput(a, c, p)
	exp.OccurredDate = "2026-09-10"
	if _, e = s.CreateExpense(ctx, exp); e != nil {
		t.Fatal(e)
	}
	for _, day := range []string{"2026-09-01", "2026-09-10", "2026-09-11"} {
		if _, e = s.CreateIncome(ctx, CreateIncomeInput{OperationKey: "income-" + day, AccountID: a.ID, Currency: "RON", Amount: "100", OccurredDate: day, Description: "Credit"}); e != nil {
			t.Fatal(e)
		}
	}
	ti := CreateTransferInput{OperationKey: "out", SourceAccountID: a.ID, DestinationAccountID: b.ID, SourceCurrency: "RON", DestinationCurrency: "RON", SourceAmount: "20", DestinationAmount: "20", OccurredDate: "2026-09-10", Description: "Out"}
	if _, e = s.CreateTransfer(ctx, ti); e != nil {
		t.Fatal(e)
	}
	ti.OperationKey = "in"
	ti.SourceAccountID = b.ID
	ti.DestinationAccountID = a.ID
	ti.SourceAmount = "10"
	ti.DestinationAmount = "10"
	if _, e = s.CreateTransfer(ctx, ti); e != nil {
		t.Fatal(e)
	}
	debt, e := s.CreateReceivable(ctx, CreateReceivableInput{OperationKey: "debt", Debtor: "Friend", Amount: "100", Currency: "RON"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateRepayment(ctx, CreateRepaymentInput{OperationKey: "repaid", ReceivableID: debt.ID, ExpectedReceivableVersion: 1, AccountID: a.ID, Amount: "30", OccurredDate: "2026-09-10", Description: "Repayment"}); e != nil {
		t.Fatal(e)
	}
	in := RecordBalanceObservationInput{OperationKey: "observe", AccountID: a.ID, Currency: "RON", AsOfDate: "2026-09-10", ActualBalance: "790", Basis: "posted_end_of_day"}
	o, e := s.RecordBalanceObservation(ctx, in)
	if e != nil || o.CurrentLedgerBalance != "790.00" || o.Status != "matched" {
		t.Fatal(o, e)
	}
	in.OperationKey = "start"
	in.AsOfDate = "2026-09-01"
	in.ActualBalance = "1100"
	o, e = s.RecordBalanceObservation(ctx, in)
	if e != nil || o.CurrentLedgerBalance != "1100.00" {
		t.Fatal(o, e)
	}
	in.OperationKey = "invalid"
	in.AsOfDate = "2026-08-31"
	if _, e = s.RecordBalanceObservation(ctx, in); !errors.Is(e, ErrValidation) {
		t.Fatal(e)
	}
	in.AsOfDate = "2026-09-01"
	in.Currency = "EUR"
	if _, e = s.RecordBalanceObservation(ctx, in); !errors.Is(e, ErrValidation) {
		t.Fatal(e)
	}
	in.Currency = "RON"
	in.Basis = "available"
	if _, e = s.RecordBalanceObservation(ctx, in); !errors.Is(e, ErrValidation) {
		t.Fatal(e)
	}
}
func TestReconciliationDynamicBalanceAndOverflow(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	in := RecordBalanceObservationInput{OperationKey: "observe", AccountID: a.ID, Currency: "RON", AsOfDate: "2026-09-10", ActualBalance: "-92233720368547758.08", Basis: "posted_end_of_day"}
	o, e := s.RecordBalanceObservation(ctx, in)
	if e != nil || o.Difference != "-92233720368548758.08" {
		t.Fatal(o, e)
	}
	inc, e := s.CreateIncome(ctx, CreateIncomeInput{OperationKey: "huge", AccountID: a.ID, Currency: "RON", Amount: "92233720368547758.07", OccurredDate: "2026-09-10", Description: "Big"})
	if e != nil {
		t.Fatal(e)
	}
	page, e := s.QueryBalanceObservations(ctx, BalanceObservationQuery{})
	if e != nil || page.Observations[0].RecordedLedgerBalance != "1000.00" || page.Observations[0].CurrentLedgerBalance != "92233720368548758.07" || page.Observations[0].Difference != "-184467440737096516.15" {
		t.Fatal(page, e)
	}
	if _, e = s.VoidIncome(ctx, VoidIncomeInput{OperationKey: "void-huge", ID: inc.ID, ExpectedVersion: 1}); e != nil {
		t.Fatal(e)
	}
	page, e = s.QueryBalanceObservations(ctx, BalanceObservationQuery{})
	if e != nil || page.Observations[0].CurrentLedgerBalance != "1000.00" {
		t.Fatal(page, e)
	}
}
func TestReconciliationConcurrentLedgerWrite(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	o, e := s.RecordBalanceObservation(ctx, RecordBalanceObservationInput{OperationKey: "observe", AccountID: a.ID, Currency: "RON", AsOfDate: "2026-09-10", ActualBalance: "950", Basis: "posted_end_of_day"})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	start := make(chan struct{})
	var adjustErr, incomeErr error
	go func() {
		defer wg.Done()
		<-start
		_, adjustErr = s.CreateBalanceAdjustment(ctx, CreateBalanceAdjustmentInput{OperationKey: "adjust", ObservationID: o.ID, ExpectedObservationVersion: 1, ExpectedLedgerBalance: "1000.00", Amount: "-50", Reason: "Unknown"})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, incomeErr = s.CreateIncome(ctx, CreateIncomeInput{OperationKey: "credit", AccountID: a.ID, Currency: "RON", Amount: "100", OccurredDate: "2026-09-10", Description: "Credit"})
	}()
	close(start)
	wg.Wait()
	if incomeErr != nil {
		t.Fatal(incomeErr)
	}
	if adjustErr != nil && !errors.Is(adjustErr, ErrConflict) {
		t.Fatal(adjustErr)
	}
	page, e := s.QueryBalanceObservations(ctx, BalanceObservationQuery{})
	if e != nil {
		t.Fatal(e)
	}
	want := "-100.00"
	if adjustErr != nil {
		want = "-150.00"
	}
	if page.Observations[0].Difference != want || page.Observations[0].Status != "unmatched" {
		t.Fatal(page, adjustErr)
	}
}

func TestReconciliationAdjustmentsRespectAsOfAndVoids(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	observe := func(key, day, actual string) BalanceObservation {
		t.Helper()
		o, e := s.RecordBalanceObservation(ctx, RecordBalanceObservationInput{OperationKey: key, AccountID: a.ID, Currency: "RON", AsOfDate: day, ActualBalance: actual, Basis: "posted_end_of_day"})
		if e != nil {
			t.Fatal(e)
		}
		return o
	}
	earlier := observe("earlier", "2026-09-09", "1000")
	o := observe("later", "2026-09-10", "1050")
	adjustment := CreateBalanceAdjustmentInput{OperationKey: "positive", ObservationID: o.ID, ExpectedObservationVersion: 1, ExpectedLedgerBalance: "1000.00", Amount: "50", Reason: "Unexplained credit"}
	r, e := s.CreateBalanceAdjustment(ctx, adjustment)
	if e != nil || r.Observation.Status != "matched" {
		t.Fatal(r, e)
	}
	adjustment.OperationKey = "duplicate"
	adjustment.ExpectedObservationVersion = 2
	adjustment.ExpectedLedgerBalance = "1050.00"
	if _, e = s.CreateBalanceAdjustment(ctx, adjustment); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	// A later expense must not change either earlier observation.
	expense := expenseInput(a, c, p)
	expense.OccurredDate = "2026-09-11"
	x, e := s.CreateExpense(ctx, expense)
	if e != nil {
		t.Fatal(e)
	}
	page, e := s.QueryBalanceObservations(ctx, BalanceObservationQuery{})
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range page.Observations {
		if v.Status != "matched" {
			t.Fatal(v)
		}
		if v.ID == earlier.ID && v.CurrentLedgerBalance != "1000.00" {
			t.Fatal(v)
		}
	}
	// Moving that expense to the observed day changes only the later observation.
	expense.OperationKey = "backdate"
	expense.OccurredDate = "2026-09-10"
	x, e = s.AmendExpense(ctx, AmendExpenseInput{CreateExpenseInput: expense, ID: x.ID, ExpectedVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	page, e = s.QueryBalanceObservations(ctx, BalanceObservationQuery{})
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range page.Observations {
		if v.ID == o.ID && (v.Difference != "430.00" || v.RecordedLedgerBalance != "1000.00") {
			t.Fatal(v)
		}
		if v.ID == earlier.ID && v.Status != "matched" {
			t.Fatal(v)
		}
	}
	if _, e = s.VoidExpense(ctx, VoidExpenseInput{OperationKey: "void-expense", ID: x.ID, ExpectedVersion: 2}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.VoidBalanceAdjustment(ctx, VoidBalanceAdjustmentInput{OperationKey: "void-positive", ID: r.Adjustment.ID, ExpectedVersion: 1}); e != nil {
		t.Fatal(e)
	}
	page, e = s.QueryBalanceObservations(ctx, BalanceObservationQuery{})
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range page.Observations {
		if v.ID == o.ID && v.Difference != "50.00" {
			t.Fatal(v)
		}
	}
	empty, e := s.QueryBalanceAdjustments(ctx, BalanceAdjustmentQuery{})
	if e != nil || empty.TotalCount != 0 {
		t.Fatal(empty, e)
	}
}
