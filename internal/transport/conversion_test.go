package transport

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
)

func TestMCPCrossCurrencyExpense(t *testing.T) {
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
	category := call[ledger.Named](t, session, "create_category", ledger.CreateNamedInput{OperationKey: "category", Name: "Travel"})
	in := map[string]any{"operation_key": "purchase", "account_id": account.ID, "currency": "EUR", "amount": "10", "date": "2026-09-27", "description": "EUR purchase from RON account", "allocations": []map[string]any{{"amount": "10", "category_id": category.ID}}, "payment": map[string]any{"currency": "RON", "state": "exact", "amount": "51.23"}}
	expense := call[ledger.Expense](t, session, "create_expense", in)
	if expense.Currency != "EUR" || expense.Amount != "10.00" {
		t.Fatal(expense)
	}
	if replay := call[ledger.Expense](t, session, "create_expense", in); !reflect.DeepEqual(expense, replay) {
		t.Fatal(replay)
	}
	d := get[ledger.Dashboard](t, server, "/api/dashboard")
	if d.Accounts[0].Balance != "948.77" {
		t.Fatal(d)
	}
	page := call[ledger.ExpensePage](t, session, "query_expenses", ledger.ExpenseQuery{Currency: "EUR"})
	if api := get[ledger.ExpensePage](t, server, "/api/expenses?currency=EUR"); !reflect.DeepEqual(page, api) || page.TotalCount != 1 {
		t.Fatal(page, api)
	}
	// Missing conversion retains the purchase but must not invent an account debit.
	in["operation_key"] = "unresolved"
	delete(in, "payment")
	pending := call[ledger.Expense](t, session, "create_expense", in)
	d = get[ledger.Dashboard](t, server, "/api/dashboard")
	if d.Accounts[0].Balance != "948.77" {
		t.Fatal(d)
	}
	obs := call[ledger.BalanceObservation](t, session, "record_balance_observation", ledger.RecordBalanceObservationInput{OperationKey: "obs", AccountID: account.ID, Currency: "RON", AsOfDate: "2026-09-27", ActualBalance: "900", Basis: "posted_end_of_day"})
	rejected(t, session, "create_balance_adjustment", ledger.CreateBalanceAdjustmentInput{OperationKey: "no-hide-unresolved", ObservationID: obs.ID, ExpectedObservationVersion: obs.Version, ExpectedLedgerBalance: "948.77", Amount: "-48.77", Reason: "Must resolve conversion first"})
	attention := call[ledger.ExpensePage](t, session, "query_expenses", ledger.ExpenseQuery{PaymentState: "needs_attention", Limit: 1, Offset: 10})
	if attention.TotalCount != 1 || len(attention.Expenses) != 0 || len(attention.Totals) != 1 || attention.Totals[0].Amount != "10.00" {
		t.Fatal(attention)
	}
	if api := get[ledger.ExpensePage](t, server, "/api/expenses?payment_state=needs_attention&limit=1&offset=10"); !reflect.DeepEqual(attention, api) {
		t.Fatal(api)
	}
	rejected(t, session, "query_expenses", ledger.ExpenseQuery{PaymentState: "unknown"})
	// Correct the same record with an exact bank debit, leaving original spending intact.
	in["operation_key"] = "resolve"
	in["id"] = pending.ID
	in["expected_version"] = pending.Version
	in["payment"] = map[string]any{"currency": "RON", "state": "exact", "amount": "48.77"}
	resolved := call[ledger.Expense](t, session, "amend_expense", in)
	if resolved.ID != pending.ID || resolved.Version != 2 || resolved.Currency != "EUR" {
		t.Fatal(resolved)
	}
	attention = call[ledger.ExpensePage](t, session, "query_expenses", ledger.ExpenseQuery{PaymentState: "needs_attention"})
	if attention.TotalCount != 0 {
		t.Fatal(attention)
	}
	d = get[ledger.Dashboard](t, server, "/api/dashboard")
	if d.Accounts[0].Balance != "900.00" {
		t.Fatal(d)
	}
	observations := call[ledger.BalanceObservationPage](t, session, "query_balance_observations", ledger.BalanceObservationQuery{AccountID: account.ID})
	if observations.Observations[0].Difference != "0.00" || observations.Observations[0].Status != "matched" {
		t.Fatal(observations)
	}
}
