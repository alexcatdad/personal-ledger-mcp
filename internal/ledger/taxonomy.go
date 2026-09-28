package ledger

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
)

func lockNamed(ctx context.Context, tx pgx.Tx, category bool, id string) (Named, error) {
	q := db.New(tx)
	var n Named
	var err error
	if category {
		r, e := q.LockCategory(ctx, id)
		err = e
		n = Named{ID: r.ID, Name: r.Name, Version: r.Version, Archived: r.Archived}
	} else {
		r, e := q.LockProject(ctx, id)
		err = e
		n = Named{ID: r.ID, Name: r.Name, Version: r.Version, Archived: r.Archived}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return n, fmt.Errorf("%w: category or project", ErrNotFound)
	}
	return n, err
}
func (s *Service) updateNamed(ctx context.Context, key string, input any, category, archive bool, id, name string, expected int64) (Named, error) {
	kind := "project"
	if category {
		kind = "category"
	}
	action := "rename"
	if archive {
		action = "archive"
	}
	return mutate(ctx, s, key, action+"_"+kind, input, func(tx pgx.Tx) (Named, error) {
		if expected < 1 || id == "" || (!archive && !validName(name)) {
			return Named{}, fmt.Errorf("%w: ID, positive expected version and valid name required", ErrValidation)
		}
		n, err := lockNamed(ctx, tx, category, id)
		if err != nil {
			return n, err
		}
		if n.Version != expected || (archive && n.Archived) {
			return n, fmt.Errorf("%w: stale version or archived record", ErrConflict)
		}
		if archive {
			n.Archived = true
		} else {
			n.Name = name
		}
		n.Version++
		if category {
			err = db.New(tx).UpdateCategory(ctx, db.UpdateCategoryParams{ID: n.ID, Name: n.Name, Archived: n.Archived})
		} else {
			err = db.New(tx).UpdateProject(ctx, db.UpdateProjectParams{ID: n.ID, Name: n.Name, Archived: n.Archived})
		}
		if err == nil {
			err = audit(ctx, tx, n.ID, kind+"_"+action+"d", n)
		}
		return n, err
	})
}
func (s *Service) RenameCategory(ctx context.Context, in RenameNamedInput) (Named, error) {
	return s.updateNamed(ctx, in.OperationKey, in, true, false, in.ID, in.Name, in.ExpectedVersion)
}
func (s *Service) RenameProject(ctx context.Context, in RenameNamedInput) (Named, error) {
	return s.updateNamed(ctx, in.OperationKey, in, false, false, in.ID, in.Name, in.ExpectedVersion)
}
func (s *Service) ArchiveCategory(ctx context.Context, in ArchiveNamedInput) (Named, error) {
	return s.updateNamed(ctx, in.OperationKey, in, true, true, in.ID, "", in.ExpectedVersion)
}
func (s *Service) ArchiveProject(ctx context.Context, in ArchiveNamedInput) (Named, error) {
	return s.updateNamed(ctx, in.OperationKey, in, false, true, in.ID, "", in.ExpectedVersion)
}

// Taxonomy rows stay locked until commit, serializing archive against new use.
// Amendments can retain archived IDs already attached to this expense.
func validateTaxonomy(ctx context.Context, tx pgx.Tx, allocations []AllocationInput, expenseID string) error {
	old, err := db.New(tx).ReadAllocations(ctx, expenseID)
	if err != nil {
		return err
	}
	oldCategories, oldProjects := map[string]bool{}, map[string]bool{}
	for _, a := range old {
		oldCategories[a.CategoryID] = true
		oldProjects[a.ProjectID] = true
	}
	categories, projects := map[string]bool{}, map[string]bool{}
	for _, a := range allocations {
		categories[a.CategoryID] = true
		if a.ProjectID != "" {
			projects[a.ProjectID] = true
		}
	}
	// Every expense acquires categories first, then projects, each sorted by ID.
	for i, ids := range []map[string]bool{categories, projects} {
		sorted := make([]string, 0, len(ids))
		for id := range ids {
			sorted = append(sorted, id)
		}
		sort.Strings(sorted)
		for _, id := range sorted {
			n, err := lockNamed(ctx, tx, i == 0, id)
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("%w: unknown category or project", ErrValidation)
			}
			if err != nil {
				return err
			}
			previous := oldProjects
			if i == 0 {
				previous = oldCategories
			}
			if n.Archived && !previous[id] {
				return fmt.Errorf("%w: archived category or project cannot be newly assigned", ErrValidation)
			}
		}
	}
	return nil
}
