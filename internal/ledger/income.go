package ledger

import (
	"context"
	"errors"
	"fmt"
	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
)

func writeIncome(ctx context.Context, tx pgx.Tx, in CreateIncomeInput, incomeID string, version int64) (Income, error) {
	out := Income{ID: incomeID, AccountID: in.AccountID, Currency: in.Currency, OccurredDate: in.OccurredDate, Description: in.Description, Status: "posted", Version: version}
	amount, err := parseMoney(in.Amount)
	if err != nil {
		return out, err
	}
	if amount <= 0 || len(in.Description) > 2000 {
		return out, fmt.Errorf("%w: positive income and description up to 2000 characters required", ErrValidation)
	}
	if err = date(in.OccurredDate); err != nil {
		return out, err
	}
	account, err := db.New(tx).LockAccount(ctx, in.AccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, fmt.Errorf("%w: account", ErrNotFound)
	}
	if err != nil {
		return out, err
	}
	if account.Archived {
		old, err := db.New(tx).ReadIncome(ctx, incomeID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		if err != nil || old.AccountID != in.AccountID {
			return out, archivedAccountError()
		}
	}
	if in.Currency != account.Currency {
		return out, fmt.Errorf("%w: income currency must match account", ErrValidation)
	}
	if in.OccurredDate < account.OpeningDate {
		return out, fmt.Errorf("%w: income precedes opening balance", ErrValidation)
	}
	out.Amount = formatMoney(amount)
	err = db.New(tx).UpsertIncome(ctx, db.UpsertIncomeParams{ID: out.ID, AccountID: out.AccountID, AmountMinor: amount, Column4: out.OccurredDate, Description: out.Description, Version: version})
	return out, err
}
func readIncome(ctx context.Context, tx pgx.Tx, id string) (Income, error) {
	r, err := db.New(tx).ReadIncome(ctx, id)
	if err != nil {
		return Income{}, err
	}
	return Income{ID: r.ID, AccountID: r.AccountID, Currency: r.Currency, Amount: formatMoney(r.AmountMinor), OccurredDate: r.OccurredDate, Description: r.Description, Status: r.Status, Version: r.Version}, nil
}
func (s *Service) CreateIncome(ctx context.Context, in CreateIncomeInput) (Income, error) {
	return mutate(ctx, s, in.OperationKey, "create_income", in, func(tx pgx.Tx) (Income, error) {
		out, e := writeIncome(ctx, tx, in, id(), 1)
		if e == nil {
			e = audit(ctx, tx, out.ID, "income_created", out)
		}
		return out, e
	})
}
func lockIncome(ctx context.Context, tx pgx.Tx, incomeID string, expected int64) error {
	row, e := db.New(tx).LockIncome(ctx, incomeID)
	version, status := row.Version, row.Status
	if errors.Is(e, pgx.ErrNoRows) {
		return fmt.Errorf("%w: income", ErrNotFound)
	}
	if e != nil {
		return e
	}
	if version != expected || status != "posted" {
		return fmt.Errorf("%w: stale version or void income", ErrConflict)
	}
	return nil
}
func (s *Service) AmendIncome(ctx context.Context, in AmendIncomeInput) (Income, error) {
	return mutate(ctx, s, in.OperationKey, "amend_income", in, func(tx pgx.Tx) (Income, error) {
		if e := lockIncome(ctx, tx, in.ID, in.ExpectedVersion); e != nil {
			return Income{}, e
		}
		out, e := writeIncome(ctx, tx, in.CreateIncomeInput, in.ID, in.ExpectedVersion+1)
		if e == nil {
			e = audit(ctx, tx, out.ID, "income_amended", out)
		}
		return out, e
	})
}
func (s *Service) VoidIncome(ctx context.Context, in VoidIncomeInput) (Income, error) {
	return mutate(ctx, s, in.OperationKey, "void_income", in, func(tx pgx.Tx) (Income, error) {
		if e := lockIncome(ctx, tx, in.ID, in.ExpectedVersion); e != nil {
			return Income{}, e
		}
		e := db.New(tx).VoidIncome(ctx, in.ID)
		if e != nil {
			return Income{}, e
		}
		out, e := readIncome(ctx, tx, in.ID)
		if e == nil {
			e = audit(ctx, tx, out.ID, "income_voided", out)
		}
		return out, e
	})
}

// QueryIncome reads the page, count and exact currency-separated totals from
// one snapshot. Offset pages across separate calls may change after mutations.
func (s *Service) QueryIncome(ctx context.Context, in IncomeQuery) (IncomePage, error) {
	out := IncomePage{Incomes: []Income{}, Totals: []CurrencyTotal{}}
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Status == "" {
		in.Status = "posted"
	}
	if in.Limit < 1 || in.Limit > 100 || in.Offset < 0 {
		return out, fmt.Errorf("%w: limit must be 1-100 and offset nonnegative", ErrValidation)
	}
	if in.Status != "posted" && in.Status != "void" && in.Status != "all" {
		return out, fmt.Errorf("%w: status must be posted, void or all", ErrValidation)
	}
	if in.Currency != "" && in.Currency != "RON" && in.Currency != "EUR" && in.Currency != "USD" {
		return out, fmt.Errorf("%w: unsupported currency", ErrValidation)
	}
	for _, v := range []string{in.FromDate, in.ToDate} {
		if v != "" {
			if err := date(v); err != nil {
				return out, err
			}
		}
	}
	if in.FromDate != "" && in.ToDate != "" && in.FromDate > in.ToDate {
		return out, fmt.Errorf("%w: from_date exceeds to_date", ErrValidation)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	q := db.New(tx)
	filter := db.FilteredIncomeCountParams{AccountID: in.AccountID, Currency: in.Currency, FromDate: in.FromDate, ToDate: in.ToDate, Status: in.Status}
	out.TotalCount, err = q.FilteredIncomeCount(ctx, filter)
	if err != nil {
		return out, err
	}
	ids, err := q.FilteredIncomeIDs(ctx, db.FilteredIncomeIDsParams{AccountID: in.AccountID, Currency: in.Currency, FromDate: in.FromDate, ToDate: in.ToDate, Status: in.Status, PageLimit: int64(in.Limit), PageOffset: int64(in.Offset)})
	if err != nil {
		return out, err
	}
	for _, id := range ids {
		income, err := readIncome(ctx, tx, id)
		if err != nil {
			return out, err
		}
		out.Incomes = append(out.Incomes, income)
	}
	totals, err := q.FilteredIncomeTotals(ctx, db.FilteredIncomeTotalsParams(filter))
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
	return out, tx.Commit(ctx)
}
