package ledger

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestTaxonomyLifecycle(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	if c.Version != 1 || p.Version != 1 || c.Archived || p.Archived {
		t.Fatal("invalid initial state", c, p)
	}
	expense, err := s.CreateExpense(ctx, expenseInput(a, c, p))
	if err != nil {
		t.Fatal(err)
	}
	rename := RenameNamedInput{OperationKey: "rename", ID: p.ID, Name: "BMW E30", ExpectedVersion: 1}
	renamed, err := s.RenameProject(ctx, rename)
	if err != nil || renamed.Version != 2 {
		t.Fatal(renamed, err)
	}
	replay, err := s.RenameProject(ctx, rename)
	if err != nil || replay != renamed {
		t.Fatal(replay, err)
	}
	rename.OperationKey = "stale"
	_, err = s.RenameProject(ctx, rename)
	if !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	other, err := s.CreateProject(ctx, CreateNamedInput{OperationKey: "other", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RenameProject(ctx, RenameNamedInput{OperationKey: "duplicate", ID: other.ID, Name: renamed.Name, ExpectedVersion: 1})
	if !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	archive := ArchiveNamedInput{OperationKey: "archive", ID: p.ID, ExpectedVersion: 2}
	archived, err := s.ArchiveProject(ctx, archive)
	if err != nil || !archived.Archived || archived.Version != 3 {
		t.Fatal(archived, err)
	}
	replay, err = s.ArchiveProject(ctx, archive)
	if err != nil || replay != archived {
		t.Fatal(replay, err)
	}
	_, err = s.ArchiveCategory(ctx, ArchiveNamedInput{OperationKey: "archive-category", ID: c.ID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	input := expenseInput(a, c, p)
	input.OperationKey = "new-archived"
	if _, err = s.CreateExpense(ctx, input); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
	input.OperationKey = "amend"
	input.Description = "Corrected description"
	amended, err := s.AmendExpense(ctx, AmendExpenseInput{CreateExpenseInput: input, ID: expense.ID, ExpectedVersion: 1})
	if err != nil || amended.Version != 2 {
		t.Fatal(amended, err)
	}
	// Historic names remain correctable without reactivating their IDs.
	renamed, err = s.RenameProject(ctx, RenameNamedInput{OperationKey: "historic-rename", ID: p.ID, ExpectedVersion: 3, Name: "E30 restoration"})
	if err != nil || !renamed.Archived {
		t.Fatal(renamed, err)
	}
	dash, err := s.Dashboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(dash.Expenses) != 1 || dash.Accounts[0].Balance != "570.00" || dash.Spending[0].Amount != "430.00" || dash.Spending[0].ProjectName != renamed.Name {
		t.Fatal(dash)
	}
	found := false
	for _, v := range dash.Projects {
		if v.ID == p.ID {
			found = true
			if v != renamed {
				t.Fatal(v)
			}
		}
	}
	if !found {
		t.Fatal("archived project omitted")
	}
	audit, err := s.Audit(ctx, p.ID)
	if err != nil || len(audit) != 4 {
		t.Fatal(audit, err)
	}
	// An amendment may retain historic IDs but may not newly assign another archived ID.
	other, err = s.ArchiveProject(ctx, ArchiveNamedInput{OperationKey: "archive-other", ID: other.ID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	input.OperationKey = "amend-new-archived"
	input.Allocations[0].ProjectID = other.ID
	if _, err = s.AmendExpense(ctx, AmendExpenseInput{CreateExpenseInput: input, ID: expense.ID, ExpectedVersion: 2}); !errors.Is(err, ErrValidation) {
		t.Fatal(err)
	}
}

func TestConcurrentTaxonomyRenames(t *testing.T) {
	s, ctx := testService(t)
	_, c, _ := seed(t, s, ctx)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.RenameCategory(ctx, RenameNamedInput{OperationKey: fmt.Sprintf("rename-%d", i), ID: c.ID, Name: fmt.Sprintf("name-%d", i), ExpectedVersion: 1})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d", successes)
	}
	audit, err := s.Audit(ctx, c.ID)
	if err != nil || len(audit) != 2 {
		t.Fatal(audit, err)
	}
}
