package transport

import (
	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMCPAdjustmentReplacedByRepayment(t *testing.T) {
	pool := integrationPool(t)
	h, e := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	a := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "bank", Name: "Bank", Currency: "RON", OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	debt := call[ledger.Receivable](t, session, "create_receivable", ledger.CreateReceivableInput{OperationKey: "debt", Debtor: "Friend", Currency: "RON", Amount: "100"})
	o := call[ledger.BalanceObservation](t, session, "record_balance_observation", ledger.RecordBalanceObservationInput{OperationKey: "obs", AccountID: a.ID, Currency: "RON", AsOfDate: "2026-09-27", ActualBalance: "1050", Basis: "posted_end_of_day"})
	adj := call[ledger.BalanceAdjustmentResult](t, session, "create_balance_adjustment", ledger.CreateBalanceAdjustmentInput{OperationKey: "adj", ObservationID: o.ID, ExpectedObservationVersion: 1, ExpectedLedgerBalance: "1000.00", Amount: "50", Reason: "Unidentified credit"})
	in := ledger.BulkTransactionsInput{OperationKey: "replace", Items: []ledger.BulkTransactionItem{{Kind: "adjustment", Action: "void", ID: adj.Adjustment.ID, ExpectedVersion: 1}, {Kind: "repayment", Action: "create", ReceivableID: debt.ID, ExpectedReceivableVersion: 2, Repayment: &ledger.RepaymentDraft{AccountID: a.ID, Amount: "50", OccurredDate: "2026-09-27", Description: "Identified repayment"}}}}
	rejected(t, session, "bulk_transactions", in)
	active := call[ledger.BalanceAdjustmentPage](t, session, "query_balance_adjustments", ledger.BalanceAdjustmentQuery{})
	if active.TotalCount != 1 {
		t.Fatal(active)
	}
	page := call[ledger.RepaymentPage](t, session, "query_repayments", ledger.RepaymentQuery{})
	if page.TotalCount != 0 {
		t.Fatal(page)
	}
	in.Items[1].ExpectedReceivableVersion = 1
	r := call[ledger.BulkTransactionsResult](t, session, "bulk_transactions", in)
	if r.Results[0].Adjustment.Status != "void" || r.Results[1].Repayment.Amount != "50.00" || r.Results[1].Receivable.RemainingAmount != "50.00" {
		t.Fatal(r)
	}
	if replay := call[ledger.BulkTransactionsResult](t, session, "bulk_transactions", in); !reflect.DeepEqual(r, replay) {
		t.Fatal(replay)
	}
	d := get[ledger.Dashboard](t, server, "/api/dashboard")
	if d.Accounts[0].Balance != "1050.00" {
		t.Fatal(d)
	}
	observations := call[ledger.BalanceObservationPage](t, session, "query_balance_observations", ledger.BalanceObservationQuery{})
	if observations.Observations[0].Status != "matched" {
		t.Fatal(observations)
	}
	income := get[ledger.IncomePage](t, server, "/api/income")
	if income.TotalCount != 0 {
		t.Fatal(income)
	}
}
