package transport

import (
	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMCPTransferLifecycle(t *testing.T) {
	pool := integrationPool(t)
	h, err := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	source := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "bank", Name: "Bank", Currency: "RON", OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	dest := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "cash", Name: "Cash", Currency: "RON", OpeningAmount: "0", OpeningDate: "2026-09-01"})
	in := ledger.CreateTransferInput{OperationKey: "withdrawal", SourceAccountID: source.ID, DestinationAccountID: dest.ID, SourceCurrency: "RON", DestinationCurrency: "RON", SourceAmount: "200", DestinationAmount: "200", OccurredDate: "2026-09-27", Description: "ATM withdrawal"}
	transfer := call[ledger.Transfer](t, session, "create_transfer", in)
	retry := call[ledger.Transfer](t, session, "create_transfer", in)
	if !reflect.DeepEqual(transfer, retry) {
		t.Fatal(transfer, retry)
	}
	page := call[ledger.TransferPage](t, session, "query_transfers", ledger.TransferQuery{AccountID: dest.ID})
	api := get[ledger.TransferPage](t, server, "/api/transfers?account_id="+dest.ID)
	if !reflect.DeepEqual(page, api) || page.TotalCount != 1 {
		t.Fatal(page, api)
	}
	assertBalances := func(bank, cash string) {
		t.Helper()
		d := get[ledger.Dashboard](t, server, "/api/dashboard")
		balances := map[string]string{}
		for _, a := range d.Accounts {
			balances[a.ID] = a.Balance
		}
		if balances[source.ID] != bank || balances[dest.ID] != cash || len(d.Expenses) != 0 || len(d.Spending) != 0 {
			t.Fatal(d)
		}
		incomes := call[ledger.IncomePage](t, session, "query_income", ledger.IncomeQuery{})
		if incomes.TotalCount != 0 || len(incomes.Totals) != 0 {
			t.Fatal(incomes)
		}
	}
	assertBalances("800.00", "200.00")
	in.OperationKey = "correction"
	in.SourceAmount = "250"
	in.DestinationAmount = "250"
	amended := call[ledger.Transfer](t, session, "amend_transfer", ledger.AmendTransferInput{CreateTransferInput: in, ID: transfer.ID, ExpectedVersion: 1})
	if amended.Version != 2 {
		t.Fatal(amended)
	}
	assertBalances("750.00", "250.00")
	in.OperationKey = "stale"
	rejected(t, session, "amend_transfer", ledger.AmendTransferInput{CreateTransferInput: in, ID: transfer.ID, ExpectedVersion: 1})
	in.OperationKey = "unequal"
	in.DestinationAmount = "249"
	rejected(t, session, "create_transfer", in)
	assertBalances("750.00", "250.00")
	voided := call[ledger.Transfer](t, session, "void_transfer", ledger.VoidTransferInput{OperationKey: "void", ID: transfer.ID, ExpectedVersion: 2})
	if voided.Version != 3 || voided.Status != "void" {
		t.Fatal(voided)
	}
	assertBalances("1000.00", "0.00")
	all := call[ledger.TransferPage](t, session, "query_transfers", ledger.TransferQuery{Status: "all"})
	if all.TotalCount != 1 || all.Transfers[0].Status != "void" {
		t.Fatal(all)
	}
	audit := call[AuditOutput](t, session, "get_audit", AuditInput{ID: transfer.ID})
	if len(audit.Entries) != 3 {
		t.Fatal(audit)
	}
}
