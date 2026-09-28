package ledger

import (
	"errors"
	"sync"
	"testing"
)

func TestAccountLifecycleFinancialBoundaries(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	b := transferAccount(t, s, ctx, "other", "RON")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	reject := func(err error) {
		t.Helper()
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("expected archive validation: %v", err)
		}
	}
	ei := expenseInput(a, c, p)
	ex, e := s.CreateExpense(ctx, ei)
	must(e)
	ii := CreateIncomeInput{OperationKey: "income", AccountID: a.ID, Currency: "RON", Amount: "100", OccurredDate: "2026-09-27"}
	inc, e := s.CreateIncome(ctx, ii)
	must(e)
	ti := transferInput(a, b)
	tr, e := s.CreateTransfer(ctx, ti)
	must(e)
	debt, e := s.CreateReceivable(ctx, CreateReceivableInput{OperationKey: "debt", Debtor: "Friend", Amount: "500", Currency: "RON"})
	must(e)
	ri := CreateRepaymentInput{OperationKey: "repay", ReceivableID: debt.ID, ExpectedReceivableVersion: 1, AccountID: a.ID, Amount: "20", OccurredDate: "2026-09-27"}
	rep, e := s.CreateRepayment(ctx, ri)
	must(e)
	rename := RenameNamedInput{OperationKey: "rename", ID: a.ID, Name: "Renamed", ExpectedVersion: 1}
	a, e = s.RenameAccount(ctx, rename)
	must(e)
	again, e := s.RenameAccount(ctx, rename)
	must(e)
	if again != a || a.Version != 2 || a.OpeningAmount != "1000.00" {
		t.Fatal(a, again)
	}
	rename.Name = "Changed retry"
	_, e = s.RenameAccount(ctx, rename)
	if !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	archive := SetAccountArchivedInput{OperationKey: "archive", ID: a.ID, ExpectedVersion: 2, Archived: true}
	a, e = s.SetAccountArchived(ctx, archive)
	must(e)
	again, e = s.SetAccountArchived(ctx, archive)
	must(e)
	if again != a || !a.Archived || a.Version != 3 {
		t.Fatal(a, again)
	}
	archive.OperationKey = "stale"
	_, e = s.SetAccountArchived(ctx, archive)
	if !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	ei.OperationKey = "new-expense"
	_, e = s.CreateExpense(ctx, ei)
	reject(e)
	ii.OperationKey = "new-income"
	_, e = s.CreateIncome(ctx, ii)
	reject(e)
	ti.OperationKey = "new-transfer"
	_, e = s.CreateTransfer(ctx, ti)
	reject(e)
	ti.SourceAccountID, ti.DestinationAccountID = b.ID, a.ID
	_, e = s.CreateTransfer(ctx, ti)
	reject(e)
	ri.OperationKey = "new-repay"
	ri.ExpectedReceivableVersion = rep.Receivable.Version
	_, e = s.CreateRepayment(ctx, ri)
	reject(e)
	// A correction retains its existing archived account, regardless of amount changes.
	ei.OperationKey = "amend-expense"
	_, e = s.AmendExpense(ctx, AmendExpenseInput{CreateExpenseInput: ei, ID: ex.ID, ExpectedVersion: 1})
	must(e)
	ii.OperationKey = "amend-income"
	_, e = s.AmendIncome(ctx, AmendIncomeInput{CreateIncomeInput: ii, ID: inc.ID, ExpectedVersion: 1})
	must(e)
	ti.OperationKey = "swap-legs"
	_, e = s.AmendTransfer(ctx, AmendTransferInput{CreateTransferInput: ti, ID: tr.ID, ExpectedVersion: 1})
	reject(e)
	ti.SourceAccountID, ti.DestinationAccountID = a.ID, b.ID
	ti.OperationKey = "amend-transfer"
	_, e = s.AmendTransfer(ctx, AmendTransferInput{CreateTransferInput: ti, ID: tr.ID, ExpectedVersion: 1})
	must(e)
	ri.OperationKey = "amend-repay"
	_, e = s.AmendRepayment(ctx, AmendRepaymentInput{CreateRepaymentInput: ri, ID: rep.Repayment.ID, ExpectedVersion: 1})
	must(e)
	// Moving an expense away cannot make a later assignment back grandfathered.
	ei.AccountID = b.ID
	ei.OperationKey = "move-expense"
	_, e = s.AmendExpense(ctx, AmendExpenseInput{CreateExpenseInput: ei, ID: ex.ID, ExpectedVersion: 2})
	must(e)
	ei.AccountID = a.ID
	ei.OperationKey = "return-expense"
	_, e = s.AmendExpense(ctx, AmendExpenseInput{CreateExpenseInput: ei, ID: ex.ID, ExpectedVersion: 3})
	reject(e)
	ri.AccountID = b.ID
	ri.OperationKey = "move-repay"
	ri.ExpectedReceivableVersion = 3
	_, e = s.AmendRepayment(ctx, AmendRepaymentInput{CreateRepaymentInput: ri, ID: rep.Repayment.ID, ExpectedVersion: 2})
	must(e)
	ri.AccountID = a.ID
	ri.OperationKey = "return-repay"
	ri.ExpectedReceivableVersion = 4
	_, e = s.AmendRepayment(ctx, AmendRepaymentInput{CreateRepaymentInput: ri, ID: rep.Repayment.ID, ExpectedVersion: 3})
	reject(e)
	// Reassigning an existing record away is allowed, but assigning it back is new use.
	ii.AccountID = b.ID
	ii.OperationKey = "move-income"
	_, e = s.AmendIncome(ctx, AmendIncomeInput{CreateIncomeInput: ii, ID: inc.ID, ExpectedVersion: 2})
	must(e)
	ii.AccountID = a.ID
	ii.OperationKey = "move-back"
	_, e = s.AmendIncome(ctx, AmendIncomeInput{CreateIncomeInput: ii, ID: inc.ID, ExpectedVersion: 3})
	reject(e)
	o, e := s.RecordBalanceObservation(ctx, RecordBalanceObservationInput{OperationKey: "observe", AccountID: a.ID, Currency: "RON", AsOfDate: "2026-09-27", ActualBalance: "500", Basis: "posted_end_of_day"})
	must(e)
	_, e = s.CreateBalanceAdjustment(ctx, CreateBalanceAdjustmentInput{OperationKey: "adjust", ObservationID: o.ID, ExpectedObservationVersion: o.Version, ExpectedLedgerBalance: o.CurrentLedgerBalance, Amount: o.Difference, Reason: "Difference"})
	reject(e)
	_, e = s.VoidExpense(ctx, VoidExpenseInput{OperationKey: "void", ID: ex.ID, ExpectedVersion: 3})
	must(e)
	a, e = s.SetAccountArchived(ctx, SetAccountArchivedInput{OperationKey: "unarchive", ID: a.ID, ExpectedVersion: 3, Archived: false})
	must(e)
	if a.Archived || a.Version != 4 {
		t.Fatal(a)
	}
	ei.OperationKey = "after-unarchive"
	_, e = s.CreateExpense(ctx, ei)
	must(e)
	history, e := s.Audit(ctx, a.ID)
	must(e)
	if len(history) != 4 {
		t.Fatal(history)
	}
}

func TestAccountArchiveSerializesNewIncome(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	var wg sync.WaitGroup
	wg.Add(2)
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		defer wg.Done()
		<-start
		_, e := s.SetAccountArchived(ctx, SetAccountArchivedInput{OperationKey: "archive", ID: a.ID, ExpectedVersion: 1, Archived: true})
		errs <- e
	}()
	go func() {
		defer wg.Done()
		<-start
		_, e := s.CreateIncome(ctx, CreateIncomeInput{OperationKey: "income", AccountID: a.ID, Currency: "RON", Amount: "10", OccurredDate: "2026-09-01"})
		errs <- e
	}()
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil && !errors.Is(e, ErrValidation) {
			t.Fatal(e)
		}
	}
	d, e := s.Dashboard(ctx)
	if e != nil || !d.Accounts[0].Archived {
		t.Fatal(d, e)
	}
	// Audit sequence must show any successful new transaction preceding archive.
	var after int
	e = s.pool.QueryRow(ctx, "SELECT count(*) FROM audit_entries i JOIN audit_entries a ON a.action='account_archived' WHERE i.action='income_created' AND i.id>a.id").Scan(&after)
	if e != nil || after != 0 {
		t.Fatal(after, e)
	}
}
