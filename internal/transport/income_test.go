package transport

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
)

func TestMCPIncomeLifecycle(t *testing.T) {
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
	in := ledger.CreateIncomeInput{OperationKey: "income", AccountID: account.ID, Currency: "RON", Amount: "2500", OccurredDate: "2026-09-27", Description: "Salary"}
	income := call[ledger.Income](t, session, "create_income", in)
	retry := call[ledger.Income](t, session, "create_income", in)
	if !reflect.DeepEqual(income, retry) {
		t.Fatal(income, retry)
	}
	page := call[ledger.IncomePage](t, session, "query_income", ledger.IncomeQuery{})
	api := get[ledger.IncomePage](t, server, "/api/income")
	if !reflect.DeepEqual(page, api) || page.TotalCount != 1 || len(page.Totals) != 1 || page.Totals[0].Amount != "2500.00" {
		t.Fatal(page, api)
	}
	dashboard := get[ledger.Dashboard](t, server, "/api/dashboard")
	if dashboard.Accounts[0].Balance != "3500.00" || len(dashboard.Expenses) != 0 || len(dashboard.Spending) != 0 {
		t.Fatal(dashboard)
	}
	in.OperationKey = "amend"
	in.Amount = "2600"
	amended := call[ledger.Income](t, session, "amend_income", ledger.AmendIncomeInput{CreateIncomeInput: in, ID: income.ID, ExpectedVersion: 1})
	if amended.Version != 2 || amended.Amount != "2600.00" {
		t.Fatal(amended)
	}
	in.OperationKey = "stale"
	rejected(t, session, "amend_income", ledger.AmendIncomeInput{CreateIncomeInput: in, ID: income.ID, ExpectedVersion: 1})
	in.OperationKey = "bad"
	in.Currency = "EUR"
	rejected(t, session, "create_income", in)
	voided := call[ledger.Income](t, session, "void_income", ledger.VoidIncomeInput{OperationKey: "void", ID: income.ID, ExpectedVersion: 2})
	if voided.Version != 3 || voided.Status != "void" {
		t.Fatal(voided)
	}
	page = call[ledger.IncomePage](t, session, "query_income", ledger.IncomeQuery{Status: "all"})
	api = get[ledger.IncomePage](t, server, "/api/income?status=all")
	if !reflect.DeepEqual(page, api) || page.TotalCount != 1 || len(page.Totals) != 0 {
		t.Fatal(page, api)
	}
	dashboard = get[ledger.Dashboard](t, server, "/api/dashboard")
	if dashboard.Accounts[0].Balance != "1000.00" {
		t.Fatal(dashboard)
	}
	audit := call[AuditOutput](t, session, "get_audit", AuditInput{ID: income.ID})
	if len(audit.Entries) != 3 {
		t.Fatal(audit)
	}
}
