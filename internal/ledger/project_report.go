package ledger

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
)

type ProjectReportQuery struct {
	ProjectID string `json:"project_id"`
	FromDate  string `json:"from_date,omitempty"`
	ToDate    string `json:"to_date,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

type ProjectReport struct {
	Project        Named             `json:"project"`
	FromDate       string            `json:"from_date,omitempty"`
	ToDate         string            `json:"to_date,omitempty"`
	SpendingTotals []CurrencyTotal   `json:"spending_totals"`
	CategoryTotals []AllocationTotal `json:"category_totals"`
	// History includes complete expenses, including allocations to other projects.
	// Its purchase totals must not be interpreted as this project's spending.
	History ExpensePage `json:"history"`
}

// QueryProjectReport reports only posted allocations to the requested project.
// Metadata, full-scope totals and paginated history share one read snapshot.
func (s *Service) QueryProjectReport(ctx context.Context, in ProjectReportQuery) (ProjectReport, error) {
	out := ProjectReport{FromDate: in.FromDate, ToDate: in.ToDate, SpendingTotals: []CurrencyTotal{}, CategoryTotals: []AllocationTotal{}}
	if in.ProjectID == "" {
		return out, fmt.Errorf("%w: project_id required", ErrValidation)
	}
	filter, err := validateExpenseQuery(ExpenseQuery{ProjectID: in.ProjectID, FromDate: in.FromDate, ToDate: in.ToDate, Limit: in.Limit, Offset: in.Offset})
	if err != nil {
		return out, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	p, err := db.New(tx).ReadProject(ctx, in.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, fmt.Errorf("%w: project", ErrNotFound)
	}
	if err != nil {
		return out, err
	}
	out.Project = Named{ID: p.ID, Name: p.Name, Version: p.Version, Archived: p.Archived}
	out.History, err = queryExpenses(ctx, tx, filter)
	if err != nil {
		return out, err
	}
	out.CategoryTotals = out.History.MatchingAllocationTotals
	totals := map[string]*big.Rat{}
	for _, allocation := range out.CategoryTotals {
		amount, ok := new(big.Rat).SetString(allocation.Amount)
		if !ok {
			return out, fmt.Errorf("invalid stored aggregate")
		}
		if totals[allocation.Currency] == nil {
			totals[allocation.Currency] = new(big.Rat)
		}
		totals[allocation.Currency].Add(totals[allocation.Currency], amount)
	}
	currencies := make([]string, 0, len(totals))
	for currency := range totals {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)
	for _, currency := range currencies {
		out.SpendingTotals = append(out.SpendingTotals, CurrencyTotal{Currency: currency, Amount: totals[currency].FloatString(2)})
	}
	return out, tx.Commit(ctx)
}
