package ledger

import (
	"errors"
	"testing"
)

func TestMonthlyReportBounds(t *testing.T) {
	for _, tc := range []struct{ month, from, to string }{{"2028-02", "2028-02-01", "2028-02-29"}, {"2027-02", "2027-02-01", "2027-02-28"}, {"9999-12", "9999-12-01", "9999-12-31"}} {
		from, to, e := monthBounds(tc.month)
		if e != nil || from != tc.from || to != tc.to {
			t.Fatal(tc, from, to, e)
		}
	}
	for _, month := range []string{"", "2026-9", "2026-13", "0000-01", "2026-09-01", " 2026-09", "10000-01"} {
		if _, _, e := monthBounds(month); !errors.Is(e, ErrValidation) {
			t.Fatal(month, e)
		}
	}
}
func TestMonthlyReportSeparatesSpendingAndMovements(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	b := transferAccount(t, s, ctx, "other", "RON")
	in := foreignExpense(a, c, p)
	in.Payment = &ExpensePayment{Currency: "RON", State: "exact", Amount: "50"}
	if _, e := s.CreateExpense(ctx, in); e != nil {
		t.Fatal(e)
	}
	in.OperationKey = "void-me"
	x, e := s.CreateExpense(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.VoidExpense(ctx, VoidExpenseInput{OperationKey: "void", ID: x.ID, ExpectedVersion: 1}); e != nil {
		t.Fatal(e)
	}
	for _, day := range []string{"2026-09-01", "2026-09-30", "2026-10-01"} {
		if _, e = s.CreateIncome(ctx, CreateIncomeInput{OperationKey: "income" + day, AccountID: a.ID, Currency: "RON", Amount: "50", OccurredDate: day, Description: "Income"}); e != nil {
			t.Fatal(e)
		}
	}
	ti := CreateTransferInput{OperationKey: "out", SourceAccountID: a.ID, DestinationAccountID: b.ID, SourceCurrency: "RON", DestinationCurrency: "RON", SourceAmount: "20", DestinationAmount: "20", OccurredDate: "2026-09-27"}
	if _, e = s.CreateTransfer(ctx, ti); e != nil {
		t.Fatal(e)
	}
	ti.OperationKey = "in"
	ti.SourceAccountID = b.ID
	ti.DestinationAccountID = a.ID
	ti.SourceAmount = "7"
	ti.DestinationAmount = "7"
	if _, e = s.CreateTransfer(ctx, ti); e != nil {
		t.Fatal(e)
	}
	debt, e := s.CreateReceivable(ctx, CreateReceivableInput{OperationKey: "debt", Debtor: "Friend", Amount: "30", Currency: "RON"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateRepayment(ctx, CreateRepaymentInput{OperationKey: "repay", ReceivableID: debt.ID, ExpectedReceivableVersion: 1, AccountID: a.ID, Amount: "30", OccurredDate: "2026-09-27"}); e != nil {
		t.Fatal(e)
	}
	obs, e := s.RecordBalanceObservation(ctx, RecordBalanceObservationInput{OperationKey: "obs", AccountID: a.ID, Currency: "RON", AsOfDate: "2026-09-30", ActualBalance: "1070", Basis: "posted_end_of_day"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateBalanceAdjustment(ctx, CreateBalanceAdjustmentInput{OperationKey: "adjust", ObservationID: obs.ID, ExpectedObservationVersion: 1, ExpectedLedgerBalance: "1067.00", Amount: "3", Reason: "Known difference"}); e != nil {
		t.Fatal(e)
	}
	in.OperationKey = "unresolved"
	in.Amount = "2"
	in.Payment = nil
	in.Allocations = []AllocationInput{{Amount: "2", CategoryID: c.ID}}
	if _, e = s.CreateExpense(ctx, in); e != nil {
		t.Fatal(e)
	}
	in.OperationKey = "estimated"
	in.Currency = "USD"
	in.Amount = "1"
	in.Allocations[0].Amount = "1"
	in.Payment = &ExpensePayment{Currency: "RON", State: "estimated", Evidence: quote("4")}
	if _, e = s.CreateExpense(ctx, in); e != nil {
		t.Fatal(e)
	}
	report, e := s.QueryMonthlyReport(ctx, MonthlyReportQuery{Month: "2026-09", AccountID: a.ID})
	if e != nil {
		t.Fatal(e)
	}
	if report.FromDate != "2026-09-01" || report.ToDate != "2026-09-30" || len(report.Accounts) != 1 || len(report.IncomeTotals) != 1 || report.IncomeTotals[0].Amount != "100.00" || report.SpendingTotals[0].Currency != "EUR" || report.SpendingTotals[0].Amount != "12.00" || report.SpendingTotals[1].Currency != "USD" || report.SpendingTotals[1].Amount != "1.00" {
		t.Fatal(report)
	}
	m := report.Accounts[0]
	if m.Currency != "RON" || m.ExpenseDebits != "54.00" || m.Income != "100.00" || m.TransferIn != "7.00" || m.TransferOut != "20.00" || m.Repayments != "30.00" || m.Adjustments != "3.00" || m.NetChange != "66.00" || m.BalanceQuality != "incomplete" || m.UnresolvedExpenseCount != 1 || m.EstimatedExpenseCount != 1 {
		t.Fatal(m)
	}
	if len(report.CategoryTotals) != 2 || report.CategoryTotals[0].Amount != "12.00" {
		t.Fatal(report.CategoryTotals)
	}
	var assigned, unassigned string
	for _, total := range report.ProjectTotals {
		if total.Currency == "EUR" {
			switch total.ID {
			case "":
				unassigned = total.Amount
			case p.ID:
				assigned = total.Amount
			}
		}
	}
	if assigned != "6.00" || unassigned != "6.00" {
		t.Fatal(report.ProjectTotals)
	}
	other, e := s.QueryMonthlyReport(ctx, MonthlyReportQuery{Month: "2026-09", AccountID: b.ID})
	if e != nil || len(other.SpendingTotals) != 0 || len(other.IncomeTotals) != 0 || other.Accounts[0].NetChange != "13.00" {
		t.Fatal(other, e)
	}
	before, e := s.QueryMonthlyReport(ctx, MonthlyReportQuery{Month: "2026-08"})
	if e != nil || len(before.Accounts) != 0 || len(before.SpendingTotals) != 0 {
		t.Fatal(before, e)
	}
	october, e := s.QueryMonthlyReport(ctx, MonthlyReportQuery{Month: "2026-10", AccountID: a.ID})
	if e != nil || october.IncomeTotals[0].Amount != "50.00" || october.Accounts[0].BalanceQuality != "exact" {
		t.Fatal(october, e)
	}
}
func TestMonthlyReportLeapBoundaryAndOverflow(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	for _, day := range []string{"2028-02-01", "2028-02-29", "2028-03-01"} {
		if _, e := s.CreateIncome(ctx, CreateIncomeInput{OperationKey: day, AccountID: a.ID, Currency: "RON", Amount: "92233720368547758.07", OccurredDate: day}); e != nil {
			t.Fatal(e)
		}
	}
	report, e := s.QueryMonthlyReport(ctx, MonthlyReportQuery{Month: "2028-02"})
	if e != nil || report.ToDate != "2028-02-29" || report.IncomeTotals[0].Amount != "184467440737095516.14" || report.Accounts[0].NetChange != "184467440737095516.14" {
		t.Fatal(report, e)
	}
}
