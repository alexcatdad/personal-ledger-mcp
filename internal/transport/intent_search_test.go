package transport

import (
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
)

func TestMCPBuyingIntentSearch(t *testing.T) {
	pool := integrationPool(t)
	h, err := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	first := call[ledger.BuyingIntent](t, session, "create_buying_intent", ledger.CreateBuyingIntentInput{OperationKey: "first", Title: "Coffee shop headphones", Notes: "Save 20% on ad_A"})
	second := call[ledger.BuyingIntent](t, session, "create_buying_intent", ledger.CreateBuyingIntentInput{OperationKey: "second", Title: "Travel set", Notes: "HEADPHONES with isolation"})
	closed := call[ledger.BuyingIntent](t, session, "create_buying_intent", ledger.CreateBuyingIntentInput{OperationKey: "third", Title: "Headphones old offer"})
	call[ledger.BuyingIntent](t, session, "cancel_buying_intent", ledger.CancelBuyingIntentInput{OperationKey: "cancel", IntentID: closed.ID, ExpectedVersion: closed.Version})
	for _, tc := range []struct {
		search, status string
		offset         int
		count          int64
		ids            []string
	}{
		{"  hEADphones  ", "", 0, 2, []string{second.ID, first.ID}},
		{"%", "all", 0, 1, []string{first.ID}},
		{"_", "all", 0, 1, []string{first.ID}},
		{"headphones", "cancelled", 0, 1, []string{closed.ID}},
		{"missing", "all", 0, 0, []string{}},
		{"headphones", "", 50, 2, []string{}},
		{"   ", "", 0, 2, []string{second.ID, first.ID}},
	} {
		query := ledger.BuyingIntentQuery{Search: tc.search, Status: tc.status, Offset: tc.offset}
		page := call[ledger.BuyingIntentPage](t, session, "query_buying_intents", query)
		params := url.Values{"search": {tc.search}, "status": {tc.status}, "offset": {strconv.Itoa(tc.offset)}}
		api := get[ledger.BuyingIntentPage](t, server, "/api/buying-intents?"+params.Encode())
		if !reflect.DeepEqual(page, api) || page.TotalCount != tc.count || len(page.Intents) != len(tc.ids) {
			t.Fatalf("%+v: %+v / %+v", tc, page, api)
		}
		for i, id := range tc.ids {
			if page.Intents[i].ID != id {
				t.Fatal(tc, page)
			}
		}
	}
	rejected(t, session, "query_buying_intents", ledger.BuyingIntentQuery{Search: strings.Repeat("ă", 201)})
	page := call[ledger.BuyingIntentPage](t, session, "query_buying_intents", ledger.BuyingIntentQuery{Search: strings.Repeat("ă", 200)})
	if page.TotalCount != 0 {
		t.Fatal(page)
	}
}
