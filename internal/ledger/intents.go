package ledger

import (
	"context"
	"errors"
	"fmt"
	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"unicode/utf8"
)

func readBuyingIntent(ctx context.Context, tx pgx.Tx, intentID string) (BuyingIntent, error) {
	q := db.New(tx)
	r, e := q.ReadBuyingIntent(ctx, intentID)
	if errors.Is(e, pgx.ErrNoRows) {
		return BuyingIntent{}, fmt.Errorf("%w: buying intent", ErrNotFound)
	}
	if e != nil {
		return BuyingIntent{}, e
	}
	out := BuyingIntent{ID: r.ID, Title: r.Title, Notes: r.Notes, Status: r.Status, Version: r.Version, CandidateID: r.CandidateID, OutcomeNote: r.OutcomeNote, Candidates: []IntentCandidate{}}
	candidates, e := q.ListIntentCandidates(ctx, intentID)
	if e != nil {
		return out, e
	}
	for _, c := range candidates {
		candidate := IntentCandidate{ID: c.ID, Reference: c.Reference, Notes: c.Notes, Currency: c.Currency}
		if c.AdvertisedMinor.Valid {
			candidate.AdvertisedAmount = formatMoney(c.AdvertisedMinor.Int64)
		}
		out.Candidates = append(out.Candidates, candidate)
	}
	if r.ExpenseID != "" {
		expense, e := readExpense(ctx, tx, r.ExpenseID)
		if e != nil {
			return out, e
		}
		out.Expense = &expense
	}
	return out, nil
}
func lockBuyingIntent(ctx context.Context, tx pgx.Tx, intentID string, expected int64) error {
	r, e := db.New(tx).LockBuyingIntent(ctx, intentID)
	if errors.Is(e, pgx.ErrNoRows) {
		return fmt.Errorf("%w: buying intent", ErrNotFound)
	}
	if e != nil {
		return e
	}
	if r.Version != expected || r.Status != "open" {
		return fmt.Errorf("%w: stale version or closed buying intent", ErrConflict)
	}
	return nil
}
func (s *Service) CreateBuyingIntent(ctx context.Context, in CreateBuyingIntentInput) (BuyingIntent, error) {
	return mutate(ctx, s, in.OperationKey, "create_buying_intent", in, func(tx pgx.Tx) (BuyingIntent, error) {
		if !validName(in.Title) || len(in.Notes) > 4000 {
			return BuyingIntent{}, fmt.Errorf("%w: title required and notes maximum 4000 characters", ErrValidation)
		}
		intentID := id()
		e := db.New(tx).InsertBuyingIntent(ctx, db.InsertBuyingIntentParams{ID: intentID, Title: in.Title, Notes: in.Notes})
		if e != nil {
			return BuyingIntent{}, e
		}
		out, e := readBuyingIntent(ctx, tx, intentID)
		if e == nil {
			e = audit(ctx, tx, intentID, "buying_intent_created", out)
		}
		return out, e
	})
}
func (s *Service) UpdateBuyingIntent(ctx context.Context, in UpdateBuyingIntentInput) (BuyingIntent, error) {
	return mutate(ctx, s, in.OperationKey, "update_buying_intent", in, func(tx pgx.Tx) (BuyingIntent, error) {
		if !validName(in.Title) || len(in.Notes) > 4000 {
			return BuyingIntent{}, fmt.Errorf("%w: title required and notes maximum 4000 characters", ErrValidation)
		}
		if e := lockBuyingIntent(ctx, tx, in.IntentID, in.ExpectedVersion); e != nil {
			return BuyingIntent{}, e
		}
		if e := db.New(tx).UpdateBuyingIntent(ctx, db.UpdateBuyingIntentParams{ID: in.IntentID, Title: in.Title, Notes: in.Notes}); e != nil {
			return BuyingIntent{}, e
		}
		out, e := readBuyingIntent(ctx, tx, in.IntentID)
		if e == nil {
			e = audit(ctx, tx, in.IntentID, "buying_intent_updated", out)
		}
		return out, e
	})
}
func (s *Service) AddIntentCandidate(ctx context.Context, in AddIntentCandidateInput) (BuyingIntent, error) {
	return mutate(ctx, s, in.OperationKey, "add_intent_candidate", in, func(tx pgx.Tx) (BuyingIntent, error) {
		if strings.TrimSpace(in.Reference) == "" || len(in.Reference) > 2000 || len(in.Notes) > 4000 {
			return BuyingIntent{}, fmt.Errorf("%w: reference required (maximum 2000), notes maximum 4000", ErrValidation)
		}
		if (in.AdvertisedAmount == "") != (in.Currency == "") {
			return BuyingIntent{}, fmt.Errorf("%w: advertised amount and currency must be supplied together", ErrValidation)
		}
		minor := pgtype.Int8{}
		currency := pgtype.Text{}
		if in.AdvertisedAmount != "" {
			n, e := parseMoney(in.AdvertisedAmount)
			if e != nil {
				return BuyingIntent{}, e
			}
			if n <= 0 || (in.Currency != "RON" && in.Currency != "EUR" && in.Currency != "USD") {
				return BuyingIntent{}, fmt.Errorf("%w: positive advertised amount and supported currency required", ErrValidation)
			}
			minor = pgtype.Int8{Int64: n, Valid: true}
			currency = pgtype.Text{String: in.Currency, Valid: true}
		}
		if e := lockBuyingIntent(ctx, tx, in.IntentID, in.ExpectedVersion); e != nil {
			return BuyingIntent{}, e
		}
		q := db.New(tx)
		// Bound the nested result and stored audit snapshot; references are never fetched.
		candidates, e := q.ListIntentCandidates(ctx, in.IntentID)
		if e != nil {
			return BuyingIntent{}, e
		}
		if len(candidates) >= 100 {
			return BuyingIntent{}, fmt.Errorf("%w: maximum 100 candidates per intent", ErrValidation)
		}
		e = q.InsertIntentCandidate(ctx, db.InsertIntentCandidateParams{ID: id(), IntentID: in.IntentID, Reference: in.Reference, Notes: in.Notes, AdvertisedMinor: minor, Currency: currency})
		if e != nil {
			return BuyingIntent{}, e
		}
		if e = q.BumpBuyingIntent(ctx, in.IntentID); e != nil {
			return BuyingIntent{}, e
		}
		out, e := readBuyingIntent(ctx, tx, in.IntentID)
		if e == nil {
			e = audit(ctx, tx, in.IntentID, "intent_candidate_added", out)
		}
		return out, e
	})
}
func (s *Service) CancelBuyingIntent(ctx context.Context, in CancelBuyingIntentInput) (BuyingIntent, error) {
	return mutate(ctx, s, in.OperationKey, "cancel_buying_intent", in, func(tx pgx.Tx) (BuyingIntent, error) {
		if len(in.OutcomeNote) > 4000 {
			return BuyingIntent{}, fmt.Errorf("%w: outcome note maximum 4000", ErrValidation)
		}
		if e := lockBuyingIntent(ctx, tx, in.IntentID, in.ExpectedVersion); e != nil {
			return BuyingIntent{}, e
		}
		if e := db.New(tx).CancelBuyingIntent(ctx, db.CancelBuyingIntentParams{ID: in.IntentID, OutcomeNote: in.OutcomeNote}); e != nil {
			return BuyingIntent{}, e
		}
		out, e := readBuyingIntent(ctx, tx, in.IntentID)
		if e == nil {
			e = audit(ctx, tx, in.IntentID, "buying_intent_cancelled", out)
		}
		return out, e
	})
}
func (s *Service) ConfirmIntentPurchase(ctx context.Context, in ConfirmIntentPurchaseInput) (BuyingIntent, error) {
	return mutate(ctx, s, in.OperationKey, "confirm_intent_purchase", in, func(tx pgx.Tx) (BuyingIntent, error) {
		if len(in.OutcomeNote) > 4000 || (in.NewExpense == nil) == (in.ExpenseID == "") || (in.NewExpense != nil && in.ExpectedExpenseVersion != 0) || (in.ExpenseID != "" && in.ExpectedExpenseVersion < 1) {
			return BuyingIntent{}, fmt.Errorf("%w: supply exactly one new expense or existing expense with its version; outcome note maximum 4000", ErrValidation)
		}
		if e := lockBuyingIntent(ctx, tx, in.IntentID, in.ExpectedVersion); e != nil {
			return BuyingIntent{}, e
		}
		q := db.New(tx)
		if in.CandidateID != "" {
			candidates, e := q.ListIntentCandidates(ctx, in.IntentID)
			if e != nil {
				return BuyingIntent{}, e
			}
			found := false
			for _, c := range candidates {
				if c.ID == in.CandidateID {
					found = true
				}
			}
			if !found {
				return BuyingIntent{}, fmt.Errorf("%w: candidate does not belong to intent", ErrValidation)
			}
		}
		expenseID := in.ExpenseID
		if in.NewExpense != nil {
			draft := in.NewExpense
			expense, e := writeExpense(ctx, tx, CreateExpenseInput{Payment: draft.Payment, AccountID: draft.AccountID, Currency: draft.Currency, Amount: draft.Amount, OccurredDate: draft.OccurredDate, Description: draft.Description, Allocations: draft.Allocations}, id(), 1)
			if e != nil {
				return BuyingIntent{}, e
			}
			expenseID = expense.ID
			if e = audit(ctx, tx, expenseID, "expense_created", expense); e != nil {
				return BuyingIntent{}, e
			}
		} else {
			if e := lockExpense(ctx, tx, expenseID, in.ExpectedExpenseVersion); e != nil {
				return BuyingIntent{}, e
			}
		}
		if e := q.PurchaseBuyingIntent(ctx, db.PurchaseBuyingIntentParams{ID: in.IntentID, OutcomeNote: in.OutcomeNote, Column3: in.CandidateID, ExpenseID: pgtype.Text{String: expenseID, Valid: true}}); e != nil {
			return BuyingIntent{}, e
		}
		out, e := readBuyingIntent(ctx, tx, in.IntentID)
		if e == nil {
			e = audit(ctx, tx, in.IntentID, "intent_purchase_confirmed", out)
		}
		return out, e
	})
}
func (s *Service) QueryBuyingIntents(ctx context.Context, in BuyingIntentQuery) (BuyingIntentPage, error) {
	out := BuyingIntentPage{Intents: []BuyingIntent{}}
	in.Search = strings.TrimSpace(in.Search)
	if utf8.RuneCountInString(in.Search) > 200 {
		return out, fmt.Errorf("%w: search maximum 200 characters", ErrValidation)
	}
	if in.Status == "" {
		in.Status = "open"
	}
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Limit < 1 || in.Limit > 100 || in.Offset < 0 || (in.Status != "open" && in.Status != "purchased" && in.Status != "cancelled" && in.Status != "all") {
		return out, fmt.Errorf("%w: invalid status or pagination", ErrValidation)
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	q := db.New(tx)
	out.TotalCount, e = q.CountBuyingIntents(ctx, db.CountBuyingIntentsParams{Status: in.Status, Search: in.Search})
	if e != nil {
		return out, e
	}
	ids, e := q.ListBuyingIntentIDs(ctx, db.ListBuyingIntentIDsParams{Status: in.Status, Search: in.Search, PageLimit: int64(in.Limit), PageOffset: int64(in.Offset)})
	if e != nil {
		return out, e
	}
	for _, intentID := range ids {
		intent, e := readBuyingIntent(ctx, tx, intentID)
		if e != nil {
			return out, e
		}
		out.Intents = append(out.Intents, intent)
	}
	return out, tx.Commit(ctx)
}
