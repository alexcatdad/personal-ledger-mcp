package transport

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
)

func TestMCPReceivableRepayments(t *testing.T) {
	pool := integrationPool(t)
	h, err := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	account := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "account", Name: "Bank", Currency: "RON", OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	debt := call[ledger.Receivable](t, session, "create_receivable", ledger.CreateReceivableInput{OperationKey: "debt", Debtor: "Demo debtor", Amount: "300", Currency: "RON", DueDate: "2026-10-01", Notes: "Existing debt"})
	d := get[ledger.Dashboard](t, server, "/api/dashboard")
	if d.Accounts[0].Balance != "1000.00" {
		t.Fatal(d)
	}
	in := ledger.CreateRepaymentInput{OperationKey: "payment", ReceivableID: debt.ID, ExpectedReceivableVersion: 1, AccountID: account.ID, Amount: "100", OccurredDate: "2026-09-27", Description: "Partial repayment"}
	paid := call[ledger.RepaymentResult](t, session, "create_repayment", in)
	replay := call[ledger.RepaymentResult](t, session, "create_repayment", in)
	if !reflect.DeepEqual(paid, replay) || paid.Receivable.RemainingAmount != "200.00" || paid.Receivable.Version != 2 {
		t.Fatal(paid, replay)
	}
	page := call[ledger.ReceivablePage](t, session, "query_receivables", ledger.ReceivableQuery{})
	api := get[ledger.ReceivablePage](t, server, "/api/receivables")
	if !reflect.DeepEqual(page, api) || page.TotalCount != 1 || page.OutstandingTotals[0].Amount != "200.00" {
		t.Fatal(page, api)
	}
	repayments := call[ledger.RepaymentPage](t, session, "query_repayments", ledger.RepaymentQuery{ReceivableID: debt.ID})
	if !reflect.DeepEqual(repayments, get[ledger.RepaymentPage](t, server, "/api/repayments?receivable_id="+debt.ID)) || repayments.TotalCount != 1 {
		t.Fatal(repayments)
	}
	in.OperationKey = "stale"
	in.Amount = "50"
	rejected(t, session, "create_repayment", in)
	in.OperationKey = "overpay"
	in.ExpectedReceivableVersion = 2
	in.Amount = "201"
	rejected(t, session, "create_repayment", in)
	rejected(t, session, "void_receivable", ledger.VoidReceivableInput{OperationKey: "bad-void", ID: debt.ID, ExpectedVersion: 2})
	in.OperationKey = "amend"
	in.Amount = "150"
	amended := call[ledger.RepaymentResult](t, session, "amend_repayment", ledger.AmendRepaymentInput{CreateRepaymentInput: in, ID: paid.Repayment.ID, ExpectedVersion: 1})
	if amended.Receivable.RemainingAmount != "150.00" || amended.Receivable.Version != 3 || amended.Repayment.Version != 2 {
		t.Fatal(amended)
	}
	d = get[ledger.Dashboard](t, server, "/api/dashboard")
	if d.Accounts[0].Balance != "1150.00" || len(d.Expenses) != 0 || len(d.Spending) != 0 {
		t.Fatal(d)
	}
	income := get[ledger.IncomePage](t, server, "/api/income")
	if income.TotalCount != 0 || len(income.Totals) != 0 {
		t.Fatal(income)
	}
	reversed := call[ledger.RepaymentResult](t, session, "void_repayment", ledger.VoidRepaymentInput{OperationKey: "reverse", ID: paid.Repayment.ID, ReceivableID: debt.ID, ExpectedVersion: 2, ExpectedReceivableVersion: 3})
	if reversed.Receivable.RemainingAmount != "300.00" || reversed.Receivable.Version != 4 || reversed.Repayment.Status != "void" {
		t.Fatal(reversed)
	}
	d = get[ledger.Dashboard](t, server, "/api/dashboard")
	if d.Accounts[0].Balance != "1000.00" {
		t.Fatal(d)
	}
	voided := call[ledger.Receivable](t, session, "void_receivable", ledger.VoidReceivableInput{OperationKey: "void-debt", ID: debt.ID, ExpectedVersion: 4})
	if voided.Status != "void" {
		t.Fatal(voided)
	}
	page = call[ledger.ReceivablePage](t, session, "query_receivables", ledger.ReceivableQuery{})
	if page.TotalCount != 0 || len(page.OutstandingTotals) != 0 {
		t.Fatal(page)
	}
	audit := call[AuditOutput](t, session, "get_audit", AuditInput{ID: debt.ID})
	if len(audit.Entries) != 5 {
		t.Fatal(audit)
	}
}
