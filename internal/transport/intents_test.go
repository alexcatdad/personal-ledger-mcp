package transport

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
)

func TestMCPBuyingIntentPurchase(t *testing.T) {
	pool := integrationPool(t)
	handler, err := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	account := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "account", Name: "Bank", Currency: "RON", OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	category := call[ledger.Named](t, session, "create_category", ledger.CreateNamedInput{OperationKey: "category", Name: "Headphones"})
	intent := call[ledger.BuyingIntent](t, session, "create_buying_intent", ledger.CreateBuyingIntentInput{OperationKey: "intent", Title: "Coffee shop headphones", Notes: "Need isolation for work"})
	intent = call[ledger.BuyingIntent](t, session, "add_intent_candidate", ledger.AddIntentCandidateInput{OperationKey: "offer", IntentID: intent.ID, ExpectedVersion: 1, Reference: "Ad A", AdvertisedAmount: "500", Currency: "RON"})
	if intent.Version != 2 || len(intent.Candidates) != 1 || intent.Candidates[0].AdvertisedAmount != "500.00" {
		t.Fatalf("offer not preserved: %+v", intent)
	}
	draft := ledger.ExpenseDraft{AccountID: account.ID, Currency: "RON", Amount: "430", OccurredDate: "2026-09-27", Description: "Negotiated headphones", Allocations: []ledger.AllocationInput{{Amount: "430", CategoryID: category.ID}}}
	bad := draft
	bad.Amount = "431"
	rejected(t, session, "confirm_intent_purchase", ledger.ConfirmIntentPurchaseInput{OperationKey: "bad-purchase", IntentID: intent.ID, ExpectedVersion: 2, CandidateID: intent.Candidates[0].ID, NewExpense: &bad})
	before := get[ledger.Dashboard](t, server, "/api/dashboard")
	if len(before.Expenses) != 0 || before.Accounts[0].Balance != "1000.00" {
		t.Fatalf("failed confirmation wrote money: %+v", before)
	}
	confirmation := ledger.ConfirmIntentPurchaseInput{OperationKey: "purchase", IntentID: intent.ID, ExpectedVersion: 2, CandidateID: intent.Candidates[0].ID, OutcomeNote: "Negotiated down", NewExpense: &draft}
	purchased := call[ledger.BuyingIntent](t, session, "confirm_intent_purchase", confirmation)
	replay := call[ledger.BuyingIntent](t, session, "confirm_intent_purchase", confirmation)
	if !reflect.DeepEqual(purchased, replay) || purchased.Status != "purchased" || purchased.Expense == nil || purchased.Expense.Amount != "430.00" || purchased.Candidates[0].AdvertisedAmount != "500.00" {
		t.Fatalf("purchase/replay invalid: %+v", purchased)
	}
	dashboard := get[ledger.Dashboard](t, server, "/api/dashboard")
	if len(dashboard.Expenses) != 1 || dashboard.Accounts[0].Balance != "570.00" {
		t.Fatalf("duplicate purchase: %+v", dashboard)
	}
	pending := call[ledger.BuyingIntentPage](t, session, "query_buying_intents", ledger.BuyingIntentQuery{})
	if pending.TotalCount != 0 {
		t.Fatalf("purchased intent still pending: %+v", pending)
	}
	all := call[ledger.BuyingIntentPage](t, session, "query_buying_intents", ledger.BuyingIntentQuery{Status: "all"})
	api := get[ledger.BuyingIntentPage](t, server, "/api/buying-intents?status=all")
	if !reflect.DeepEqual(all, api) {
		t.Fatal("MCP/API intents differ")
	}
	// A later financial void is visible without rewriting the historical purchase decision.
	call[ledger.Expense](t, session, "void_expense", ledger.VoidExpenseInput{OperationKey: "void-purchase", ID: purchased.Expense.ID, ExpectedVersion: 1})
	all = call[ledger.BuyingIntentPage](t, session, "query_buying_intents", ledger.BuyingIntentQuery{Status: "all"})
	if all.Intents[0].Status != "purchased" || all.Intents[0].Expense.Status != "void" {
		t.Fatalf("linked financial state stale: %+v", all)
	}
	cancelled := call[ledger.BuyingIntent](t, session, "create_buying_intent", ledger.CreateBuyingIntentInput{OperationKey: "cancel-intent", Title: "Another option"})
	cancelled = call[ledger.BuyingIntent](t, session, "cancel_buying_intent", ledger.CancelBuyingIntentInput{OperationKey: "cancel", IntentID: cancelled.ID, ExpectedVersion: 1, OutcomeNote: "No longer needed"})
	if cancelled.Status != "cancelled" {
		t.Fatal(cancelled)
	}
	after := get[ledger.Dashboard](t, server, "/api/dashboard")
	if after.Accounts[0].Balance != "1000.00" || len(after.Expenses) != 1 {
		t.Fatalf("cancel affected balance: %+v", after)
	}
}
