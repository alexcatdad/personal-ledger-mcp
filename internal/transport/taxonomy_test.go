package transport

import (
	"net/http/httptest"
	"testing"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
)

func TestMCPTaxonomyHistory(t *testing.T) {
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
	cat := call[ledger.Named](t, session, "create_category", ledger.CreateNamedInput{OperationKey: "category", Name: "Parts"})
	project := call[ledger.Named](t, session, "create_project", ledger.CreateNamedInput{OperationKey: "project", Name: "Car"})
	input := ledger.CreateExpenseInput{OperationKey: "expense", AccountID: account.ID, Currency: "RON", Amount: "430", OccurredDate: "2026-09-27", Description: "Parts", Allocations: []ledger.AllocationInput{{Amount: "430", CategoryID: cat.ID, ProjectID: project.ID}}}
	expense := call[ledger.Expense](t, session, "create_expense", input)
	cat = call[ledger.Named](t, session, "rename_category", ledger.RenameNamedInput{OperationKey: "rename-category", ID: cat.ID, Name: "Suspension", ExpectedVersion: cat.Version})
	project = call[ledger.Named](t, session, "rename_project", ledger.RenameNamedInput{OperationKey: "rename-project", ID: project.ID, Name: "E30", ExpectedVersion: project.Version})
	if cat.Version != 2 || project.Version != 2 {
		t.Fatalf("versions not advanced: %+v %+v", cat, project)
	}
	archiveCategory := ledger.ArchiveNamedInput{OperationKey: "archive-category", ID: cat.ID, ExpectedVersion: cat.Version}
	archived := call[ledger.Named](t, session, "archive_category", archiveCategory)
	replay := call[ledger.Named](t, session, "archive_category", archiveCategory)
	if archived != replay || !archived.Archived || archived.Version != 3 {
		t.Fatalf("archive/replay mismatch %+v %+v", archived, replay)
	}
	project = call[ledger.Named](t, session, "archive_project", ledger.ArchiveNamedInput{OperationKey: "archive-project", ID: project.ID, ExpectedVersion: project.Version})
	if !project.Archived {
		t.Fatal("project did not archive")
	}
	rejected(t, session, "rename_category", ledger.RenameNamedInput{OperationKey: "stale", ID: cat.ID, Name: "Stale", ExpectedVersion: 2})
	input.OperationKey = "new-expense"
	rejected(t, session, "create_expense", input)
	// Correcting the same historical expense retains its archived references.
	input.OperationKey = "correction"
	input.Description = "Corrected note"
	call[ledger.Expense](t, session, "amend_expense", ledger.AmendExpenseInput{CreateExpenseInput: input, ID: expense.ID, ExpectedVersion: 1})
	dashboard := get[ledger.Dashboard](t, server, "/api/dashboard")
	if len(dashboard.Expenses) != 1 || dashboard.Accounts[0].Balance != "570.00" || dashboard.Spending[0].Amount != "430.00" || dashboard.Spending[0].ProjectName != "E30" {
		t.Fatalf("history/totals changed: %+v", dashboard)
	}
	if !dashboard.Categories[0].Archived || !dashboard.Projects[0].Archived {
		t.Fatal("historical taxonomy state missing")
	}
	for _, id := range []string{cat.ID, project.ID} {
		history := call[AuditOutput](t, session, "get_audit", AuditInput{ID: id})
		if len(history.Entries) != 3 {
			t.Fatalf("expected create/rename/archive history: %+v", history)
		}
	}
}
