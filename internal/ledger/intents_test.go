package ledger

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func intentDraft(a Account, c, p Named) *ExpenseDraft {
	in := expenseInput(a, c, p)
	return &ExpenseDraft{AccountID: in.AccountID, Currency: in.Currency, Amount: in.Amount, OccurredDate: in.OccurredDate, Description: in.Description, Allocations: in.Allocations}
}
func TestBuyingIntentNegotiatedPurchase(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	intent, e := s.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "intent", Title: "Headphones", Notes: "Coffee shops"})
	if e != nil {
		t.Fatal(e)
	}
	candidateInput := AddIntentCandidateInput{OperationKey: "candidate", IntentID: intent.ID, ExpectedVersion: 1, Reference: "https://example.com/ad-a", AdvertisedAmount: "500", Currency: "RON"}
	intent, e = s.AddIntentCandidate(ctx, candidateInput)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := s.AddIntentCandidate(ctx, candidateInput)
	if e != nil || retry.Version != 2 || len(retry.Candidates) != 1 {
		t.Fatalf("candidate retry %+v %v", retry, e)
	}
	in := ConfirmIntentPurchaseInput{OperationKey: "purchase", IntentID: intent.ID, ExpectedVersion: 2, CandidateID: intent.Candidates[0].ID, OutcomeNote: "Negotiated to 430", NewExpense: intentDraft(a, c, p)}
	purchased, e := s.ConfirmIntentPurchase(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if purchased.Status != "purchased" || purchased.Version != 3 || purchased.Expense.Amount != "430.00" || purchased.Candidates[0].AdvertisedAmount != "500.00" {
		t.Fatalf("purchase %+v", purchased)
	}
	again, e := s.ConfirmIntentPurchase(ctx, in)
	if e != nil || again.Expense.ID != purchased.Expense.ID {
		t.Fatalf("retry %+v %v", again, e)
	}
	d, e := s.Dashboard(ctx)
	if e != nil || len(d.Expenses) != 1 || d.Accounts[0].Balance != "570.00" {
		t.Fatalf("debit %+v %v", d, e)
	}
	if _, e = s.VoidExpense(ctx, VoidExpenseInput{OperationKey: "void", ID: purchased.Expense.ID, ExpectedVersion: 1}); e != nil {
		t.Fatal(e)
	}
	page, e := s.QueryBuyingIntents(ctx, BuyingIntentQuery{Status: "purchased"})
	if e != nil || page.TotalCount != 1 || page.Intents[0].Expense.Status != "void" || page.Intents[0].Expense.Version != 2 {
		t.Fatalf("live expense %+v %v", page, e)
	}
	pending, e := s.QueryBuyingIntents(ctx, BuyingIntentQuery{})
	if e != nil || pending.TotalCount != 0 {
		t.Fatalf("pending %+v %v", pending, e)
	}
	var auditCount int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM audit_entries WHERE entity_id=$1", intent.ID).Scan(&auditCount); e != nil {
		t.Fatal(e)
	}
	if auditCount != 3 {
		t.Fatalf("intent audit count %d", auditCount)
	}
}
func TestBuyingIntentLinkAndRollback(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	expense, e := s.CreateExpense(ctx, expenseInput(a, c, p))
	if e != nil {
		t.Fatal(e)
	}
	intent, e := s.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "intent", Title: "Parts"})
	if e != nil {
		t.Fatal(e)
	}
	bad := ConfirmIntentPurchaseInput{OperationKey: "bad", IntentID: intent.ID, ExpectedVersion: 1, NewExpense: intentDraft(a, c, p)}
	bad.NewExpense.Amount = "500"
	if _, e = s.ConfirmIntentPurchase(ctx, bad); !errors.Is(e, ErrValidation) {
		t.Fatalf("invalid split %v", e)
	}
	page, e := s.QueryBuyingIntents(ctx, BuyingIntentQuery{})
	if e != nil || page.Intents[0].Version != 1 {
		t.Fatalf("rollback %+v %v", page, e)
	}
	if _, e = s.ConfirmIntentPurchase(ctx, ConfirmIntentPurchaseInput{OperationKey: "link", IntentID: intent.ID, ExpectedVersion: 1, ExpenseID: expense.ID, ExpectedExpenseVersion: 1}); e != nil {
		t.Fatal(e)
	}
	other, e := s.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "other", Title: "Other"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConfirmIntentPurchase(ctx, ConfirmIntentPurchaseInput{OperationKey: "duplicate-link", IntentID: other.ID, ExpectedVersion: 1, ExpenseID: expense.ID, ExpectedExpenseVersion: 1}); !errors.Is(e, ErrConflict) {
		t.Fatalf("duplicate link %v", e)
	}
	cancelled, e := s.CancelBuyingIntent(ctx, CancelBuyingIntentInput{OperationKey: "cancel", IntentID: other.ID, ExpectedVersion: 1, OutcomeNote: "Already bought"})
	if e != nil || cancelled.Status != "cancelled" {
		t.Fatalf("cancel %+v %v", cancelled, e)
	}
	if _, e = s.AddIntentCandidate(ctx, AddIntentCandidateInput{OperationKey: "closed", IntentID: other.ID, ExpectedVersion: 2, Reference: "ad"}); !errors.Is(e, ErrConflict) {
		t.Fatalf("closed edit %v", e)
	}
	d, e := s.Dashboard(ctx)
	if e != nil || len(d.Expenses) != 1 || d.Accounts[0].Balance != "570.00" {
		t.Fatalf("no extra debit %+v %v", d, e)
	}
}
func TestBuyingIntentConcurrentConfirmation(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	intent, e := s.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "intent", Title: "Purchase"})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, key := range []string{"one", "two"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			_, e := s.ConfirmIntentPurchase(ctx, ConfirmIntentPurchaseInput{OperationKey: key, IntentID: intent.ID, ExpectedVersion: 1, NewExpense: intentDraft(a, c, p)})
			errs <- e
		}(key)
	}
	wg.Wait()
	close(errs)
	success, conflicts := 0, 0
	for e := range errs {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflicts)
	}
	d, e := s.Dashboard(ctx)
	if e != nil || len(d.Expenses) != 1 || d.Accounts[0].Balance != "570.00" {
		t.Fatalf("concurrent debit %+v %v", d, e)
	}
}

func TestBuyingIntentRejectsInvalidCandidatesAndForeignSelection(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	intent, e := s.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "intent", Title: "Headphones"})
	if e != nil {
		t.Fatal(e)
	}
	for i, in := range []AddIntentCandidateInput{
		{Reference: "ad", AdvertisedAmount: "500"},
		{Reference: "ad", Currency: "RON"},
		{Reference: "ad", AdvertisedAmount: "0", Currency: "RON"},
		{Reference: "ad", AdvertisedAmount: "-1", Currency: "RON"},
		{Reference: "ad", AdvertisedAmount: "1.001", Currency: "RON"},
		{Reference: "ad", AdvertisedAmount: "1", Currency: "GBP"},
		{Reference: "   "},
	} {
		in.OperationKey = fmt.Sprintf("invalid-%d", i)
		in.IntentID = intent.ID
		in.ExpectedVersion = 1
		if _, e = s.AddIntentCandidate(ctx, in); !errors.Is(e, ErrValidation) {
			t.Fatalf("candidate %d %v", i, e)
		}
	}
	other, e := s.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "other", Title: "Other"})
	if e != nil {
		t.Fatal(e)
	}
	other, e = s.AddIntentCandidate(ctx, AddIntentCandidateInput{OperationKey: "other-ad", IntentID: other.ID, ExpectedVersion: 1, Reference: "ad without known price"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConfirmIntentPurchase(ctx, ConfirmIntentPurchaseInput{OperationKey: "foreign", IntentID: intent.ID, ExpectedVersion: 1, CandidateID: other.Candidates[0].ID, NewExpense: intentDraft(a, c, p)}); !errors.Is(e, ErrValidation) {
		t.Fatalf("foreign candidate %v", e)
	}
	if _, e = s.CancelBuyingIntent(ctx, CancelBuyingIntentInput{OperationKey: "stale", IntentID: other.ID, ExpectedVersion: 1}); !errors.Is(e, ErrConflict) {
		t.Fatalf("stale cancel %v", e)
	}
	d, e := s.Dashboard(ctx)
	if e != nil || len(d.Expenses) != 0 || d.Accounts[0].Balance != "1000.00" {
		t.Fatalf("rollback %+v %v", d, e)
	}
	page, e := s.QueryBuyingIntents(ctx, BuyingIntentQuery{Limit: 1})
	if e != nil || len(page.Intents) != 1 || page.TotalCount != 2 || page.Intents[0].ID != other.ID {
		t.Fatalf("pagination %+v %v", page, e)
	}
	page, e = s.QueryBuyingIntents(ctx, BuyingIntentQuery{Limit: 1, Offset: 1})
	if e != nil || len(page.Intents) != 1 || page.Intents[0].ID != intent.ID || page.Intents[0].Version != 1 {
		t.Fatalf("rollback page %+v %v", page, e)
	}
}
