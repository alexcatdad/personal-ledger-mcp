package ledger

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func testService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		if os.Getenv("REQUIRE_INTEGRATION") == "1" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("TEST_DATABASE_URL unset")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	schema := "ledger_test_" + id()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(url)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		defer admin.Close()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})
	migrationDB := stdlib.OpenDB(*cfg.ConnConfig)
	defer func() { _ = migrationDB.Close() }()
	if e = goose.SetDialect("postgres"); e != nil {
		t.Fatal(e)
	}
	if e = goose.UpToContext(ctx, migrationDB, filepath.Join("..", "..", "migrations"), 1); e != nil {
		t.Fatal(e)
	}
	// Account lifecycle migration must initialize historical accounts safely.
	if _, e = pool.Exec(ctx, "INSERT INTO accounts(id,name,currency,opening_minor,opening_date) VALUES('account-migration-sentinel','Legacy account','RON',12345,'2026-01-01')"); e != nil {
		t.Fatal(e)
	}
	// Upgrading the initial schema must preserve existing records. Subsequent up is a no-op.
	if _, e = pool.Exec(ctx, "INSERT INTO categories(id,name) VALUES('migration-sentinel','Migration sentinel')"); e != nil {
		t.Fatal(e)
	}
	if e = goose.UpContext(ctx, migrationDB, filepath.Join("..", "..", "migrations")); e != nil {
		t.Fatal(e)
	}
	if e = goose.UpContext(ctx, migrationDB, filepath.Join("..", "..", "migrations")); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM categories WHERE id='migration-sentinel' AND version=1 AND archived=false").Scan(&count); e != nil || count != 1 {
		t.Fatalf("migration lost data: %d %v", count, e)
	}
	if _, e = pool.Exec(ctx, "DELETE FROM categories WHERE id='migration-sentinel'"); e != nil {
		t.Fatal(e)
	}

	var accountCount int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM accounts WHERE id='account-migration-sentinel' AND version=1 AND archived=false AND opening_minor=12345").Scan(&accountCount); e != nil || accountCount != 1 {
		t.Fatal(accountCount, e)
	}
	if _, e = pool.Exec(ctx, "DELETE FROM accounts WHERE id='account-migration-sentinel'"); e != nil {
		t.Fatal(e)
	}
	return New(pool), ctx
}
func seed(t *testing.T, s *Service, ctx context.Context) (Account, Named, Named) {
	t.Helper()
	a, e := s.CreateAccount(ctx, CreateAccountInput{OperationKey: "account", Name: "Bank", Currency: "RON", OpeningAmount: "1000", OpeningDate: "2026-09-01"})
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.CreateCategory(ctx, CreateNamedInput{OperationKey: "category", Name: "Suspension"})
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.CreateProject(ctx, CreateNamedInput{OperationKey: "project", Name: "E30"})
	if e != nil {
		t.Fatal(e)
	}
	return a, c, p
}
func expenseInput(a Account, c, p Named) CreateExpenseInput {
	return CreateExpenseInput{OperationKey: "expense", AccountID: a.ID, Currency: "RON", Amount: "430", OccurredDate: "2026-09-27", Description: "E30 parts", Allocations: []AllocationInput{{Amount: "250", CategoryID: c.ID, ProjectID: p.ID}, {Amount: "180", CategoryID: c.ID, ProjectID: p.ID}}}
}
func TestLedgerLifecycle(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	in := expenseInput(a, c, p)
	x, e := s.CreateExpense(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := s.CreateExpense(ctx, in)
	if e != nil || retry.ID != x.ID {
		t.Fatalf("retry %v %v", retry, e)
	}
	d, e := s.Dashboard(ctx)
	if e != nil || len(d.Expenses) != 1 || d.Accounts[0].Balance != "570.00" || d.Spending[0].Amount != "430.00" {
		t.Fatalf("dashboard %+v %v", d, e)
	}
	in.OperationKey = "amend"
	in.Amount = "400"
	in.Allocations = []AllocationInput{{Amount: "400", CategoryID: c.ID, ProjectID: p.ID}}
	amend := AmendExpenseInput{CreateExpenseInput: in, ID: x.ID, ExpectedVersion: 1}
	x, e = s.AmendExpense(ctx, amend)
	if e != nil || x.Version != 2 {
		t.Fatalf("amend %+v %v", x, e)
	}
	amend.OperationKey = "stale"
	if _, e = s.AmendExpense(ctx, amend); !errors.Is(e, ErrConflict) {
		t.Fatalf("stale %v", e)
	}
	x, e = s.VoidExpense(ctx, VoidExpenseInput{OperationKey: "void", ID: x.ID, ExpectedVersion: 2})
	if e != nil || x.Status != "void" || x.Version != 3 {
		t.Fatalf("void %+v %v", x, e)
	}
	d, e = s.Dashboard(ctx)
	if e != nil || d.Accounts[0].Balance != "1000.00" || len(d.Spending) != 0 {
		t.Fatalf("void balance %+v %v", d, e)
	}
	history, e := s.Audit(ctx, x.ID)
	if e != nil || len(history) != 3 {
		t.Fatalf("audit %+v %v", history, e)
	}
	restarted := New(s.pool)
	d, e = restarted.Dashboard(ctx)
	if e != nil || d.Expenses[0].Status != "void" {
		t.Fatalf("persistence %+v %v", d, e)
	}
}
func TestInvalidExpenseAtomicity(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	for _, tc := range []string{"split", "category", "project", "currency", "date", "overflow"} {
		t.Run(tc, func(t *testing.T) {
			in := expenseInput(a, c, p)
			in.OperationKey = "invalid-" + tc
			switch tc {
			case "split":
				in.Allocations[1].Amount = "179"
			case "category":
				in.Allocations[1].CategoryID = "missing"
			case "project":
				in.Allocations[1].ProjectID = "missing"
			case "currency":
				in.Currency = "XYZ"
			case "date":
				in.OccurredDate = "2026-08-31"
			case "overflow":
				in.Amount = "92233720368547758.08"
			}
			if _, e := s.CreateExpense(ctx, in); !errors.Is(e, ErrValidation) {
				t.Fatalf("got %v", e)
			}
			var n int
			if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM expenses").Scan(&n); e != nil || n != 0 {
				t.Fatalf("partial transaction %d %v", n, e)
			}
			if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM audit_entries WHERE action='expense_created'").Scan(&n); e != nil || n != 0 {
				t.Fatalf("partial audit %d %v", n, e)
			}
		})
	}
}
func TestConcurrentRetriesAndEdits(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	in := expenseInput(a, c, p)
	const workers = 8
	var wg sync.WaitGroup
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); x, e := s.CreateExpense(ctx, in); ids <- x.ID; errs <- e }()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if first != id {
			t.Fatal("duplicate ids")
		}
	}
	in.Description = "different"
	if _, e := s.CreateExpense(ctx, in); !errors.Is(e, ErrConflict) {
		t.Fatalf("key misuse %v", e)
	}
	errs = make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.VoidExpense(ctx, VoidExpenseInput{OperationKey: id(), ID: first, ExpectedVersion: 1})
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatalf("successful concurrent edits %d", success)
	}
}
func TestAggregateBeyondInt64(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	for i := 0; i < 2; i++ {
		in := expenseInput(a, c, p)
		in.OperationKey = id()
		in.Amount = "92233720368547758.07"
		in.Allocations = []AllocationInput{{Amount: in.Amount, CategoryID: c.ID, ProjectID: p.ID}}
		if _, e := s.CreateExpense(ctx, in); e != nil {
			t.Fatal(e)
		}
	}
	d, e := s.Dashboard(ctx)
	if e != nil || d.Spending[0].Amount != "184467440737095516.14" {
		t.Fatalf("aggregate %+v %v", d, e)
	}
}
