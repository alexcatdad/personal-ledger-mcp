package ledger

import (
	"context"
	"fmt"
	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
	"time"
)

type MonthlyReportQuery struct {
	Month     string `json:"month"`
	AccountID string `json:"account_id,omitempty"`
}
type MonthlyAllocationTotal struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}
type MonthlyAccountMovement struct {
	AccountID              string `json:"account_id"`
	Name                   string `json:"name"`
	Currency               string `json:"currency"`
	ExpenseDebits          string `json:"expense_debits"`
	Income                 string `json:"income"`
	TransferIn             string `json:"transfer_in"`
	TransferOut            string `json:"transfer_out"`
	Repayments             string `json:"repayments"`
	Adjustments            string `json:"adjustments"`
	NetChange              string `json:"net_change"`
	BalanceQuality         string `json:"balance_quality"`
	EstimatedExpenseCount  int64  `json:"estimated_expense_count"`
	UnresolvedExpenseCount int64  `json:"unresolved_expense_count"`
}
type MonthlyReport struct {
	Month          string                   `json:"month"`
	FromDate       string                   `json:"from_date"`
	ToDate         string                   `json:"to_date"`
	AccountID      string                   `json:"account_id,omitempty"`
	IncomeTotals   []CurrencyTotal          `json:"income_totals"`
	SpendingTotals []CurrencyTotal          `json:"spending_totals"`
	CategoryTotals []MonthlyAllocationTotal `json:"category_totals"`
	ProjectTotals  []MonthlyAllocationTotal `json:"project_totals"`
	Accounts       []MonthlyAccountMovement `json:"accounts"`
}

func monthBounds(month string) (string, string, error) {
	start, e := time.Parse("2006-01", month)
	if e != nil || start.Year() < 1 || len(month) != 7 {
		return "", "", fmt.Errorf("%w: month must be YYYY-MM with year 0001..9999", ErrValidation)
	}
	return start.Format("2006-01-02"), start.AddDate(0, 1, -1).Format("2006-01-02"), nil
}

// QueryMonthlyReport keeps purchase amounts and account movements separate.
// All sections share one snapshot. Unknown debits are absent from the numerical
// movement subtotal and explicitly counted; transfers and debt principal never
// count as income or spending.
func (s *Service) QueryMonthlyReport(ctx context.Context, in MonthlyReportQuery) (MonthlyReport, error) {
	out := MonthlyReport{Month: in.Month, AccountID: in.AccountID, IncomeTotals: []CurrencyTotal{}, SpendingTotals: []CurrencyTotal{}, CategoryTotals: []MonthlyAllocationTotal{}, ProjectTotals: []MonthlyAllocationTotal{}, Accounts: []MonthlyAccountMovement{}}
	from, to, e := monthBounds(in.Month)
	if e != nil {
		return out, e
	}
	out.FromDate, out.ToDate = from, to
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	q := db.New(tx)
	income, e := q.MonthlyIncomeTotals(ctx, db.MonthlyIncomeTotalsParams{FromDate: from, ToDate: to, AccountID: in.AccountID})
	if e != nil {
		return out, e
	}
	for _, r := range income {
		v, e := formatAggregate(r.Amount)
		if e != nil {
			return out, e
		}
		out.IncomeTotals = append(out.IncomeTotals, CurrencyTotal{Currency: r.Currency, Amount: v})
	}
	spending, e := q.MonthlySpendingTotals(ctx, db.MonthlySpendingTotalsParams{FromDate: from, ToDate: to, AccountID: in.AccountID})
	if e != nil {
		return out, e
	}
	for _, r := range spending {
		v, e := formatAggregate(r.Amount)
		if e != nil {
			return out, e
		}
		out.SpendingTotals = append(out.SpendingTotals, CurrencyTotal{Currency: r.Currency, Amount: v})
	}
	categories, e := q.MonthlyCategoryTotals(ctx, db.MonthlyCategoryTotalsParams{FromDate: from, ToDate: to, AccountID: in.AccountID})
	if e != nil {
		return out, e
	}
	for _, r := range categories {
		v, e := formatAggregate(r.Amount)
		if e != nil {
			return out, e
		}
		out.CategoryTotals = append(out.CategoryTotals, MonthlyAllocationTotal{ID: r.ID, Name: r.Name, Currency: r.Currency, Amount: v})
	}
	projects, e := q.MonthlyProjectTotals(ctx, db.MonthlyProjectTotalsParams{FromDate: from, ToDate: to, AccountID: in.AccountID})
	if e != nil {
		return out, e
	}
	for _, r := range projects {
		v, e := formatAggregate(r.Amount)
		if e != nil {
			return out, e
		}
		out.ProjectTotals = append(out.ProjectTotals, MonthlyAllocationTotal{ID: r.ID, Name: r.Name, Currency: r.Currency, Amount: v})
	}
	accounts, e := q.MonthlyAccountMovements(ctx, db.MonthlyAccountMovementsParams{FromDate: from, ToDate: to, AccountID: in.AccountID})
	if e != nil {
		return out, e
	}
	for _, r := range accounts {
		a := MonthlyAccountMovement{AccountID: r.ID, Name: r.Name, Currency: r.Currency, BalanceQuality: balanceQuality(r.EstimatedCount, r.UnresolvedCount), EstimatedExpenseCount: r.EstimatedCount, UnresolvedExpenseCount: r.UnresolvedCount}
		for _, v := range []struct {
			raw string
			dst *string
		}{{r.ExpenseDebits, &a.ExpenseDebits}, {r.Income, &a.Income}, {r.TransferIn, &a.TransferIn}, {r.TransferOut, &a.TransferOut}, {r.Repayments, &a.Repayments}, {r.Adjustments, &a.Adjustments}, {r.NetChange, &a.NetChange}} {
			*v.dst, e = formatAggregate(v.raw)
			if e != nil {
				return out, e
			}
		}
		out.Accounts = append(out.Accounts, a)
	}
	return out, tx.Commit(ctx)
}
