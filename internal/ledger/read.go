package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
	"math/big"
)

func readExpense(ctx context.Context, tx pgx.Tx, id string) (Expense, error) {
	q := db.New(tx)
	r, err := q.ReadExpense(ctx, id)
	if err != nil {
		return Expense{}, err
	}
	e := Expense{Payment: ExpensePayment{Currency: r.DebitCurrency, State: r.DebitState}, ID: r.ID, AccountID: r.AccountID, Amount: formatMoney(r.AmountMinor), Currency: r.Currency, OccurredDate: r.OccurredDate, Description: r.Description, Status: r.Status, Version: r.Version, Allocations: []Allocation{}}
	if r.DebitMinor.Valid {
		e.Payment.Amount = formatMoney(r.DebitMinor.Int64)
	}
	if r.ConversionEvidence != nil {
		if err = json.Unmarshal(r.ConversionEvidence, &e.Payment.Evidence); err != nil {
			return e, err
		}
	}
	rows, err := q.ReadAllocations(ctx, id)
	if err != nil {
		return e, err
	}
	for _, a := range rows {
		e.Allocations = append(e.Allocations, Allocation{Amount: formatMoney(a.AmountMinor), CategoryID: a.CategoryID, ProjectID: a.ProjectID})
	}
	return e, nil
}
func formatAggregate(s string) (string, error) {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return "", fmt.Errorf("invalid aggregate")
	}
	neg := n.Sign() < 0
	n.Abs(n)
	v := n.String()
	for len(v) < 3 {
		v = "0" + v
	}
	v = v[:len(v)-2] + "." + v[len(v)-2:]
	if neg {
		v = "-" + v
	}
	return v, nil
}

// Dashboard uses one repeatable-read snapshot so all displayed financial totals
// describe the same committed state. Expenses are the latest 100; totals are all-time.
func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	out := Dashboard{Accounts: []Account{}, Expenses: []Expense{}, Categories: []Named{}, Projects: []Named{}, Spending: []Spending{}}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	q := db.New(tx)
	accounts, err := q.AccountBalances(ctx)
	if err != nil {
		return out, err
	}
	for _, r := range accounts {
		balance, err := formatAggregate(r.Balance)
		if err != nil {
			return out, err
		}
		out.Accounts = append(out.Accounts, Account{Version: r.Version, Archived: r.Archived, BalanceQuality: balanceQuality(r.EstimatedCount, r.UnresolvedCount), EstimatedExpenseCount: r.EstimatedCount, UnresolvedExpenseCount: r.UnresolvedCount, ID: r.ID, Name: r.Name, Currency: r.Currency, OpeningAmount: formatMoney(r.OpeningMinor), OpeningDate: r.OpeningDate, Balance: balance})
	}
	categories, err := q.Categories(ctx)
	if err != nil {
		return out, err
	}
	for _, r := range categories {
		out.Categories = append(out.Categories, Named{ID: r.ID, Name: r.Name, Version: r.Version, Archived: r.Archived})
	}
	projects, err := q.Projects(ctx)
	if err != nil {
		return out, err
	}
	for _, r := range projects {
		out.Projects = append(out.Projects, Named{ID: r.ID, Name: r.Name, Version: r.Version, Archived: r.Archived})
	}
	ids, err := q.RecentExpenseIDs(ctx)
	if err != nil {
		return out, err
	}
	for _, id := range ids {
		e, err := readExpense(ctx, tx, id)
		if err != nil {
			return out, err
		}
		out.Expenses = append(out.Expenses, e)
	}
	spending, err := q.ProjectSpending(ctx)
	if err != nil {
		return out, err
	}
	for _, r := range spending {
		amount, err := formatAggregate(r.Amount)
		if err != nil {
			return out, err
		}
		out.Spending = append(out.Spending, Spending{ProjectID: r.ID, ProjectName: r.Name, Currency: r.Currency, Amount: amount})
	}
	return out, tx.Commit(ctx)
}
func (s *Service) Audit(ctx context.Context, entityID string) ([]AuditEntry, error) {
	out := []AuditEntry{}
	rows, err := db.New(s.pool).Audit(ctx, entityID)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		a := AuditEntry{Source: r.Source, OperationKey: r.OperationKey, RequestID: r.RequestID, ClientName: r.ClientName, ClientVersion: r.ClientVersion, ID: r.ID, EntityID: r.EntityID, Action: r.Action, RecordedAt: r.RecordedAt}
		if err = json.Unmarshal(r.Snapshot, &a.Snapshot); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
