package transport

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPBulkExpenses(t *testing.T) {
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
	draft := ledger.ExpenseDraft{AccountID: a.ID, Currency: "RON", Amount: "430", OccurredDate: "2026-09-27", Description: "Parts", Allocations: []ledger.AllocationInput{{Amount: "250", CategoryID: c.ID}, {Remainder: true, CategoryID: c.ID}}}
	in := ledger.BulkExpensesInput{OperationKey: "batch", Items: []ledger.BulkExpenseItem{{Action: "create", Expense: &draft}, {Action: "amend", ID: "missing", ExpectedVersion: 1, Expense: &draft}, {Action: "void", ID: "missing-too", ExpectedVersion: 1}}}
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "bulk_expenses", Arguments: in})
	if err != nil || !result.IsError {
		t.Fatalf("expected batch error: %+v %v", result, err)
	}
	if len(result.Content) != 1 {
		t.Fatal(result.Content)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal(result.Content)
	}
	var failure ledger.BulkValidationError
	if err = json.Unmarshal([]byte(text.Text), &failure); err != nil {
		t.Fatal(err, text.Text)
	}
	if len(failure.Issues) != 2 || failure.Issues[0].Index != 1 || failure.Issues[1].Index != 2 {
		t.Fatalf("item errors: %+v", failure)
	}
	d := get[ledger.Dashboard](t, server, "/api/dashboard")
	if len(d.Expenses) != 0 || d.Accounts[0].Balance != "1000.00" {
		t.Fatal(d)
	}
	// A rejected key has no committed result and can be corrected.
	in.Items = in.Items[:1]
	good := call[ledger.BulkExpensesResult](t, session, "bulk_expenses", in)
	retry := call[ledger.BulkExpensesResult](t, session, "bulk_expenses", in)
	if !reflect.DeepEqual(good, retry) || len(good.Expenses) != 1 || good.Expenses[0].Allocations[1].Amount != "180.00" {
		t.Fatal(good, retry)
	}
	d = get[ledger.Dashboard](t, server, "/api/dashboard")
	if !reflect.DeepEqual(d.Expenses, good.Expenses) || d.Accounts[0].Balance != "570.00" {
		t.Fatal(d)
	}
	draft.Amount = "400"
	in.OperationKey = "correction"
	in.Items = []ledger.BulkExpenseItem{{Action: "amend", ID: good.Expenses[0].ID, ExpectedVersion: 1, Expense: &draft}}
	amended := call[ledger.BulkExpensesResult](t, session, "bulk_expenses", in)
	if amended.Expenses[0].Version != 2 || amended.Expenses[0].Allocations[1].Amount != "150.00" {
		t.Fatal(amended)
	}
}
