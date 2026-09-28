package ledger

import (
	"context"
	"fmt"
	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
)

type IncomeDraft struct {
	AccountID    string `json:"account_id"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	OccurredDate string `json:"date"`
	Description  string `json:"description"`
}
type TransferDraft struct {
	SourceAccountID      string `json:"source_account_id"`
	DestinationAccountID string `json:"destination_account_id"`
	SourceCurrency       string `json:"source_currency"`
	DestinationCurrency  string `json:"destination_currency"`
	SourceAmount         string `json:"source_amount"`
	DestinationAmount    string `json:"destination_amount"`
	OccurredDate         string `json:"date"`
	Description          string `json:"description"`
}
type RepaymentDraft struct {
	AccountID    string `json:"account_id"`
	Amount       string `json:"amount"`
	OccurredDate string `json:"date"`
	Description  string `json:"description"`
}
type BulkTransactionsInput struct {
	OperationKey string                `json:"operation_key"`
	Items        []BulkTransactionItem `json:"items"`
}
type BulkTransactionItem struct {
	Repayment                 *RepaymentDraft `json:"repayment,omitempty"`
	ReceivableID              string          `json:"receivable_id,omitempty"`
	ExpectedReceivableVersion int64           `json:"expected_receivable_version,omitempty"`
	Kind                      string          `json:"kind"`
	Action                    string          `json:"action"`
	Expense                   *ExpenseDraft   `json:"expense,omitempty"`
	Income                    *IncomeDraft    `json:"income,omitempty"`
	Transfer                  *TransferDraft  `json:"transfer,omitempty"`
	ID                        string          `json:"id,omitempty"`
	ExpectedVersion           int64           `json:"expected_version,omitempty"`
}
type BulkTransactionsResult struct {
	Results []BulkTransactionResult `json:"results"`
}
type BulkTransactionResult struct {
	Repayment   *Repayment          `json:"repayment,omitempty"`
	Receivable  *Receivable         `json:"receivable,omitempty"`
	Adjustment  *BalanceAdjustment  `json:"adjustment,omitempty"`
	Observation *BalanceObservation `json:"observation,omitempty"`
	Kind        string              `json:"kind"`
	Expense     *Expense            `json:"expense,omitempty"`
	Income      *Income             `json:"income,omitempty"`
	Transfer    *Transfer           `json:"transfer,omitempty"`
}

func (s *Service) BulkTransactions(ctx context.Context, in BulkTransactionsInput) (BulkTransactionsResult, error) {
	return mutate(ctx, s, in.OperationKey, "bulk_transactions", in, func(tx pgx.Tx) (BulkTransactionsResult, error) {
		results, err := runBulk(ctx, tx, in.Items, func(item BulkTransactionItem) string {
			if item.ID == "" {
				return ""
			}
			return item.Kind + ":" + item.ID
		}, validateBulkTransactionItem, applyBulkTransactionItem)
		if err != nil {
			return BulkTransactionsResult{}, err
		}
		return BulkTransactionsResult{Results: results}, nil
	})
}
func validateBulkTransactionItem(item BulkTransactionItem) error {
	if item.Kind == "repayment" {
		if item.ReceivableID == "" || item.ExpectedReceivableVersion <= 0 {
			return fmt.Errorf("%w: repayment requires receivable_id and expected_receivable_version", ErrValidation)
		}
	} else if item.ReceivableID != "" || item.ExpectedReceivableVersion != 0 {
		return fmt.Errorf("%w: receivable fields require repayment kind", ErrValidation)
	}
	count := 0
	if item.Repayment != nil {
		count++
	}
	if item.Expense != nil {
		count++
	}
	if item.Income != nil {
		count++
	}
	if item.Transfer != nil {
		count++
	}
	matches := false
	switch item.Kind {
	case "adjustment":
		if item.Action == "void" && count == 0 && item.ID != "" && item.ExpectedVersion > 0 {
			return nil
		}
		return fmt.Errorf("%w: adjustments support only void in a mixed batch", ErrValidation)
	case "repayment":
		matches = item.Repayment != nil
	case "expense":
		matches = item.Expense != nil
	case "income":
		matches = item.Income != nil
	case "transfer":
		matches = item.Transfer != nil
	default:
		return fmt.Errorf("%w: kind must be expense, income, transfer, repayment or adjustment", ErrValidation)
	}
	switch item.Action {
	case "create":
		if count == 1 && matches && item.ID == "" && item.ExpectedVersion == 0 {
			return nil
		}
	case "amend":
		if count == 1 && matches && item.ID != "" && item.ExpectedVersion > 0 {
			return nil
		}
	case "void":
		if count == 0 && item.ID != "" && item.ExpectedVersion > 0 {
			return nil
		}
	default:
		return fmt.Errorf("%w: action must be create, amend or void", ErrValidation)
	}
	return fmt.Errorf("%w: fields do not match bulk kind and action", ErrValidation)
}
func applyBulkTransactionItem(ctx context.Context, tx pgx.Tx, item BulkTransactionItem) (BulkTransactionResult, error) {
	out := BulkTransactionResult{Kind: item.Kind}
	if item.Kind == "repayment" {
		if err := lockReceivable(ctx, tx, item.ReceivableID, item.ExpectedReceivableVersion); err != nil {
			return out, err
		}
		key, version, old := id(), int64(1), int64(0)
		if item.Action != "create" {
			var err error
			old, err = lockRepayment(ctx, tx, item.ID, item.ReceivableID, item.ExpectedVersion)
			if err != nil {
				return out, err
			}
			key, version = item.ID, item.ExpectedVersion+1
		}
		var result RepaymentResult
		var err error
		if item.Action == "void" {
			err = db.New(tx).VoidRepayment(ctx, key)
			if err == nil {
				result, err = finishRepayment(ctx, tx, key, item.ReceivableID, "repayment_voided")
			}
		} else {
			d := item.Repayment
			result, err = writeRepayment(ctx, tx, CreateRepaymentInput{ReceivableID: item.ReceivableID, ExpectedReceivableVersion: item.ExpectedReceivableVersion, AccountID: d.AccountID, Amount: d.Amount, OccurredDate: d.OccurredDate, Description: d.Description}, key, version, old)
		}
		if err == nil {
			out.Repayment = &result.Repayment
			out.Receivable = &result.Receivable
		}
		return out, err
	}
	if item.Kind == "adjustment" {
		result, err := voidBalanceAdjustment(ctx, tx, item.ID, item.ExpectedVersion)
		if err != nil {
			return out, err
		}
		out.Adjustment = &result.Adjustment
		out.Observation = &result.Observation
		return out, nil
	}
	if item.Kind == "expense" {
		value, err := applyBulkItem(ctx, tx, BulkExpenseItem{Action: item.Action, Expense: item.Expense, ID: item.ID, ExpectedVersion: item.ExpectedVersion})
		if err == nil {
			out.Expense = &value
		}
		return out, err
	}
	entityID, version, suffix := item.ID, item.ExpectedVersion+1, "amended"
	if item.Action == "create" {
		entityID, version, suffix = id(), 1, "created"
	} else {
		var err error
		if item.Kind == "income" {
			err = lockIncome(ctx, tx, item.ID, item.ExpectedVersion)
		} else {
			err = lockTransfer(ctx, tx, item.ID, item.ExpectedVersion)
		}
		if err != nil {
			return out, err
		}
	}
	if item.Action == "void" {
		suffix = "voided"
	}
	var err error
	if item.Kind == "income" {
		var value Income
		if item.Action == "void" {
			err = db.New(tx).VoidIncome(ctx, item.ID)
			if err == nil {
				value, err = readIncome(ctx, tx, item.ID)
			}
		} else {
			d := item.Income
			value, err = writeIncome(ctx, tx, CreateIncomeInput{AccountID: d.AccountID, Currency: d.Currency, Amount: d.Amount, OccurredDate: d.OccurredDate, Description: d.Description}, entityID, version)
		}
		if err == nil {
			err = audit(ctx, tx, value.ID, "income_"+suffix, value)
			out.Income = &value
		}
	} else {
		var value Transfer
		if item.Action == "void" {
			err = db.New(tx).VoidTransfer(ctx, item.ID)
			if err == nil {
				value, err = readTransfer(ctx, tx, item.ID)
			}
		} else {
			d := item.Transfer
			value, err = writeTransfer(ctx, tx, CreateTransferInput{SourceAccountID: d.SourceAccountID, DestinationAccountID: d.DestinationAccountID, SourceCurrency: d.SourceCurrency, DestinationCurrency: d.DestinationCurrency, SourceAmount: d.SourceAmount, DestinationAmount: d.DestinationAmount, OccurredDate: d.OccurredDate, Description: d.Description}, entityID, version)
		}
		if err == nil {
			err = audit(ctx, tx, value.ID, "transfer_"+suffix, value)
			out.Transfer = &value
		}
	}
	return out, err
}
