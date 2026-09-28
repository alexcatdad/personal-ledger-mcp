package ledger

import (
	"errors"
	"reflect"
	"testing"
)

func TestBulkRepaymentLifecycleAndVersions(t *testing.T) {
	s, ctx := testService(t)
	a, _, _ := seed(t, s, ctx)
	debt, e := s.CreateReceivable(ctx, CreateReceivableInput{OperationKey: "debt", Debtor: "Friend", Amount: "100", Currency: "RON"})
	if e != nil {
		t.Fatal(e)
	}
	item := BulkTransactionItem{Kind: "repayment", Action: "create", ReceivableID: debt.ID, ExpectedReceivableVersion: 1, Repayment: &RepaymentDraft{AccountID: a.ID, Amount: "20", OccurredDate: "2026-09-27", Description: "Repayment"}}
	in := BulkTransactionsInput{OperationKey: "repay-batch", Items: []BulkTransactionItem{item, item}} // second item sees first version bump and must roll everything back
	if _, e = s.BulkTransactions(ctx, in); e != nil {
		var bulk *BulkValidationError
		if !errors.As(e, &bulk) || len(bulk.Issues) != 1 || bulk.Issues[0].Index != 1 || bulk.Issues[0].Code != "conflict" {
			t.Fatal(e)
		}
	} else {
		t.Fatal("stale debt accepted")
	}
	page, e := s.QueryRepayments(ctx, RepaymentQuery{})
	if e != nil || page.TotalCount != 0 {
		t.Fatal(page, e)
	}
	in.Items[1].ExpectedReceivableVersion = 2
	r, e := s.BulkTransactions(ctx, in)
	if e != nil || r.Results[1].Receivable.RemainingAmount != "60.00" || r.Results[1].Receivable.Version != 3 {
		t.Fatal(r, e)
	}
	rr, e := s.BulkTransactions(ctx, in)
	if e != nil || !reflect.DeepEqual(r, rr) {
		t.Fatal(rr, e)
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "1040.00"})
	first := r.Results[0].Repayment
	item.Action = "amend"
	item.ID = first.ID
	item.ExpectedVersion = 1
	item.ExpectedReceivableVersion = 3
	item.Repayment = &RepaymentDraft{AccountID: a.ID, Amount: "40", OccurredDate: "2026-09-27", Description: "Corrected"}
	amend, e := s.BulkTransactions(ctx, BulkTransactionsInput{OperationKey: "amend-batch", Items: []BulkTransactionItem{item}})
	if e != nil || amend.Results[0].Repayment.Version != 2 || amend.Results[0].Receivable.RemainingAmount != "40.00" {
		t.Fatal(amend, e)
	}
	item.Action = "void"
	item.ExpectedVersion = 2
	item.ExpectedReceivableVersion = 3
	item.Repayment = nil
	if _, e = s.BulkTransactions(ctx, BulkTransactionsInput{OperationKey: "void-batch", Items: []BulkTransactionItem{item}}); e != nil {
		var bulk *BulkValidationError
		if !errors.As(e, &bulk) || len(bulk.Issues) != 1 || bulk.Issues[0].Code != "conflict" {
			t.Fatal(e)
		}
	} else {
		t.Fatal("stale debt accepted")
	}
	item.ExpectedReceivableVersion = 4
	v, e := s.BulkTransactions(ctx, BulkTransactionsInput{OperationKey: "void-batch", Items: []BulkTransactionItem{item}})
	if e != nil || v.Results[0].Repayment.Status != "void" || v.Results[0].Receivable.RemainingAmount != "80.00" {
		t.Fatal(v, e)
	}
	audit, e := s.Audit(ctx, first.ID)
	if e != nil || len(audit) != 3 {
		t.Fatal(audit, e)
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "1020.00"})
	income, e := s.QueryIncome(ctx, IncomeQuery{})
	if e != nil || income.TotalCount != 0 {
		t.Fatal(income, e)
	}
}
func TestBulkRepaymentShapeValidation(t *testing.T) {
	base := BulkTransactionItem{Kind: "repayment", Action: "create", ReceivableID: "debt", ExpectedReceivableVersion: 1, Repayment: &RepaymentDraft{}}
	cases := []BulkTransactionItem{base, base, base, base, base}
	cases[0].ReceivableID = ""
	cases[1].ExpectedReceivableVersion = 0
	cases[2].Income = &IncomeDraft{}
	cases[3].Kind = "income"
	cases[4].Action = "void"
	cases[4].ID = "repay"
	cases[4].ExpectedVersion = 1
	for _, item := range cases {
		if e := validateBulkTransactionItem(item); !errors.Is(e, ErrValidation) {
			t.Fatalf("accepted %+v %v", item, e)
		}
	}
}
