package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
)

func archivedAccountError() error {
	return fmt.Errorf("%w: archived account cannot receive new financial transactions or newly assigned references", ErrValidation)
}

// Read balances within the mutation transaction so audit and retry results describe
// the same serialized financial state as the account metadata change.
func readAccount(ctx context.Context, tx pgx.Tx, accountID string) (Account, error) {
	rows, err := db.New(tx).AccountBalances(ctx)
	if err != nil {
		return Account{}, err
	}
	for _, r := range rows {
		if r.ID != accountID {
			continue
		}
		balance, err := formatAggregate(r.Balance)
		return Account{ID: r.ID, Name: r.Name, Currency: r.Currency, OpeningAmount: formatMoney(r.OpeningMinor), OpeningDate: r.OpeningDate, Balance: balance, Version: r.Version, Archived: r.Archived, BalanceQuality: balanceQuality(r.EstimatedCount, r.UnresolvedCount), EstimatedExpenseCount: r.EstimatedCount, UnresolvedExpenseCount: r.UnresolvedCount}, err
	}
	return Account{}, fmt.Errorf("%w: account", ErrNotFound)
}

func (s *Service) updateAccount(ctx context.Context, key, action string, input any, accountID string, expected int64, name *string, archived *bool) (Account, error) {
	return mutate(ctx, s, key, action, input, func(tx pgx.Tx) (Account, error) {
		if accountID == "" || expected < 1 || (name != nil && !validName(*name)) {
			return Account{}, fmt.Errorf("%w: ID, positive expected version and valid name required", ErrValidation)
		}
		current, err := db.New(tx).LockAccount(ctx, accountID)
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, fmt.Errorf("%w: account", ErrNotFound)
		}
		if err != nil {
			return Account{}, err
		}
		if current.Version != expected {
			return Account{}, fmt.Errorf("%w: stale account version", ErrConflict)
		}
		a, err := readAccount(ctx, tx, accountID)
		if err != nil {
			return a, err
		}
		if name != nil {
			a.Name = *name
		}
		if archived != nil {
			a.Archived = *archived
		}
		err = db.New(tx).UpdateAccountLifecycle(ctx, db.UpdateAccountLifecycleParams{ID: a.ID, Name: a.Name, Archived: a.Archived})
		if err != nil {
			return a, err
		}
		a.Version++
		auditAction := "account_renamed"
		if archived != nil {
			auditAction = "account_unarchived"
			if *archived {
				auditAction = "account_archived"
			}
		}
		err = audit(ctx, tx, a.ID, auditAction, a)
		return a, err
	})
}
func (s *Service) RenameAccount(ctx context.Context, in RenameNamedInput) (Account, error) {
	return s.updateAccount(ctx, in.OperationKey, "rename_account", in, in.ID, in.ExpectedVersion, &in.Name, nil)
}
func (s *Service) SetAccountArchived(ctx context.Context, in SetAccountArchivedInput) (Account, error) {
	return s.updateAccount(ctx, in.OperationKey, "set_account_archived", in, in.ID, in.ExpectedVersion, nil, &in.Archived)
}
