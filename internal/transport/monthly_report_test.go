package transport

import (
	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMCPMonthlyReportParity(t *testing.T) {
	pool := integrationPool(t)
	h, e := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	a := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "a", Name: "Bank", Currency: "RON", OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	c := call[ledger.Named](t, session, "create_category", ledger.CreateNamedInput{OperationKey: "c", Name: "Travel"})
	call[ledger.Expense](t, session, "create_expense", ledger.CreateExpenseInput{OperationKey: "expense", AccountID: a.ID, Currency: "EUR", Amount: "10", OccurredDate: "2026-09-30", Payment: &ledger.ExpensePayment{Currency: "RON", State: "exact", Amount: "51"}, Allocations: []ledger.AllocationInput{{Amount: "10", CategoryID: c.ID}}})
	report := call[ledger.MonthlyReport](t, session, "query_monthly_report", ledger.MonthlyReportQuery{Month: "2026-09", AccountID: a.ID})
	api := get[ledger.MonthlyReport](t, server, "/api/reports/monthly?month=2026-09&account_id="+a.ID)
	if !reflect.DeepEqual(report, api) || report.SpendingTotals[0].Currency != "EUR" || report.Accounts[0].NetChange != "-51.00" {
		t.Fatal(report, api)
	}
	rejected(t, session, "query_monthly_report", ledger.MonthlyReportQuery{Month: "2026-13"})
	req, e := http.NewRequestWithContext(t.Context(), "GET", server.URL+"/api/reports/monthly?month=2026-13", nil)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 422 {
		t.Fatal(res.StatusCode)
	}
}
