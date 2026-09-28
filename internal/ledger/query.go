package ledger

import (
	"context"
	"fmt"
	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
)

// QueryExpenses reads the page, count and exact currency-separated totals from
// one snapshot. Offset pages across separate calls may change after mutations.
func (s *Service) QueryExpenses(ctx context.Context, in ExpenseQuery) (ExpensePage, error) {
	in, err := validateExpenseQuery(in)
	if err != nil {
		return ExpensePage{}, err
	}
	out := ExpensePage{}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	out, err = queryExpenses(ctx, tx, in)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func validateExpenseQuery(in ExpenseQuery) (ExpenseQuery, error) {
	if in.PaymentState != "" && in.PaymentState != "exact" && in.PaymentState != "estimated" && in.PaymentState != "unresolved" && in.PaymentState != "needs_attention" {
		return in, fmt.Errorf("%w: payment_state must be exact, estimated, unresolved or needs_attention", ErrValidation)
	}
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Status == "" {
		in.Status = "posted"
	}
	if in.Limit < 1 || in.Limit > 100 || in.Offset < 0 {
		return in, fmt.Errorf("%w: limit must be 1-100 and offset nonnegative", ErrValidation)
	}
	if in.Status != "posted" && in.Status != "void" && in.Status != "all" {
		return in, fmt.Errorf("%w: status must be posted, void or all", ErrValidation)
	}
	if in.Currency != "" && in.Currency != "RON" && in.Currency != "EUR" && in.Currency != "USD" {
		return in, fmt.Errorf("%w: unsupported currency", ErrValidation)
	}
	for _, v := range []string{in.FromDate, in.ToDate} {
		if v != "" {
			if err := date(v); err != nil {
				return in, err
			}
		}
	}
	if in.FromDate != "" && in.ToDate != "" && in.FromDate > in.ToDate {
		return in, fmt.Errorf("%w: from_date exceeds to_date", ErrValidation)
	}
	return in, nil
}

// queryExpenses requires validated input and borrows the caller's read snapshot.
func queryExpenses(ctx context.Context, tx pgx.Tx, in ExpenseQuery) (ExpensePage, error) {
	out := ExpensePage{Expenses: []Expense{}, Totals: []CurrencyTotal{}, MatchingAllocationTotals: []AllocationTotal{}}
	var err error
	q := db.New(tx)
	filter := db.FilteredExpenseCountParams{PaymentState: in.PaymentState, AccountID: in.AccountID, Currency: in.Currency, FromDate: in.FromDate, ToDate: in.ToDate, Status: in.Status, CategoryID: in.CategoryID, ProjectID: in.ProjectID}
	out.TotalCount, err = q.FilteredExpenseCount(ctx, filter)
	if err != nil {
		return out, err
	}
	ids, err := q.FilteredExpenseIDs(ctx, db.FilteredExpenseIDsParams{PaymentState: in.PaymentState, AccountID: in.AccountID, Currency: in.Currency, FromDate: in.FromDate, ToDate: in.ToDate, Status: in.Status, CategoryID: in.CategoryID, ProjectID: in.ProjectID, PageLimit: int64(in.Limit), PageOffset: int64(in.Offset)})
	if err != nil {
		return out, err
	}
	for _, id := range ids {
		expense, err := readExpense(ctx, tx, id)
		if err != nil {
			return out, err
		}
		out.Expenses = append(out.Expenses, expense)
	}
	totals, err := q.FilteredExpenseTotals(ctx, db.FilteredExpenseTotalsParams(filter))
	if err != nil {
		return out, err
	}
	for _, r := range totals {
		amount, err := formatAggregate(r.Amount)
		if err != nil {
			return out, err
		}
		out.Totals = append(out.Totals, CurrencyTotal{Currency: r.Currency, Amount: amount})
	}
	allocations, err := q.FilteredAllocationTotals(ctx, db.FilteredAllocationTotalsParams(filter))
	if err != nil {
		return out, err
	}
	for _, r := range allocations {
		amount, err := formatAggregate(r.Amount)
		if err != nil {
			return out, err
		}
		out.MatchingAllocationTotals = append(out.MatchingAllocationTotals, AllocationTotal{CategoryID: r.CategoryID, ProjectID: r.ProjectID, Currency: r.Currency, Amount: amount})
	}
	return out, nil
}
