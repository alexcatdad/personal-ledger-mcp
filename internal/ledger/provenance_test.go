package ledger

import (
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"reflect"
	"strings"
	"testing"
)

func TestAuditApplicationProvenanceAndBulkRollback(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	entries, e := s.Audit(ctx, a.ID)
	if e != nil || len(entries) != 1 {
		t.Fatal(entries, e)
	}
	first := entries[0]
	if first.Source != "application" || first.OperationKey != "account" || len(first.RequestID) != 32 || first.ClientName != "" || first.ClientVersion != "" {
		t.Fatal(first)
	}
	draft := intentDraft(a, c, p)
	in := BulkExpensesInput{OperationKey: "bulk-audit", Items: []BulkExpenseItem{{Action: "create", Expense: draft}, {Action: "create", Expense: draft}}}
	r, e := s.BulkExpenses(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	var request string
	for _, expense := range r.Expenses {
		rows, e := s.Audit(ctx, expense.ID)
		if e != nil || len(rows) != 1 {
			t.Fatal(rows, e)
		}
		row := rows[0]
		if request == "" {
			request = row.RequestID
		}
		if row.Source != "application" || row.RequestID != request || row.OperationKey != in.OperationKey {
			t.Fatal(row)
		}
		if _, ok := row.Snapshot["request_id"]; ok {
			t.Fatal("metadata polluted snapshot")
		}
	}
	replay, e := s.BulkExpenses(ctx, in)
	if e != nil || !reflect.DeepEqual(r, replay) {
		t.Fatal(replay, e)
	}
	rows, e := s.Audit(ctx, r.Expenses[0].ID)
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	bad := *draft
	bad.Amount = "1"
	in.OperationKey = "bulk-rollback"
	in.Items[1].Expense = &bad
	if _, e = s.BulkExpenses(ctx, in); e == nil {
		t.Fatal("invalid batch accepted")
	}
	var count int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM audit_entries WHERE operation_key='bulk-rollback'`).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
}
func TestAuditClientMetadataBounds(t *testing.T) {
	s, ctx := testService(t)
	ctx = WithMCPClient(ctx, "\n"+strings.Repeat("界", 205), strings.Repeat("v", 105)+"\x00")
	a, e := s.CreateAccount(ctx, CreateAccountInput{OperationKey: "bounded", Name: "Bank", Currency: "RON", OpeningAmount: "0", OpeningDate: "2026-09-01"})
	if e != nil {
		t.Fatal(e)
	}
	rows, e := s.Audit(ctx, a.ID)
	if e != nil || len(rows) != 1 || len([]rune(rows[0].ClientName)) != 200 || len(rows[0].ClientVersion) != 100 || strings.ContainsAny(rows[0].ClientName, "\n\x00") {
		t.Fatal(rows, e)
	}
}
func TestAuditProvenanceMigrationLegacyUnknown(t *testing.T) {
	s, ctx := testService(t)
	migrationDB := stdlib.OpenDB(*s.pool.Config().ConnConfig)
	defer func() { _ = migrationDB.Close() }()
	if e := goose.DownToContext(ctx, migrationDB, "../../migrations", 9); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO audit_entries(entity_id,action,snapshot) VALUES('legacy','legacy_created','{"id":"legacy"}')`); e != nil {
		t.Fatal(e)
	}
	if e := goose.UpContext(ctx, migrationDB, "../../migrations"); e != nil {
		t.Fatal(e)
	}
	rows, e := s.Audit(ctx, "legacy")
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	row := rows[0]
	if row.Source != "unknown" || row.OperationKey != "" || row.RequestID != "" || row.ClientName != "" || row.Snapshot["id"] != "legacy" {
		t.Fatal(row)
	}
}
