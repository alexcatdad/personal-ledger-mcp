package transport

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
)

func TestMCPReconciliationAndReplacement(t *testing.T) {
	pool := integrationPool(t)
	h, err := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	account := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "bank", Name: "Bank", Currency: "RON", OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	category := call[ledger.Named](t, session, "create_category", ledger.CreateNamedInput{OperationKey: "fee-category", Name: "Bank fees"})
	obsIn := ledger.RecordBalanceObservationInput{OperationKey: "observation", AccountID: account.ID, Currency: "RON", AsOfDate: "2026-09-27", ActualBalance: "950", Basis: "posted_end_of_day", Notes: "Statement posted balance"}
	obs := call[ledger.BalanceObservation](t, session, "record_balance_observation", obsIn)
	if obs.RecordedLedgerBalance != "1000.00" || obs.CurrentLedgerBalance != "1000.00" || obs.Difference != "-50.00" || obs.Status != "unmatched" {
		t.Fatal(obs)
	}
	if d := get[ledger.Dashboard](t, server, "/api/dashboard"); d.Accounts[0].Balance != "1000.00" {
		t.Fatal(d)
	}
	adjustmentIn := ledger.CreateBalanceAdjustmentInput{OperationKey: "adjustment", ObservationID: obs.ID, ExpectedObservationVersion: 1, ExpectedLedgerBalance: "1000.00", Amount: "-50", Reason: "Explicit unexplained difference"}
	bad := adjustmentIn
	bad.OperationKey = "stale-balance"
	bad.ExpectedLedgerBalance = "999.00"
	rejected(t, session, "create_balance_adjustment", bad)
	result := call[ledger.BalanceAdjustmentResult](t, session, "create_balance_adjustment", adjustmentIn)
	if result.Observation.Status != "matched" || result.Observation.Difference != "0.00" {
		t.Fatal(result)
	}
	if replay := call[ledger.BalanceAdjustmentResult](t, session, "create_balance_adjustment", adjustmentIn); !reflect.DeepEqual(replay, result) {
		t.Fatal(replay)
	}
	page := call[ledger.BalanceObservationPage](t, session, "query_balance_observations", ledger.BalanceObservationQuery{})
	if api := get[ledger.BalanceObservationPage](t, server, "/api/balance-observations"); !reflect.DeepEqual(page, api) {
		t.Fatal(page, api)
	}
	if income := get[ledger.IncomePage](t, server, "/api/income"); income.TotalCount != 0 {
		t.Fatal(income)
	}
	if d := get[ledger.Dashboard](t, server, "/api/dashboard"); d.Accounts[0].Balance != "950.00" || len(d.Expenses) != 0 {
		t.Fatal(d)
	}
	draft := ledger.ExpenseDraft{AccountID: account.ID, Currency: "RON", Amount: "50", OccurredDate: "2026-09-27", Description: "Identified bank fee", Allocations: []ledger.AllocationInput{{Amount: "49", CategoryID: category.ID}}}
	batch := ledger.BulkTransactionsInput{OperationKey: "replace", Items: []ledger.BulkTransactionItem{{Kind: "adjustment", Action: "void", ID: result.Adjustment.ID, ExpectedVersion: 1}, {Kind: "expense", Action: "create", Expense: &draft}}}
	rejected(t, session, "bulk_transactions", batch)
	active := call[ledger.BalanceAdjustmentPage](t, session, "query_balance_adjustments", ledger.BalanceAdjustmentQuery{})
	if active.TotalCount != 1 || active.Adjustments[0].Status != "posted" {
		t.Fatal(active)
	}
	draft.Allocations[0].Amount = "50"
	replaced := call[ledger.BulkTransactionsResult](t, session, "bulk_transactions", batch)
	if replaced.Results[0].Adjustment.Status != "void" || replaced.Results[1].Expense.Amount != "50.00" {
		t.Fatal(replaced)
	}
	if d := get[ledger.Dashboard](t, server, "/api/dashboard"); d.Accounts[0].Balance != "950.00" || len(d.Expenses) != 1 {
		t.Fatal(d)
	}
	page = call[ledger.BalanceObservationPage](t, session, "query_balance_observations", ledger.BalanceObservationQuery{})
	if page.Observations[0].Status != "matched" || page.Observations[0].RecordedLedgerBalance != "1000.00" {
		t.Fatal(page)
	}
}
