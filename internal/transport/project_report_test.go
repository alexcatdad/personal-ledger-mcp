package transport

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
)

func TestMCPProjectReportParity(t *testing.T) {
	pool := integrationPool(t)
	h, err := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	a := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "a", Name: "Bank", Currency: "RON", OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	c := call[ledger.Named](t, session, "create_category", ledger.CreateNamedInput{OperationKey: "c", Name: "Parts"})
	p := call[ledger.Named](t, session, "create_project", ledger.CreateNamedInput{OperationKey: "p", Name: "E30"})
	call[ledger.Expense](t, session, "create_expense", ledger.CreateExpenseInput{OperationKey: "expense", AccountID: a.ID, Currency: "RON", Amount: "100", OccurredDate: "2026-09-30", Allocations: []ledger.AllocationInput{{Amount: "40", CategoryID: c.ID, ProjectID: p.ID}, {Amount: "60", CategoryID: c.ID}}})
	report := call[ledger.ProjectReport](t, session, "query_project_report", ledger.ProjectReportQuery{ProjectID: p.ID, FromDate: "2026-09-30", ToDate: "2026-09-30", Limit: 1})
	api := get[ledger.ProjectReport](t, server, "/api/projects/"+p.ID+"/report?from_date=2026-09-30&to_date=2026-09-30&limit=1")
	if !reflect.DeepEqual(report, api) || report.SpendingTotals[0].Amount != "40.00" || report.History.Totals[0].Amount != "100.00" {
		t.Fatal(report, api)
	}
	rejected(t, session, "query_project_report", ledger.ProjectReportQuery{ProjectID: p.ID, FromDate: "bad"})
	rejected(t, session, "query_project_report", ledger.ProjectReportQuery{ProjectID: "missing"})
	for _, tc := range []struct {
		path   string
		status int
	}{{"/api/projects/" + p.ID + "/report?from_date=bad", 422}, {"/api/projects/missing/report", 404}} {
		req, err := http.NewRequestWithContext(t.Context(), "GET", server.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+testToken)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatal(res.StatusCode, tc)
		}
	}
}
