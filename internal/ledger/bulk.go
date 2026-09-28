package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
)

type BulkExpensesInput struct {
	OperationKey string            `json:"operation_key"`
	Items        []BulkExpenseItem `json:"items"`
}
type BulkExpenseItem struct {
	Action          string        `json:"action"`
	Expense         *ExpenseDraft `json:"expense,omitempty"`
	ID              string        `json:"id,omitempty"`
	ExpectedVersion int64         `json:"expected_version,omitempty"`
}
type BulkExpensesResult struct {
	Expenses []Expense `json:"expenses"`
}
type BulkItemError struct {
	Index   int    `json:"index"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type BulkValidationError struct {
	Issues []BulkItemError `json:"issues"`
}

func (e *BulkValidationError) Error() string {
	return fmt.Sprintf("validation: bulk operation rejected (%d invalid items)", len(e.Issues))
}
func (e *BulkValidationError) Unwrap() error { return ErrValidation }

// BulkExpenses applies one atomic operation. Savepoints allow collecting independent
// item errors without retaining any changes from a rejected item or batch.
func (s *Service) BulkExpenses(ctx context.Context, in BulkExpensesInput) (BulkExpensesResult, error) {
	return mutate(ctx, s, in.OperationKey, "bulk_expenses", in, func(tx pgx.Tx) (BulkExpensesResult, error) {
		results, err := runBulk(ctx, tx, in.Items, func(item BulkExpenseItem) string { return item.ID }, validateBulkItem, applyBulkItem)
		if err != nil {
			return BulkExpensesResult{}, err
		}
		return BulkExpensesResult{Expenses: results}, nil
	})
}

// runBulk is shared by both public batch contracts. Successful items remain in
// the outer transaction until every item passes; any issue rejects it entirely.
func runBulk[I, O any](ctx context.Context, tx pgx.Tx, items []I, identity func(I) string, validate func(I) error, apply func(context.Context, pgx.Tx, I) (O, error)) ([]O, error) {
	if len(items) == 0 || len(items) > 100 {
		return nil, fmt.Errorf("%w: 1-100 bulk items required", ErrValidation)
	}
	out := make([]O, len(items))
	issues := &BulkValidationError{}
	seen := make(map[string]bool)
	for i, item := range items {
		var e error
		key := identity(item)
		if key != "" && seen[key] {
			e = fmt.Errorf("%w: duplicate entity id in batch", ErrValidation)
		}
		if key != "" {
			seen[key] = true
		}
		if e == nil {
			e = validate(item)
		}
		if e == nil {
			nested, beginErr := tx.Begin(ctx)
			if beginErr != nil {
				return nil, beginErr
			}
			out[i], e = apply(ctx, nested, item)
			if e != nil {
				if rollbackErr := nested.Rollback(ctx); rollbackErr != nil {
					return nil, rollbackErr
				}
			} else if e = nested.Commit(ctx); e != nil {
				return nil, e
			}
		}
		if e != nil {
			e = dbError(e)
			code := ""
			switch {
			case errors.Is(e, ErrValidation):
				code = "validation"
			case errors.Is(e, ErrConflict):
				code = "conflict"
			case errors.Is(e, ErrNotFound):
				code = "not_found"
			default:
				return nil, e
			}
			issues.Issues = append(issues.Issues, BulkItemError{Index: i, Code: code, Message: e.Error()})
		}
	}
	if len(issues.Issues) > 0 {
		return nil, issues
	}
	return out, nil
}

func validateBulkItem(item BulkExpenseItem) error {
	switch item.Action {
	case "create":
		if item.Expense != nil && item.ID == "" && item.ExpectedVersion == 0 {
			return nil
		}
	case "amend":
		if item.Expense != nil && item.ID != "" && item.ExpectedVersion > 0 {
			return nil
		}
	case "void":
		if item.Expense == nil && item.ID != "" && item.ExpectedVersion > 0 {
			return nil
		}
	default:
		return fmt.Errorf("%w: action must be create, amend or void", ErrValidation)
	}
	return fmt.Errorf("%w: fields do not match bulk action", ErrValidation)
}
func applyBulkItem(ctx context.Context, tx pgx.Tx, item BulkExpenseItem) (Expense, error) {
	expenseID, version, action := item.ID, item.ExpectedVersion+1, "expense_amended"
	if item.Action == "create" {
		expenseID, version, action = id(), 1, "expense_created"
	} else if e := lockExpense(ctx, tx, item.ID, item.ExpectedVersion); e != nil {
		return Expense{}, e
	}
	var out Expense
	var e error
	if item.Action == "void" {
		action = "expense_voided"
		e = db.New(tx).VoidExpense(ctx, item.ID)
		if e == nil {
			out, e = readExpense(ctx, tx, item.ID)
		}
	} else {
		d := item.Expense
		out, e = writeExpense(ctx, tx, CreateExpenseInput{Payment: d.Payment, AccountID: d.AccountID, Currency: d.Currency, Amount: d.Amount, OccurredDate: d.OccurredDate, Description: d.Description, Allocations: d.Allocations}, expenseID, version)
	}
	if e == nil {
		e = audit(ctx, tx, out.ID, action, out)
	}
	return out, e
}
