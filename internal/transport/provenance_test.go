package transport

import (
	"encoding/json"
	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type forgedAuditHeaders struct{}

func (forgedAuditHeaders) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("X-Request-ID", "forged-request")
	r.Header.Set("X-Client-Name", "forged-client")
	r.Header.Set("X-Audit-Source", "application")
	return http.DefaultTransport.RoundTrip(r)
}
func TestMCPAuditProvenance(t *testing.T) {
	pool := integrationPool(t)
	h, e := Handler(ledger.New(pool), testToken, "", pool.Ping)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "provenance-client", Version: "1.2.3"}, nil)
	session, e := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: forgedAuditHeaders{}}, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = session.Close() }()
	in := ledger.CreateAccountInput{OperationKey: "provenance-account", Name: "Bank", Currency: "RON", OpeningAmount: "100", OpeningDate: "2026-09-01"}
	a := call[ledger.Account](t, session, "create_account", in)
	rows := get[AuditOutput](t, server, "/api/audit/"+a.ID)
	if len(rows.Entries) != 1 {
		t.Fatal(rows)
	}
	row := rows.Entries[0]
	if row.Source != "mcp" || row.OperationKey != in.OperationKey || row.RequestID == "forged-request" || len(row.RequestID) != 32 || row.ClientName != "provenance-client" || row.ClientVersion != "1.2.3" {
		t.Fatal(row)
	}
	replay := call[ledger.Account](t, session, "create_account", in)
	if !reflect.DeepEqual(a, replay) {
		t.Fatal(replay)
	}
	after := get[AuditOutput](t, server, "/api/audit/"+a.ID)
	if !reflect.DeepEqual(rows, after) {
		t.Fatal(rows, after)
	}
	encoded, e := json.Marshal(after)
	if e != nil || strings.Contains(string(encoded), testToken) || strings.Contains(string(encoded), "forged-") {
		t.Fatal(string(encoded), e)
	}
}
