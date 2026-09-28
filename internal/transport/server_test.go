package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pressly/goose/v3"
)

const testToken = "integration-test-token"

type bearerTransport struct{}

func (bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+testToken)
	return http.DefaultTransport.RoundTrip(r)
}

func TestAuthentication(t *testing.T) {
	if _, err := Handler(nil, "", "", nil); err == nil {
		t.Fatal("empty token accepted")
	}
	h, err := Handler(ledger.New(nil), testToken, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/dashboard", "/api/expenses", "/api/income", "/api/transfers", "/api/receivables", "/api/repayments", "/api/balance-observations", "/api/balance-adjustments", "/api/buying-intents", "/api/audit/example", "/mcp", "/openapi.json", "/docs"} {
		for _, token := range []string{"", "Bearer wrong", testToken} {
			r := httptest.NewRequestWithContext(t.Context(), "GET", path, nil)
			r.Header.Set("Authorization", token)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Fatalf("%s with %q: got %d", path, token, w.Code)
			}
		}
	}
}

func TestStaticMethodsAndAuthRouting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/index.html", []byte("public shell"), 0600); err != nil {
		t.Fatal(err)
	}
	h, err := Handler(ledger.New(nil), testToken, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, auth string
		status             int
	}{
		{"GET", "/", "", 200}, {"HEAD", "/", "", 200}, {"POST", "/", "", 405},
		{"GET", "/healthz", "", 200}, {"POST", "/mcp", "", 401},
		{"GET", "/openapi.json", "Bearer " + testToken, 200},
		{"POST", "/api/dashboard", "Bearer " + testToken, 405},
	} {
		r := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil)
		r.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%s %s: got %d want %d", tc.method, tc.path, w.Code, tc.status)
		}
	}
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		if os.Getenv("REQUIRE_INTEGRATION") == "1" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("set TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("transport_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("cleanup schema: %v", err)
		}
	})
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	sqlDB := stdlib.OpenDB(*cfg.ConnConfig)
	defer func() { _ = sqlDB.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, os.DirFS("../../migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	return pool
}
func connect(t *testing.T, server *httptest.Server) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "ledger-integration", Version: "1"}, nil)
	s, err := c.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{}}, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func call[T any](t *testing.T, s *mcp.ClientSession, name string, args any) T {
	t.Helper()
	r, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if r.IsError {
		t.Fatalf("%s: %v", name, r.Content)
	}
	b, err := json.Marshal(r.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func rejected(t *testing.T, s *mcp.ClientSession, name string, args any) {
	t.Helper()
	r, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsError {
		t.Fatalf("%s unexpectedly succeeded", name)
	}
}
func get[T any](t *testing.T, server *httptest.Server, path string) T {
	t.Helper()
	r, err := http.NewRequestWithContext(t.Context(), "GET", server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+testToken)
	res, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		t.Fatalf("%s returned %d", path, res.StatusCode)
	}
	var out T
	if err = json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestMCPPostgresLedger(t *testing.T) {
	pool := integrationPool(t)
	start := func() *httptest.Server {
		h, e := Handler(ledger.New(pool), testToken, "", pool.Ping)
		if e != nil {
			t.Fatal(e)
		}
		return httptest.NewServer(h)
	}
	server := start()
	defer func() { server.Close() }()
	session := connect(t, server)
	defer func() { _ = session.Close() }()
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{"create_account", "create_category", "create_project", "create_expense", "amend_expense", "void_expense", "get_dashboard", "get_audit", "query_expenses", "create_buying_intent", "add_intent_candidate", "cancel_buying_intent", "confirm_intent_purchase", "query_buying_intents"} {
		if !names[name] {
			t.Fatalf("tool missing: %s", name)
		}
	}
	account := call[ledger.Account](t, session, "create_account", ledger.CreateAccountInput{OperationKey: "account", Name: "Bank", Currency: "RON", OpeningAmount: "5000", OpeningDate: "2026-09-01"})
	suspension := call[ledger.Named](t, session, "create_category", ledger.CreateNamedInput{OperationKey: "suspension", Name: "Suspension"})
	misc := call[ledger.Named](t, session, "create_category", ledger.CreateNamedInput{OperationKey: "misc", Name: "Misc"})
	project := call[ledger.Named](t, session, "create_project", ledger.CreateNamedInput{OperationKey: "project", Name: "E30"})
	input := ledger.CreateExpenseInput{OperationKey: "expense", AccountID: account.ID, Currency: "RON", Amount: "430.00", OccurredDate: "2026-09-27", Description: "E30 parts", Allocations: []ledger.AllocationInput{{Amount: "250.00", CategoryID: suspension.ID, ProjectID: project.ID}, {Amount: "180.00", CategoryID: misc.ID, ProjectID: project.ID}}}
	expense := call[ledger.Expense](t, session, "create_expense", input)
	retry := call[ledger.Expense](t, session, "create_expense", input)
	if !reflect.DeepEqual(expense, retry) {
		t.Fatal("retry changed result")
	}
	check := func(balance, spending string, version int64) {
		t.Helper()
		d := get[ledger.Dashboard](t, server, "/api/dashboard")
		if len(d.Accounts) != 1 || d.Accounts[0].Balance != balance || len(d.Expenses) != 1 || d.Expenses[0].Version != version {
			t.Fatalf("incorrect dashboard: %+v", d)
		}
		if spending == "0.00" {
			for _, s := range d.Spending {
				if s.Amount != "0.00" {
					t.Fatalf("void still counted: %+v", d.Spending)
				}
			}
		} else if len(d.Spending) != 1 || d.Spending[0].Amount != spending || d.Spending[0].ProjectID != project.ID {
			t.Fatalf("incorrect spending: %+v", d.Spending)
		}
		m := call[ledger.Dashboard](t, session, "get_dashboard", Empty{})
		if !reflect.DeepEqual(d, m) {
			t.Fatal("API and MCP differ")
		}
	}
	check("4570.00", "430.00", 1)
	filter := ledger.ExpenseQuery{ProjectID: project.ID, CategoryID: suspension.ID, Limit: 1}
	page := call[ledger.ExpensePage](t, session, "query_expenses", filter)
	apiPage := get[ledger.ExpensePage](t, server, "/api/expenses?project_id="+project.ID+"&category_id="+suspension.ID+"&limit=1")
	if !reflect.DeepEqual(page, apiPage) || page.TotalCount != 1 || len(page.Expenses) != 1 || len(page.Totals) != 1 || page.Totals[0].Amount != "430.00" || len(page.MatchingAllocationTotals) != 1 || page.MatchingAllocationTotals[0].Amount != "250.00" {
		t.Fatalf("query contract mismatch: %+v %+v", page, apiPage)
	}
	// Totals describe the complete payment selected by a category, not just its 250 RON allocation.
	filter.Offset = 1
	beyond := call[ledger.ExpensePage](t, session, "query_expenses", filter)
	if len(beyond.Expenses) != 0 || beyond.TotalCount != 1 || beyond.Totals[0].Amount != "430.00" {
		t.Fatalf("page changed totals: %+v", beyond)
	}
	rejected(t, session, "query_expenses", ledger.ExpenseQuery{Limit: 101})
	request, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL+"/api/expenses?from_date=invalid", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, queryErr := server.Client().Do(request)
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	_ = response.Body.Close()
	if response.StatusCode != 422 {
		t.Fatalf("invalid filter HTTP status %d", response.StatusCode)
	}
	mismatch := input
	mismatch.Description = "different"
	rejected(t, session, "create_expense", mismatch)
	invalid := input
	invalid.OperationKey = "bad-split"
	invalid.Amount = "431.00"
	rejected(t, session, "create_expense", invalid)
	check("4570.00", "430.00", 1)
	// A foreign-key failure occurs after the expense insert; the whole write must roll back.
	invalid = input
	invalid.OperationKey = "bad-category"
	invalid.Allocations = append([]ledger.AllocationInput(nil), input.Allocations...)
	invalid.Allocations[1].CategoryID = "missing"
	rejected(t, session, "create_expense", invalid)
	check("4570.00", "430.00", 1)
	amended := input
	amended.OperationKey = "amend"
	amended.Amount = "400.00"
	amended.Allocations = append([]ledger.AllocationInput(nil), input.Allocations...)
	amended.Allocations[1].Amount = "150.00"
	call[ledger.Expense](t, session, "amend_expense", ledger.AmendExpenseInput{CreateExpenseInput: amended, ID: expense.ID, ExpectedVersion: 1})
	check("4600.00", "400.00", 2)
	stale := amended
	stale.OperationKey = "stale"
	rejected(t, session, "amend_expense", ledger.AmendExpenseInput{CreateExpenseInput: stale, ID: expense.ID, ExpectedVersion: 1})
	check("4600.00", "400.00", 2)
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	server.Close()
	server = start()
	session = connect(t, server)
	check("4600.00", "400.00", 2)
	rejected(t, session, "void_expense", ledger.VoidExpenseInput{OperationKey: "stale-void", ID: expense.ID, ExpectedVersion: 1})
	void := ledger.VoidExpenseInput{OperationKey: "void", ID: expense.ID, ExpectedVersion: 2}
	v := call[ledger.Expense](t, session, "void_expense", void)
	if v.Status != "void" || v.Version != 3 {
		t.Fatalf("invalid void: %+v", v)
	}
	call[ledger.Expense](t, session, "void_expense", void)
	check("5000.00", "0.00", 3)
	audit := get[AuditOutput](t, server, "/api/audit/"+expense.ID)
	if len(audit.Entries) != 3 {
		t.Fatalf("expected exactly 3 audit entries, got %d", len(audit.Entries))
	}
	mcpAudit := call[AuditOutput](t, session, "get_audit", AuditInput{ID: expense.ID})
	if !reflect.DeepEqual(audit, mcpAudit) {
		t.Fatal("API and MCP audit differ")
	}
	for i, action := range []string{"expense_created", "expense_amended", "expense_voided"} {
		var snapshot ledger.Expense
		snapshotJSON, err := json.Marshal(audit.Entries[i].Snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(snapshotJSON, &snapshot); err != nil {
			t.Fatal(err)
		}
		wantAmount := "400.00"
		if i == 0 {
			wantAmount = "430.00"
		}
		if snapshot.Amount != wantAmount || snapshot.Version != int64(i+1) {
			t.Fatalf("incorrect historical snapshot: %+v", snapshot)
		}
		if audit.Entries[i].Action != action {
			t.Fatalf("unexpected audit: %+v", audit)
		}
	}
}
