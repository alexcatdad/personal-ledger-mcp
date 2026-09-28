package ledger

import (
	"context"
	"errors"
	"fmt"
	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
	"math/big"
	"strings"
)

func reconciliationNotFound(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return fmt.Errorf("%w: reconciliation record", ErrNotFound)
	}
	return e
}
func readBalanceObservation(ctx context.Context, tx pgx.Tx, key string) (BalanceObservation, error) {
	r, e := db.New(tx).ReadBalanceObservation(ctx, key)
	o := BalanceObservation{ID: r.ID, AccountID: r.AccountID, Currency: r.Currency, AsOfDate: r.AsOfDate, Basis: r.Basis, Notes: r.Notes, Status: r.Status, Version: r.Version}
	actual, recorded := r.ActualMinor, r.RecordedLedgerMinor
	if e != nil {
		return o, reconciliationNotFound(e)
	}
	o.ActualBalance = formatMoney(actual)
	o.RecordedLedgerBalance, e = formatAggregate(recorded)
	if e != nil {
		return o, e
	}
	current, e := db.New(tx).AccountBalanceAsOf(ctx, db.AccountBalanceAsOfParams{AccountID: o.AccountID, AsOfDate: o.AsOfDate})
	if e != nil {
		return o, e
	}
	quality, e := db.New(tx).ExpenseBalanceQualityAsOf(ctx, db.ExpenseBalanceQualityAsOfParams{AccountID: o.AccountID, AsOfDate: o.AsOfDate})
	if e != nil {
		return o, e
	}
	o.EstimatedExpenseCount = quality.EstimatedCount
	o.UnresolvedExpenseCount = quality.UnresolvedCount
	o.BalanceQuality = balanceQuality(quality.EstimatedCount, quality.UnresolvedCount)
	o.CurrentLedgerBalance, e = formatAggregate(current)
	if e != nil {
		return o, e
	}
	n, ok := new(big.Int).SetString(current, 10)
	if !ok {
		return o, fmt.Errorf("invalid ledger aggregate")
	}
	difference := new(big.Int).Sub(big.NewInt(actual), n)
	o.Difference, e = formatAggregate(difference.String())
	if e != nil {
		return o, e
	}
	if o.Status != "void" {
		o.Status = "unmatched"
		if difference.Sign() == 0 && o.BalanceQuality == "exact" {
			o.Status = "matched"
		}
	}
	return o, nil
}
func lockBalanceObservation(ctx context.Context, tx pgx.Tx, key string, expected int64) error {
	r, e := db.New(tx).LockBalanceObservation(ctx, key)
	if e != nil {
		return reconciliationNotFound(e)
	}
	if r.Version != expected || r.Status == "void" {
		return fmt.Errorf("%w: stale or void observation", ErrConflict)
	}
	return nil
}
func noPostedAdjustment(ctx context.Context, tx pgx.Tx, key string) error {
	exists, e := db.New(tx).HasPostedBalanceAdjustment(ctx, key)
	if e == nil && exists {
		return fmt.Errorf("%w: void the posted adjustment first", ErrConflict)
	}
	return e
}
func writeBalanceObservation(ctx context.Context, tx pgx.Tx, in RecordBalanceObservationInput, key string, version int64) (BalanceObservation, error) {
	var out BalanceObservation
	actual, e := parseMoney(in.ActualBalance)
	if e != nil {
		return out, e
	}
	if e = date(in.AsOfDate); e != nil {
		return out, e
	}
	if in.Basis != "posted_end_of_day" || len(in.Notes) > 4000 {
		return out, fmt.Errorf("%w: posted_end_of_day basis and notes up to 4000 characters required", ErrValidation)
	}
	a, e := db.New(tx).LockAccount(ctx, in.AccountID)
	if e != nil {
		return out, reconciliationNotFound(e)
	}
	if a.Currency != in.Currency || in.AsOfDate < a.OpeningDate {
		return out, fmt.Errorf("%w: account currency and opening date must match observation", ErrValidation)
	}
	current, e := db.New(tx).AccountBalanceAsOf(ctx, db.AccountBalanceAsOfParams{AccountID: in.AccountID, AsOfDate: in.AsOfDate})
	if e != nil {
		return out, e
	}
	e = db.New(tx).UpsertBalanceObservation(ctx, db.UpsertBalanceObservationParams{ID: key, AccountID: in.AccountID, AsOfDate: in.AsOfDate, ActualMinor: actual, RecordedLedgerMinor: current, Basis: in.Basis, Notes: in.Notes, Version: version})
	if e != nil {
		return out, e
	}
	return readBalanceObservation(ctx, tx, key)
}
func (s *Service) RecordBalanceObservation(ctx context.Context, in RecordBalanceObservationInput) (BalanceObservation, error) {
	return mutate(ctx, s, in.OperationKey, "record_balance_observation", in, func(tx pgx.Tx) (BalanceObservation, error) {
		o, e := writeBalanceObservation(ctx, tx, in, id(), 1)
		if e == nil {
			e = audit(ctx, tx, o.ID, "balance_observation_recorded", o)
		}
		return o, e
	})
}
func (s *Service) AmendBalanceObservation(ctx context.Context, in AmendBalanceObservationInput) (BalanceObservation, error) {
	return mutate(ctx, s, in.OperationKey, "amend_balance_observation", in, func(tx pgx.Tx) (BalanceObservation, error) {
		if e := lockBalanceObservation(ctx, tx, in.ID, in.ExpectedVersion); e != nil {
			return BalanceObservation{}, e
		}
		if e := noPostedAdjustment(ctx, tx, in.ID); e != nil {
			return BalanceObservation{}, e
		}
		o, e := writeBalanceObservation(ctx, tx, in.RecordBalanceObservationInput, in.ID, in.ExpectedVersion+1)
		if e == nil {
			e = audit(ctx, tx, o.ID, "balance_observation_amended", o)
		}
		return o, e
	})
}
func (s *Service) VoidBalanceObservation(ctx context.Context, in VoidBalanceObservationInput) (BalanceObservation, error) {
	return mutate(ctx, s, in.OperationKey, "void_balance_observation", in, func(tx pgx.Tx) (BalanceObservation, error) {
		if e := lockBalanceObservation(ctx, tx, in.ID, in.ExpectedVersion); e != nil {
			return BalanceObservation{}, e
		}
		if e := noPostedAdjustment(ctx, tx, in.ID); e != nil {
			return BalanceObservation{}, e
		}
		if e := db.New(tx).VoidBalanceObservation(ctx, in.ID); e != nil {
			return BalanceObservation{}, e
		}
		o, e := readBalanceObservation(ctx, tx, in.ID)
		if e == nil {
			e = audit(ctx, tx, o.ID, "balance_observation_voided", o)
		}
		return o, e
	})
}
func readBalanceAdjustment(ctx context.Context, tx pgx.Tx, key string) (BalanceAdjustment, error) {
	r, e := db.New(tx).ReadBalanceAdjustment(ctx, key)
	return BalanceAdjustment{ID: r.ID, ObservationID: r.ObservationID, AccountID: r.AccountID, Currency: r.Currency, Amount: formatMoney(r.AmountMinor), OccurredDate: r.OccurredDate, Reason: r.Reason, Status: r.Status, Version: r.Version}, reconciliationNotFound(e)
}
func finishBalanceAdjustment(ctx context.Context, tx pgx.Tx, a BalanceAdjustment, action string) (BalanceAdjustmentResult, error) {
	out := BalanceAdjustmentResult{Adjustment: a}
	e := db.New(tx).BumpBalanceObservation(ctx, a.ObservationID)
	if e != nil {
		return out, e
	}
	out.Observation, e = readBalanceObservation(ctx, tx, a.ObservationID)
	if e != nil {
		return out, e
	}
	if e = audit(ctx, tx, a.ID, action, a); e != nil {
		return out, e
	}
	e = audit(ctx, tx, a.ObservationID, action, out)
	return out, e
}
func (s *Service) CreateBalanceAdjustment(ctx context.Context, in CreateBalanceAdjustmentInput) (BalanceAdjustmentResult, error) {
	return mutate(ctx, s, in.OperationKey, "create_balance_adjustment", in, func(tx pgx.Tx) (BalanceAdjustmentResult, error) {
		var out BalanceAdjustmentResult
		amount, e := parseMoney(in.Amount)
		if e != nil {
			return out, e
		}
		if amount == 0 || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 4000 {
			return out, fmt.Errorf("%w: nonzero adjustment and reason up to 4000 characters required", ErrValidation)
		}
		if e = lockBalanceObservation(ctx, tx, in.ObservationID, in.ExpectedObservationVersion); e != nil {
			return out, e
		}
		if e = noPostedAdjustment(ctx, tx, in.ObservationID); e != nil {
			return out, e
		}
		o, e := readBalanceObservation(ctx, tx, in.ObservationID)
		if e != nil {
			return out, e
		}
		account, e := db.New(tx).LockAccount(ctx, o.AccountID)
		if e != nil {
			return out, e
		}
		if account.Archived {
			return out, archivedAccountError()
		}
		if o.BalanceQuality != "exact" {
			return out, fmt.Errorf("%w: resolve %d estimated and %d unresolved expense payments before adjustment", ErrConflict, o.EstimatedExpenseCount, o.UnresolvedExpenseCount)
		}
		if o.CurrentLedgerBalance != in.ExpectedLedgerBalance {
			return out, fmt.Errorf("%w: ledger balance changed; query observation again", ErrConflict)
		}
		if formatMoney(amount) != o.Difference {
			return out, fmt.Errorf("%w: adjustment must equal full current difference", ErrValidation)
		}
		key := id()
		e = db.New(tx).InsertBalanceAdjustment(ctx, db.InsertBalanceAdjustmentParams{ID: key, ObservationID: o.ID, AccountID: o.AccountID, AmountMinor: amount, OccurredDate: o.AsOfDate, Reason: in.Reason})
		if e != nil {
			return out, e
		}
		a, e := readBalanceAdjustment(ctx, tx, key)
		if e != nil {
			return out, e
		}
		return finishBalanceAdjustment(ctx, tx, a, "balance_adjustment_created")
	})
}
func voidBalanceAdjustment(ctx context.Context, tx pgx.Tx, key string, expected int64) (BalanceAdjustmentResult, error) {
	var out BalanceAdjustmentResult
	a, e := readBalanceAdjustment(ctx, tx, key)
	if e != nil {
		return out, e
	}
	if _, e = db.New(tx).LockBalanceObservation(ctx, a.ObservationID); e != nil {
		return out, e
	}
	locked, e := db.New(tx).LockBalanceAdjustment(ctx, key)
	if e != nil {
		return out, e
	}
	if locked.Version != expected || locked.Status != "posted" {
		return out, fmt.Errorf("%w: stale or void adjustment", ErrConflict)
	}
	if e = db.New(tx).VoidBalanceAdjustment(ctx, key); e != nil {
		return out, e
	}
	a, e = readBalanceAdjustment(ctx, tx, key)
	if e != nil {
		return out, e
	}
	return finishBalanceAdjustment(ctx, tx, a, "balance_adjustment_voided")
}
func (s *Service) VoidBalanceAdjustment(ctx context.Context, in VoidBalanceAdjustmentInput) (BalanceAdjustmentResult, error) {
	return mutate(ctx, s, in.OperationKey, "void_balance_adjustment", in, func(tx pgx.Tx) (BalanceAdjustmentResult, error) {
		return voidBalanceAdjustment(ctx, tx, in.ID, in.ExpectedVersion)
	})
}
func reconciliationPage(limit, offset int) (int, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return 0, fmt.Errorf("%w: limit 1..100 and nonnegative offset required", ErrValidation)
	}
	return limit, nil
}
func (s *Service) QueryBalanceObservations(ctx context.Context, in BalanceObservationQuery) (BalanceObservationPage, error) {
	out := BalanceObservationPage{Observations: []BalanceObservation{}}
	limit, e := reconciliationPage(in.Limit, in.Offset)
	if e != nil {
		return out, e
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	out.TotalCount, e = db.New(tx).FilteredBalanceObservationCount(ctx, in.AccountID)
	if e != nil {
		return out, e
	}
	ids, e := db.New(tx).FilteredBalanceObservationIDs(ctx, db.FilteredBalanceObservationIDsParams{AccountID: in.AccountID, PageLimit: int64(limit), PageOffset: int64(in.Offset)})
	if e != nil {
		return out, e
	}
	for _, key := range ids {
		o, e := readBalanceObservation(ctx, tx, key)
		if e != nil {
			return out, e
		}
		out.Observations = append(out.Observations, o)
	}
	return out, tx.Commit(ctx)
}
func (s *Service) QueryBalanceAdjustments(ctx context.Context, in BalanceAdjustmentQuery) (BalanceAdjustmentPage, error) {
	out := BalanceAdjustmentPage{Adjustments: []BalanceAdjustment{}}
	limit, e := reconciliationPage(in.Limit, in.Offset)
	if e != nil {
		return out, e
	}
	if in.Status == "" {
		in.Status = "posted"
	}
	if in.Status != "posted" && in.Status != "all" && in.Status != "void" {
		return out, fmt.Errorf("%w: adjustment status must be posted, void or all", ErrValidation)
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	out.TotalCount, e = db.New(tx).FilteredBalanceAdjustmentCount(ctx, db.FilteredBalanceAdjustmentCountParams{AccountID: in.AccountID, ObservationID: in.ObservationID, Status: in.Status})
	if e != nil {
		return out, e
	}
	ids, e := db.New(tx).FilteredBalanceAdjustmentIDs(ctx, db.FilteredBalanceAdjustmentIDsParams{AccountID: in.AccountID, ObservationID: in.ObservationID, Status: in.Status, PageLimit: int64(limit), PageOffset: int64(in.Offset)})
	if e != nil {
		return out, e
	}
	for _, key := range ids {
		a, e := readBalanceAdjustment(ctx, tx, key)
		if e != nil {
			return out, e
		}
		out.Adjustments = append(out.Adjustments, a)
	}
	return out, tx.Commit(ctx)
}
