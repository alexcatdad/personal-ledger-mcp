package transport

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
)

func TestMCPBuyingIntentUpdate(t *testing.T) {
	pool := integrationPool(t)
	h, err := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	intent := call[ledger.BuyingIntent](t, session, "create_buying_intent", ledger.CreateBuyingIntentInput{OperationKey: "intent", Title: "Headphones", Notes: "Original context"})
	intent = call[ledger.BuyingIntent](t, session, "add_intent_candidate", ledger.AddIntentCandidateInput{OperationKey: "offer", IntentID: intent.ID, ExpectedVersion: intent.Version, Reference: "Ad A", AdvertisedAmount: "500", Currency: "RON"})
	in := ledger.UpdateBuyingIntentInput{OperationKey: "edit", IntentID: intent.ID, ExpectedVersion: intent.Version, Title: "Coffee shop headphones", Notes: ""}
	rejected(t, session, "update_buying_intent", map[string]any{"operation_key": "missing-notes", "intent_id": intent.ID, "expected_version": intent.Version, "title": in.Title})
	updated := call[ledger.BuyingIntent](t, session, "update_buying_intent", in)
	replay := call[ledger.BuyingIntent](t, session, "update_buying_intent", in)
	if !reflect.DeepEqual(updated, replay) || updated.Version != intent.Version+1 || updated.Title != in.Title || updated.Notes != "" || !reflect.DeepEqual(updated.Candidates, intent.Candidates) {
		t.Fatal(updated, replay)
	}
	in.Title = "Changed retry"
	rejected(t, session, "update_buying_intent", in)
	in.OperationKey = "stale"
	rejected(t, session, "update_buying_intent", in)
	page := get[ledger.BuyingIntentPage](t, server, "/api/buying-intents")
	if len(page.Intents) != 1 || !reflect.DeepEqual(page.Intents[0], updated) {
		t.Fatal(page)
	}
	audit := call[AuditOutput](t, session, "get_audit", AuditInput{ID: intent.ID})
	if len(audit.Entries) != 3 {
		t.Fatal(audit)
	}
	closed := call[ledger.BuyingIntent](t, session, "cancel_buying_intent", ledger.CancelBuyingIntentInput{OperationKey: "cancel", IntentID: intent.ID, ExpectedVersion: updated.Version})
	in.OperationKey, in.ExpectedVersion = "closed", closed.Version
	rejected(t, session, "update_buying_intent", in)
}
