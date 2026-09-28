package transport

import (
	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"net/http/httptest"
	"testing"
)

func TestMCPAccountLifecycle(t *testing.T) {
	pool := integrationPool(t)
	handler, e := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	a := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "account", Name: "Bank", Currency: "RON", OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	a = call[ledger.Account](t, session, "rename_account", ledger.RenameNamedInput{OperationKey: "rename", ID: a.ID, Name: "Old bank", ExpectedVersion: 1})
	in := ledger.SetAccountArchivedInput{OperationKey: "archive", ID: a.ID, ExpectedVersion: 2, Archived: true}
	a = call[ledger.Account](t, session, "set_account_archived", in)
	again := call[ledger.Account](t, session, "set_account_archived", in)
	if a != again || !a.Archived || a.Version != 3 {
		t.Fatal(a, again)
	}
	rejected(t, session, "set_account_archived", map[string]any{"operation_key": "missing-state", "id": a.ID, "expected_version": 3})
	rejected(t, session, "create_income", ledger.CreateIncomeInput{OperationKey: "income", AccountID: a.ID, Currency: "RON", Amount: "20", OccurredDate: "2026-09-27"})
	dashboard := get[ledger.Dashboard](t, server, "/api/dashboard")
	if len(dashboard.Accounts) != 1 || dashboard.Accounts[0] != a {
		t.Fatal(dashboard, a)
	}
	in.OperationKey = "unarchive"
	in.ExpectedVersion = 3
	in.Archived = false
	a = call[ledger.Account](t, session, "set_account_archived", in)
	if a.Archived || a.Version != 4 {
		t.Fatal(a)
	}
	call[ledger.Income](t, session, "create_income", ledger.CreateIncomeInput{OperationKey: "income", AccountID: a.ID, Currency: "RON", Amount: "20", OccurredDate: "2026-09-27"})
}

func TestMCPOmittedAccountDate(t *testing.T) {
	pool := integrationPool(t)
	handler, err := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	a := call[ledger.Account](t, session, "create_account", map[string]any{"operation_key": "omitted-date", "name": "Bank", "currency": "RON", "opening_amount": "0"})
	if a.OpeningDate == "" {
		t.Fatal(a)
	}
	dashboard := get[ledger.Dashboard](t, server, "/api/dashboard")
	if len(dashboard.Accounts) != 1 || dashboard.Accounts[0].OpeningDate != a.OpeningDate {
		t.Fatal(dashboard)
	}
}
