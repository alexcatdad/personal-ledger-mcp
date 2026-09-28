package transport

import (
	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMCPExplicitRemainder(t *testing.T) {
	pool := integrationPool(t)
	h, err := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	a := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "account", Name: "Bank", Currency: "RON", OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	c := call[ledger.Named](t, session, "create_category", ledger.CreateNamedInput{OperationKey: "category", Name: "Parts"})
	in := ledger.CreateExpenseInput{OperationKey: "expense", AccountID: a.ID, Currency: "RON", Amount: "430", OccurredDate: "2026-09-27", Description: "Parts", Allocations: []ledger.AllocationInput{{Remainder: true, CategoryID: c.ID}, {Amount: "250", CategoryID: c.ID}}}
	x := call[ledger.Expense](t, session, "create_expense", in)
	retry := call[ledger.Expense](t, session, "create_expense", in)
	if !reflect.DeepEqual(x, retry) || x.Allocations[0].Amount != "180.00" {
		t.Fatalf("remainder/retry: %+v", x)
	}
	in.OperationKey = "amend"
	in.Amount = "400"
	amended := call[ledger.Expense](t, session, "amend_expense", ledger.AmendExpenseInput{CreateExpenseInput: in, ID: x.ID, ExpectedVersion: 1})
	if amended.Allocations[0].Amount != "150.00" {
		t.Fatal(amended)
	}
	in.OperationKey = "invalid"
	in.Amount = "250"
	rejected(t, session, "amend_expense", ledger.AmendExpenseInput{CreateExpenseInput: in, ID: x.ID, ExpectedVersion: 2})
	d := get[ledger.Dashboard](t, server, "/api/dashboard")
	if len(d.Expenses) != 1 || !reflect.DeepEqual(d.Expenses[0], amended) || d.Accounts[0].Balance != "600.00" {
		t.Fatalf("persisted state: %+v", d)
	}
}
