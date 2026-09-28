package ledger

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestUpdateBuyingIntentReplacementAndRetry(t *testing.T) {
	s, ctx := testService(t)
	original, err := s.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "create", Title: "Headphones", Notes: "Old notes"})
	if err != nil {
		t.Fatal(err)
	}
	original, err = s.AddIntentCandidate(ctx, AddIntentCandidateInput{OperationKey: "offer", IntentID: original.ID, ExpectedVersion: original.Version, Reference: "ad A", Notes: "seller", AdvertisedAmount: "500", Currency: "RON"})
	if err != nil {
		t.Fatal(err)
	}
	in := UpdateBuyingIntentInput{OperationKey: "update", IntentID: original.ID, ExpectedVersion: original.Version, Title: "Coffee shop headphones", Notes: ""}
	updated, err := s.UpdateBuyingIntent(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	expected := original
	expected.Title, expected.Notes, expected.Version = in.Title, "", original.Version+1
	if !reflect.DeepEqual(updated, expected) {
		t.Fatalf("replacement changed unrelated state: got %+v want %+v", updated, expected)
	}
	again, err := s.UpdateBuyingIntent(ctx, in)
	if err != nil || !reflect.DeepEqual(again, updated) {
		t.Fatalf("retry: %+v %v", again, err)
	}
	changed := in
	changed.Notes = "changed retry"
	if _, err = s.UpdateBuyingIntent(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("payload binding: %v", err)
	}
	stale := in
	stale.OperationKey = "stale"
	if _, err = s.UpdateBuyingIntent(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale: %v", err)
	}
	var count int
	var snapshot []byte
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM audit_entries WHERE entity_id=$1 AND action='buying_intent_updated'", original.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit count %d %v", count, err)
	}
	if err = s.pool.QueryRow(ctx, "SELECT snapshot FROM audit_entries WHERE entity_id=$1 AND action='buying_intent_updated'", original.ID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	var audited BuyingIntent
	if err = json.Unmarshal(snapshot, &audited); err != nil || !reflect.DeepEqual(audited, updated) {
		t.Fatalf("audit snapshot %+v %v", audited, err)
	}
}

func TestUpdateBuyingIntentRejectsInvalidAndClosed(t *testing.T) {
	s, ctx := testService(t)
	original, err := s.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "create", Title: "Headphones", Notes: "Preserved"})
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"", " padded", strings.Repeat("x", 201)} {
		_, err = s.UpdateBuyingIntent(ctx, UpdateBuyingIntentInput{OperationKey: "invalid", IntentID: original.ID, ExpectedVersion: 1, Title: title})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("title %q: %v", title, err)
		}
	}
	_, err = s.UpdateBuyingIntent(ctx, UpdateBuyingIntentInput{OperationKey: "invalid", IntentID: original.ID, ExpectedVersion: 1, Title: "Valid", Notes: strings.Repeat("x", 4001)})
	if !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	page, err := s.QueryBuyingIntents(ctx, BuyingIntentQuery{})
	if err != nil || len(page.Intents) != 1 || !reflect.DeepEqual(page.Intents[0], original) {
		t.Fatalf("rollback %+v %v", page, err)
	}
	var audits, operations int
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM audit_entries WHERE entity_id=$1", original.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if err = s.pool.QueryRow(ctx, "SELECT count(*) FROM operation_results WHERE operation_key='invalid'").Scan(&operations); err != nil || audits != 1 || operations != 0 {
		t.Fatalf("rollback audit=%d keys=%d %v", audits, operations, err)
	}
	// A rejected key is reusable after correction; all valid create boundaries also work for updates.
	updated, err := s.UpdateBuyingIntent(ctx, UpdateBuyingIntentInput{OperationKey: "invalid", IntentID: original.ID, ExpectedVersion: 1, Title: strings.Repeat("x", 200), Notes: strings.Repeat("n", 4000)})
	if err != nil || updated.Version != 2 {
		t.Fatalf("corrected key %+v %v", updated, err)
	}
	closed, err := s.CancelBuyingIntent(ctx, CancelBuyingIntentInput{OperationKey: "cancel", IntentID: original.ID, ExpectedVersion: 2, OutcomeNote: "No longer needed"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.UpdateBuyingIntent(ctx, UpdateBuyingIntentInput{OperationKey: "closed", IntentID: original.ID, ExpectedVersion: closed.Version, Title: "Reopen?"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("closed update %v", err)
	}
	page, err = s.QueryBuyingIntents(ctx, BuyingIntentQuery{Status: "cancelled"})
	if err != nil || len(page.Intents) != 1 || !reflect.DeepEqual(page.Intents[0], closed) {
		t.Fatalf("closed preservation %+v %v", page, err)
	}
	_, err = s.UpdateBuyingIntent(ctx, UpdateBuyingIntentInput{OperationKey: "missing", IntentID: "missing", ExpectedVersion: 1, Title: "Missing"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing %v", err)
	}
}

func TestUpdateBuyingIntentPreservesPurchasedOutcome(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	intent, err := s.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "intent", Title: "Parts", Notes: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	intent, err = s.AddIntentCandidate(ctx, AddIntentCandidateInput{OperationKey: "candidate", IntentID: intent.ID, ExpectedVersion: 1, Reference: "Ad", AdvertisedAmount: "500", Currency: "RON"})
	if err != nil {
		t.Fatal(err)
	}
	purchased, err := s.ConfirmIntentPurchase(ctx, ConfirmIntentPurchaseInput{OperationKey: "purchase", IntentID: intent.ID, ExpectedVersion: 2, CandidateID: intent.Candidates[0].ID, OutcomeNote: "Negotiated", NewExpense: intentDraft(a, c, p)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.UpdateBuyingIntent(ctx, UpdateBuyingIntentInput{OperationKey: "closed-update", IntentID: intent.ID, ExpectedVersion: purchased.Version, Title: "Changed", Notes: "Changed"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("purchased update %v", err)
	}
	page, err := s.QueryBuyingIntents(ctx, BuyingIntentQuery{Status: "purchased"})
	if err != nil || len(page.Intents) != 1 || !reflect.DeepEqual(page.Intents[0], purchased) {
		t.Fatalf("purchased preservation %+v %v", page, err)
	}
}
