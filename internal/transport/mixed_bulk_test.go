package transport

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPMixedBulk(t *testing.T) {
	pool := integrationPool(t)
	h, err := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	a := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "bank", Name: "Bank", Currency: "RON", OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	b := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "cash", Name: "Cash", Currency: "RON", OpeningAmount: "0", OpeningDate: "2026-09-01"})
	c := call[ledger.Named](t, session, "create_category", ledger.CreateNamedInput{OperationKey: "category", Name: "Parts"})
	expense := ledger.ExpenseDraft{AccountID: a.ID, Currency: "RON", Amount: "430", OccurredDate: "2026-09-27", Description: "Parts", Allocations: []ledger.AllocationInput{{Remainder: true, CategoryID: c.ID}}}
	income := ledger.IncomeDraft{AccountID: a.ID, Currency: "RON", Amount: "500", OccurredDate: "2026-09-27", Description: "Income"}
	transfer := ledger.TransferDraft{SourceAccountID: a.ID, DestinationAccountID: b.ID, SourceCurrency: "RON", DestinationCurrency: "RON", SourceAmount: "200", DestinationAmount: "199", OccurredDate: "2026-09-27", Description: "Withdrawal"}
	in := ledger.BulkTransactionsInput{OperationKey: "statement", Items: []ledger.BulkTransactionItem{{Kind: "expense", Action: "create", Expense: &expense}, {Kind: "income", Action: "create", Income: &income}, {Kind: "transfer", Action: "create", Transfer: &transfer}}}
	rejectedResult, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "bulk_transactions", Arguments: in})
	if err != nil || !rejectedResult.IsError {
		t.Fatalf("%+v %v", rejectedResult, err)
	}
	text, ok := rejectedResult.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal(rejectedResult)
	}
	var issues ledger.BulkValidationError
	if err = json.Unmarshal([]byte(text.Text), &issues); err != nil || len(issues.Issues) != 1 || issues.Issues[0].Index != 2 {
		t.Fatalf("%+v %v", issues, err)
	}
	d := get[ledger.Dashboard](t, server, "/api/dashboard")
	if len(d.Expenses) != 0 || d.Accounts[0].Balance != "1000.00" {
		t.Fatal(d)
	}
	if page := get[ledger.IncomePage](t, server, "/api/income"); page.TotalCount != 0 {
		t.Fatal(page)
	}
	transfer.DestinationAmount = "200"
	success := call[ledger.BulkTransactionsResult](t, session, "bulk_transactions", in)
	retry := call[ledger.BulkTransactionsResult](t, session, "bulk_transactions", in)
	if !reflect.DeepEqual(success, retry) || len(success.Results) != 3 || success.Results[0].Expense == nil || success.Results[1].Income == nil || success.Results[2].Transfer == nil {
		t.Fatal(success, retry)
	}
	d = get[ledger.Dashboard](t, server, "/api/dashboard")
	balances := map[string]string{}
	for _, account := range d.Accounts {
		balances[account.ID] = account.Balance
	}
	if balances[a.ID] != "870.00" || balances[b.ID] != "200.00" {
		t.Fatal(d)
	}
	if page := get[ledger.IncomePage](t, server, "/api/income"); page.TotalCount != 1 || page.Totals[0].Amount != "500.00" {
		t.Fatal(page)
	}
	if page := get[ledger.TransferPage](t, server, "/api/transfers"); page.TotalCount != 1 {
		t.Fatal(page)
	}
	in.OperationKey = "void-statement"
	in.Items = []ledger.BulkTransactionItem{
		{Kind: "expense", Action: "void", ID: success.Results[0].Expense.ID, ExpectedVersion: 1},
		{Kind: "income", Action: "void", ID: success.Results[1].Income.ID, ExpectedVersion: 1},
		{Kind: "transfer", Action: "void", ID: success.Results[2].Transfer.ID, ExpectedVersion: 1},
	}
	call[ledger.BulkTransactionsResult](t, session, "bulk_transactions", in)
	d = get[ledger.Dashboard](t, server, "/api/dashboard")
	for _, account := range d.Accounts {
		balances[account.ID] = account.Balance
	}
	if balances[a.ID] != "1000.00" || balances[b.ID] != "0.00" {
		t.Fatal(d)
	}
}
