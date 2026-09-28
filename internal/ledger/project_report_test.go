package ledger

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestProjectReportValidation(t *testing.T) {
	for _, in := range []ProjectReportQuery{{}, {ProjectID: "p", Limit: -1}, {ProjectID: "p", Limit: 101}, {ProjectID: "p", Offset: -1}, {ProjectID: "p", FromDate: "2026-02-30"}, {ProjectID: "p", ToDate: "bad"}, {ProjectID: "p", FromDate: "2026-09-02", ToDate: "2026-09-01"}} {
		if _, err := (&Service{}).QueryProjectReport(context.Background(), in); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v: %v", in, err)
		}
	}
}

func TestProjectReportAllocationScope(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	other, err := s.CreateProject(ctx, CreateNamedInput{OperationKey: "other", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	in := expenseInput(a, c, p)
	in.Allocations[1].ProjectID = other.ID
	first, err := s.CreateExpense(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.OperationKey = "second"
	in.OccurredDate = "2026-09-28"
	if _, err = s.CreateExpense(ctx, in); err != nil {
		t.Fatal(err)
	}
	in.OperationKey = "euro"
	in.Currency = "EUR"
	in.Payment = &ExpensePayment{Currency: "RON", State: "unresolved"}
	if _, err = s.CreateExpense(ctx, in); err != nil {
		t.Fatal(err)
	}
	in.OperationKey = "outside"
	in.OccurredDate = "2026-10-01"
	if _, err = s.CreateExpense(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err = s.VoidExpense(ctx, VoidExpenseInput{OperationKey: "void", ID: first.ID, ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	report, err := s.QueryProjectReport(ctx, ProjectReportQuery{ProjectID: p.ID, FromDate: "2026-09-27", ToDate: "2026-09-28", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := []CurrencyTotal{{Currency: "EUR", Amount: "250.00"}, {Currency: "RON", Amount: "250.00"}}
	if !reflect.DeepEqual(report.SpendingTotals, want) || report.Project != p || len(report.History.Expenses) != 1 || report.History.TotalCount != 2 || len(report.CategoryTotals) != 2 {
		t.Fatalf("report: %+v", report)
	}
	if report.History.Totals[0].Amount != "430.00" || report.History.Totals[1].Amount != "430.00" || len(report.History.Expenses[0].Allocations) != 2 {
		t.Fatalf("whole expenses: %+v", report.History)
	}
	page, err := s.QueryProjectReport(ctx, ProjectReportQuery{ProjectID: p.ID, FromDate: "2026-09-27", ToDate: "2026-09-28", Limit: 1, Offset: 1})
	if err != nil || !reflect.DeepEqual(page.SpendingTotals, want) || page.History.Expenses[0].ID == report.History.Expenses[0].ID {
		t.Fatalf("page: %+v %v", page, err)
	}
	empty, err := s.QueryProjectReport(ctx, ProjectReportQuery{ProjectID: p.ID, FromDate: "2026-09-27", ToDate: "2026-09-28", Offset: 99})
	if err != nil || len(empty.History.Expenses) != 0 || !reflect.DeepEqual(empty.SpendingTotals, want) {
		t.Fatalf("empty page: %+v %v", empty, err)
	}
	if _, err = s.ArchiveProject(ctx, ArchiveNamedInput{OperationKey: "archive", ID: p.ID, ExpectedVersion: p.Version}); err != nil {
		t.Fatal(err)
	}
	archived, err := s.QueryProjectReport(ctx, ProjectReportQuery{ProjectID: p.ID, FromDate: "2026-09-27", ToDate: "2026-09-28"})
	if err != nil || !archived.Project.Archived || !reflect.DeepEqual(archived.SpendingTotals, want) {
		t.Fatalf("archived: %+v %v", archived, err)
	}
	empty, err = s.QueryProjectReport(ctx, ProjectReportQuery{ProjectID: p.ID, ToDate: "2026-09-27"})
	if err != nil || len(empty.SpendingTotals) != 0 || empty.History.TotalCount != 0 || empty.CategoryTotals == nil {
		t.Fatalf("void excluded: %+v %v", empty, err)
	}
	if _, err = s.QueryProjectReport(ctx, ProjectReportQuery{ProjectID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestProjectReportAggregateBeyondInt64(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	in := expenseInput(a, c, p)
	in.Amount = "92233720368547758.07"
	in.Allocations = []AllocationInput{{Amount: in.Amount, CategoryID: c.ID, ProjectID: p.ID}}
	for _, key := range []string{"huge1", "huge2"} {
		in.OperationKey = key
		if _, err := s.CreateExpense(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	report, err := s.QueryProjectReport(ctx, ProjectReportQuery{ProjectID: p.ID, Limit: 1})
	if err != nil || report.SpendingTotals[0].Amount != "184467440737095516.14" {
		t.Fatalf("%+v %v", report, err)
	}
}
