package ledger

import (
	"context"
	"errors"
	"fmt"
	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
	"strings"
)

func readReceivable(ctx context.Context, tx pgx.Tx, key string) (Receivable, error) {
	r, e := db.New(tx).ReadReceivable(ctx, key)
	if e != nil {
		return Receivable{}, e
	}
	paid, e := formatAggregate(r.Repaid)
	if e != nil {
		return Receivable{}, e
	}
	minor, e := parseMoney(paid)
	if e != nil {
		return Receivable{}, e
	}
	remaining := r.AmountMinor - minor
	if r.Status == "void" {
		remaining = 0
	}
	return Receivable{ID: r.ID, Debtor: r.Debtor, Amount: formatMoney(r.AmountMinor), Currency: r.Currency, DueDate: r.DueDate, Notes: r.Notes, Version: r.Version, Status: r.Status, RepaidAmount: paid, RemainingAmount: formatMoney(remaining)}, nil
}
func lockReceivable(ctx context.Context, tx pgx.Tx, key string, expected int64) error {
	r, e := db.New(tx).LockReceivable(ctx, key)
	if errors.Is(e, pgx.ErrNoRows) {
		return fmt.Errorf("%w: receivable", ErrNotFound)
	}
	if e != nil {
		return e
	}
	if r.Version != expected || r.Status == "void" {
		return fmt.Errorf("%w: stale or void receivable", ErrConflict)
	}
	return nil
}
func writeReceivable(ctx context.Context, tx pgx.Tx, in CreateReceivableInput, key string, version int64) (Receivable, error) {
	amount, e := parseMoney(in.Amount)
	if e != nil {
		return Receivable{}, e
	}
	if amount <= 0 || strings.TrimSpace(in.Debtor) == "" || len(in.Debtor) > 200 || len(in.Notes) > 4000 {
		return Receivable{}, fmt.Errorf("%w: positive amount, debtor up to 200 and notes up to 4000 characters required", ErrValidation)
	}
	if in.Currency != "RON" && in.Currency != "EUR" && in.Currency != "USD" {
		return Receivable{}, fmt.Errorf("%w: unsupported currency", ErrValidation)
	}
	if in.DueDate != "" {
		if e = date(in.DueDate); e != nil {
			return Receivable{}, e
		}
	}
	e = db.New(tx).UpsertReceivable(ctx, db.UpsertReceivableParams{ID: key, Debtor: in.Debtor, AmountMinor: amount, Currency: in.Currency, DueDate: in.DueDate, Notes: in.Notes, Version: version})
	if e != nil {
		return Receivable{}, e
	}
	return readReceivable(ctx, tx, key)
}
func (s *Service) CreateReceivable(ctx context.Context, in CreateReceivableInput) (Receivable, error) {
	return mutate(ctx, s, in.OperationKey, "create_receivable", in, func(tx pgx.Tx) (Receivable, error) {
		out, e := writeReceivable(ctx, tx, in, id(), 1)
		if e == nil {
			e = audit(ctx, tx, out.ID, "receivable_created", out)
		}
		return out, e
	})
}
func (s *Service) AmendReceivable(ctx context.Context, in AmendReceivableInput) (Receivable, error) {
	return mutate(ctx, s, in.OperationKey, "amend_receivable", in, func(tx pgx.Tx) (Receivable, error) {
		if e := lockReceivable(ctx, tx, in.ID, in.ExpectedVersion); e != nil {
			return Receivable{}, e
		}
		old, e := readReceivable(ctx, tx, in.ID)
		if e != nil {
			return old, e
		}
		paid, e := parseMoney(old.RepaidAmount)
		if e != nil {
			return old, e
		}
		amount, e := parseMoney(in.Amount)
		if e != nil {
			return old, e
		}
		if amount < paid || (paid > 0 && in.Currency != old.Currency) {
			return Receivable{}, fmt.Errorf("%w: principal cannot be less than repayments and currency cannot change while repayments are posted", ErrValidation)
		}
		out, e := writeReceivable(ctx, tx, in.CreateReceivableInput, in.ID, in.ExpectedVersion+1)
		if e == nil {
			e = audit(ctx, tx, out.ID, "receivable_amended", out)
		}
		return out, e
	})
}
func (s *Service) VoidReceivable(ctx context.Context, in VoidReceivableInput) (Receivable, error) {
	return mutate(ctx, s, in.OperationKey, "void_receivable", in, func(tx pgx.Tx) (Receivable, error) {
		if e := lockReceivable(ctx, tx, in.ID, in.ExpectedVersion); e != nil {
			return Receivable{}, e
		}
		old, e := readReceivable(ctx, tx, in.ID)
		if e != nil {
			return old, e
		}
		if old.RepaidAmount != "0.00" {
			return Receivable{}, fmt.Errorf("%w: void posted repayments first", ErrConflict)
		}
		if e = db.New(tx).VoidReceivable(ctx, in.ID); e != nil {
			return Receivable{}, e
		}
		out, e := readReceivable(ctx, tx, in.ID)
		if e == nil {
			e = audit(ctx, tx, out.ID, "receivable_voided", out)
		}
		return out, e
	})
}
func readRepayment(ctx context.Context, tx pgx.Tx, key string) (Repayment, error) {
	r, e := db.New(tx).ReadRepayment(ctx, key)
	return Repayment{ID: r.ID, ReceivableID: r.ReceivableID, AccountID: r.AccountID, Currency: r.Currency, Amount: formatMoney(r.AmountMinor), OccurredDate: r.OccurredDate, Description: r.Description, Status: r.Status, Version: r.Version}, e
}

// Every repayment mutation locks its debt before its repayment and account. This
// serializes total validation and makes two simultaneous payments conflict safely.
func writeRepayment(ctx context.Context, tx pgx.Tx, in CreateRepaymentInput, key string, version int64, oldMinor int64) (RepaymentResult, error) {
	var out RepaymentResult
	debt, e := readReceivable(ctx, tx, in.ReceivableID)
	if e != nil {
		return out, e
	}
	amount, e := parseMoney(in.Amount)
	if e != nil {
		return out, e
	}
	if amount <= 0 || len(in.Description) > 2000 {
		return out, fmt.Errorf("%w: positive repayment and description up to 2000 characters required", ErrValidation)
	}
	if e = date(in.OccurredDate); e != nil {
		return out, e
	}
	principal, e := parseMoney(debt.Amount)
	if e != nil {
		return out, e
	}
	paid, e := parseMoney(debt.RepaidAmount)
	if e != nil {
		return out, e
	}
	// Subtract before adding to avoid overflow near the bigint maximum.
	if amount > principal-(paid-oldMinor) {
		return out, fmt.Errorf("%w: repayment exceeds outstanding debt", ErrValidation)
	}
	account, e := db.New(tx).LockAccount(ctx, in.AccountID)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, fmt.Errorf("%w: account", ErrNotFound)
	}
	if e != nil {
		return out, e
	}
	if account.Archived {
		old, err := db.New(tx).ReadRepayment(ctx, key)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		if err != nil || old.AccountID != in.AccountID {
			return out, archivedAccountError()
		}
	}
	if account.Currency != debt.Currency {
		return out, fmt.Errorf("%w: repayment account currency must match debt", ErrValidation)
	}
	if in.OccurredDate < account.OpeningDate {
		return out, fmt.Errorf("%w: repayment precedes opening balance", ErrValidation)
	}
	e = db.New(tx).UpsertRepayment(ctx, db.UpsertRepaymentParams{ID: key, ReceivableID: in.ReceivableID, AccountID: in.AccountID, AmountMinor: amount, OccurredDate: in.OccurredDate, Description: in.Description, Version: version})
	if e != nil {
		return out, e
	}
	action := "repayment_amended"
	if version == 1 {
		action = "repayment_created"
	}
	return finishRepayment(ctx, tx, key, in.ReceivableID, action)
}
func finishRepayment(ctx context.Context, tx pgx.Tx, key, debtID, action string) (RepaymentResult, error) {
	var out RepaymentResult
	e := db.New(tx).BumpReceivable(ctx, debtID)
	if e != nil {
		return out, e
	}
	out.Repayment, e = readRepayment(ctx, tx, key)
	if e != nil {
		return out, e
	}
	out.Receivable, e = readReceivable(ctx, tx, debtID)
	if e != nil {
		return out, e
	}
	if e = audit(ctx, tx, key, action, out.Repayment); e != nil {
		return out, e
	}
	e = audit(ctx, tx, debtID, action, out.Receivable)
	return out, e
}
func (s *Service) CreateRepayment(ctx context.Context, in CreateRepaymentInput) (RepaymentResult, error) {
	return mutate(ctx, s, in.OperationKey, "create_repayment", in, func(tx pgx.Tx) (RepaymentResult, error) {
		if e := lockReceivable(ctx, tx, in.ReceivableID, in.ExpectedReceivableVersion); e != nil {
			return RepaymentResult{}, e
		}
		return writeRepayment(ctx, tx, in, id(), 1, 0)
	})
}
func lockRepayment(ctx context.Context, tx pgx.Tx, key, debtID string, expected int64) (int64, error) {
	r, e := db.New(tx).LockRepayment(ctx, key)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, fmt.Errorf("%w: repayment", ErrNotFound)
	}
	if e != nil {
		return 0, e
	}
	if r.ReceivableID != debtID {
		return 0, fmt.Errorf("%w: repayment belongs to another receivable", ErrValidation)
	}
	if r.Version != expected || r.Status != "posted" {
		return 0, fmt.Errorf("%w: stale or void repayment", ErrConflict)
	}
	return r.AmountMinor, nil
}
func (s *Service) AmendRepayment(ctx context.Context, in AmendRepaymentInput) (RepaymentResult, error) {
	return mutate(ctx, s, in.OperationKey, "amend_repayment", in, func(tx pgx.Tx) (RepaymentResult, error) {
		if e := lockReceivable(ctx, tx, in.ReceivableID, in.ExpectedReceivableVersion); e != nil {
			return RepaymentResult{}, e
		}
		old, e := lockRepayment(ctx, tx, in.ID, in.ReceivableID, in.ExpectedVersion)
		if e != nil {
			return RepaymentResult{}, e
		}
		return writeRepayment(ctx, tx, in.CreateRepaymentInput, in.ID, in.ExpectedVersion+1, old)
	})
}
func (s *Service) VoidRepayment(ctx context.Context, in VoidRepaymentInput) (RepaymentResult, error) {
	return mutate(ctx, s, in.OperationKey, "void_repayment", in, func(tx pgx.Tx) (RepaymentResult, error) {
		if e := lockReceivable(ctx, tx, in.ReceivableID, in.ExpectedReceivableVersion); e != nil {
			return RepaymentResult{}, e
		}
		if _, e := lockRepayment(ctx, tx, in.ID, in.ReceivableID, in.ExpectedVersion); e != nil {
			return RepaymentResult{}, e
		}
		if e := db.New(tx).VoidRepayment(ctx, in.ID); e != nil {
			return RepaymentResult{}, e
		}
		return finishRepayment(ctx, tx, in.ID, in.ReceivableID, "repayment_voided")
	})
}

func (s *Service) QueryReceivables(ctx context.Context, in ReceivableQuery) (ReceivablePage, error) {
	out := ReceivablePage{Receivables: []Receivable{}, OutstandingTotals: []CurrencyTotal{}}
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Status == "" {
		in.Status = "open"
	}
	if in.Limit < 1 || in.Limit > 100 || in.Offset < 0 || len(in.Debtor) > 200 {
		return out, fmt.Errorf("%w: invalid pagination or debtor", ErrValidation)
	}
	if in.Status != "open" && in.Status != "settled" && in.Status != "void" && in.Status != "all" {
		return out, fmt.Errorf("%w: invalid receivable status", ErrValidation)
	}
	if in.Currency != "" && in.Currency != "RON" && in.Currency != "EUR" && in.Currency != "USD" {
		return out, fmt.Errorf("%w: unsupported currency", ErrValidation)
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	q := db.New(tx)
	filter := db.FilteredReceivableCountParams{Currency: in.Currency, Debtor: in.Debtor, Status: in.Status}
	out.TotalCount, e = q.FilteredReceivableCount(ctx, filter)
	if e != nil {
		return out, e
	}
	ids, e := q.FilteredReceivableIDs(ctx, db.FilteredReceivableIDsParams{Currency: in.Currency, Debtor: in.Debtor, Status: in.Status, PageLimit: int64(in.Limit), PageOffset: int64(in.Offset)})
	if e != nil {
		return out, e
	}
	for _, key := range ids {
		r, e := readReceivable(ctx, tx, key)
		if e != nil {
			return out, e
		}
		out.Receivables = append(out.Receivables, r)
	}
	totals, e := q.FilteredReceivableTotals(ctx, db.FilteredReceivableTotalsParams(filter))
	if e != nil {
		return out, e
	}
	for _, r := range totals {
		amount, e := formatAggregate(r.Amount)
		if e != nil {
			return out, e
		}
		out.OutstandingTotals = append(out.OutstandingTotals, CurrencyTotal{Currency: r.Currency, Amount: amount})
	}
	return out, tx.Commit(ctx)
}
func (s *Service) QueryRepayments(ctx context.Context, in RepaymentQuery) (RepaymentPage, error) {
	out := RepaymentPage{Repayments: []Repayment{}}
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Status == "" {
		in.Status = "posted"
	}
	if in.Limit < 1 || in.Limit > 100 || in.Offset < 0 {
		return out, fmt.Errorf("%w: invalid pagination", ErrValidation)
	}
	if in.Status != "posted" && in.Status != "void" && in.Status != "all" {
		return out, fmt.Errorf("%w: invalid repayment status", ErrValidation)
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	q := db.New(tx)
	out.TotalCount, e = q.FilteredRepaymentCount(ctx, db.FilteredRepaymentCountParams{ReceivableID: in.ReceivableID, AccountID: in.AccountID, Status: in.Status})
	if e != nil {
		return out, e
	}
	ids, e := q.FilteredRepaymentIDs(ctx, db.FilteredRepaymentIDsParams{ReceivableID: in.ReceivableID, AccountID: in.AccountID, Status: in.Status, PageLimit: int64(in.Limit), PageOffset: int64(in.Offset)})
	if e != nil {
		return out, e
	}
	for _, key := range ids {
		r, e := readRepayment(ctx, tx, key)
		if e != nil {
			return out, e
		}
		out.Repayments = append(out.Repayments, r)
	}
	return out, tx.Commit(ctx)
}
