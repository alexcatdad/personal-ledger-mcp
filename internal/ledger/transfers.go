package ledger

import (
	"context"
	"errors"
	"fmt"
	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
	"sort"
)

func writeTransfer(ctx context.Context, tx pgx.Tx, in CreateTransferInput, transferID string, version int64) (Transfer, error) {
	out := Transfer{ID: transferID, SourceAccountID: in.SourceAccountID, DestinationAccountID: in.DestinationAccountID, SourceCurrency: in.SourceCurrency, DestinationCurrency: in.DestinationCurrency, OccurredDate: in.OccurredDate, Description: in.Description, Status: "posted", Version: version}
	source, err := parseMoney(in.SourceAmount)
	if err != nil {
		return out, err
	}
	destination, err := parseMoney(in.DestinationAmount)
	if err != nil {
		return out, err
	}
	if source <= 0 || destination <= 0 || in.SourceAccountID == in.DestinationAccountID || len(in.Description) > 2000 {
		return out, fmt.Errorf("%w: different accounts, positive amounts and description up to 2000 characters required", ErrValidation)
	}
	if err = date(in.OccurredDate); err != nil {
		return out, err
	}
	ids := []string{in.SourceAccountID, in.DestinationAccountID}
	sort.Strings(ids)
	accounts := map[string]db.LockAccountRow{}
	for _, accountID := range ids {
		account, e := db.New(tx).LockAccount(ctx, accountID)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, fmt.Errorf("%w: account", ErrNotFound)
		}
		if e != nil {
			return out, e
		}
		accounts[accountID] = account
	}
	if accounts[in.SourceAccountID].Archived || accounts[in.DestinationAccountID].Archived {
		old, e := db.New(tx).ReadTransfer(ctx, transferID)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
		if e != nil || (accounts[in.SourceAccountID].Archived && old.SourceAccountID != in.SourceAccountID) || (accounts[in.DestinationAccountID].Archived && old.DestinationAccountID != in.DestinationAccountID) {
			return out, archivedAccountError()
		}
	}
	if in.SourceCurrency != accounts[in.SourceAccountID].Currency || in.DestinationCurrency != accounts[in.DestinationAccountID].Currency {
		return out, fmt.Errorf("%w: currencies must match their accounts", ErrValidation)
	}
	if in.OccurredDate < accounts[in.SourceAccountID].OpeningDate || in.OccurredDate < accounts[in.DestinationAccountID].OpeningDate {
		return out, fmt.Errorf("%w: transfer precedes opening balance", ErrValidation)
	}
	if in.SourceCurrency == in.DestinationCurrency && source != destination {
		return out, fmt.Errorf("%w: same currency transfer amounts must match; record fees as expenses", ErrValidation)
	}
	out.SourceAmount = formatMoney(source)
	out.DestinationAmount = formatMoney(destination)
	err = db.New(tx).UpsertTransfer(ctx, db.UpsertTransferParams{ID: out.ID, SourceAccountID: out.SourceAccountID, DestinationAccountID: out.DestinationAccountID, SourceMinor: source, DestinationMinor: destination, Column6: out.OccurredDate, Description: out.Description, Version: version})
	return out, err
}
func readTransfer(ctx context.Context, tx pgx.Tx, id string) (Transfer, error) {
	r, e := db.New(tx).ReadTransfer(ctx, id)
	if e != nil {
		return Transfer{}, e
	}
	return Transfer{ID: r.ID, SourceAccountID: r.SourceAccountID, DestinationAccountID: r.DestinationAccountID, SourceCurrency: r.SourceCurrency, DestinationCurrency: r.DestinationCurrency, SourceAmount: formatMoney(r.SourceMinor), DestinationAmount: formatMoney(r.DestinationMinor), OccurredDate: r.OccurredDate, Description: r.Description, Status: r.Status, Version: r.Version}, nil
}
func (s *Service) CreateTransfer(ctx context.Context, in CreateTransferInput) (Transfer, error) {
	return mutate(ctx, s, in.OperationKey, "create_transfer", in, func(tx pgx.Tx) (Transfer, error) {
		out, e := writeTransfer(ctx, tx, in, id(), 1)
		if e == nil {
			e = audit(ctx, tx, out.ID, "transfer_created", out)
		}
		return out, e
	})
}
func lockTransfer(ctx context.Context, tx pgx.Tx, id string, expected int64) error {
	r, e := db.New(tx).LockTransfer(ctx, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return fmt.Errorf("%w: transfer", ErrNotFound)
	}
	if e != nil {
		return e
	}
	if r.Version != expected || r.Status != "posted" {
		return fmt.Errorf("%w: stale version or void transfer", ErrConflict)
	}
	return nil
}
func (s *Service) AmendTransfer(ctx context.Context, in AmendTransferInput) (Transfer, error) {
	return mutate(ctx, s, in.OperationKey, "amend_transfer", in, func(tx pgx.Tx) (Transfer, error) {
		if e := lockTransfer(ctx, tx, in.ID, in.ExpectedVersion); e != nil {
			return Transfer{}, e
		}
		out, e := writeTransfer(ctx, tx, in.CreateTransferInput, in.ID, in.ExpectedVersion+1)
		if e == nil {
			e = audit(ctx, tx, out.ID, "transfer_amended", out)
		}
		return out, e
	})
}
func (s *Service) VoidTransfer(ctx context.Context, in VoidTransferInput) (Transfer, error) {
	return mutate(ctx, s, in.OperationKey, "void_transfer", in, func(tx pgx.Tx) (Transfer, error) {
		if e := lockTransfer(ctx, tx, in.ID, in.ExpectedVersion); e != nil {
			return Transfer{}, e
		}
		if e := db.New(tx).VoidTransfer(ctx, in.ID); e != nil {
			return Transfer{}, e
		}
		out, e := readTransfer(ctx, tx, in.ID)
		if e == nil {
			e = audit(ctx, tx, out.ID, "transfer_voided", out)
		}
		return out, e
	})
}
func (s *Service) QueryTransfers(ctx context.Context, in TransferQuery) (TransferPage, error) {
	out := TransferPage{Transfers: []Transfer{}}
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
	for _, v := range []string{in.FromDate, in.ToDate} {
		if v != "" {
			if e := date(v); e != nil {
				return out, e
			}
		}
	}
	if in.FromDate != "" && in.ToDate != "" && in.FromDate > in.ToDate {
		return out, fmt.Errorf("%w: from_date exceeds to_date", ErrValidation)
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	q := db.New(tx)
	out.TotalCount, e = q.FilteredTransferCount(ctx, db.FilteredTransferCountParams{AccountID: in.AccountID, FromDate: in.FromDate, ToDate: in.ToDate, Status: in.Status})
	if e != nil {
		return out, e
	}
	ids, e := q.FilteredTransferIDs(ctx, db.FilteredTransferIDsParams{AccountID: in.AccountID, FromDate: in.FromDate, ToDate: in.ToDate, Status: in.Status, PageLimit: int64(in.Limit), PageOffset: int64(in.Offset)})
	if e != nil {
		return out, e
	}
	for _, id := range ids {
		r, e := readTransfer(ctx, tx, id)
		if e != nil {
			return out, e
		}
		out.Transfers = append(out.Transfers, r)
	}
	return out, tx.Commit(ctx)
}
