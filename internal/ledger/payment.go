package ledger

import (
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgtype"
	"math/big"
	"regexp"
	"strings"
	"time"
)

// ExpensePayment separates original purchase value from the paying account debit.
// A quote is an estimate even if its calculated amount happens to match the bank.
type ExpensePayment struct {
	Currency string              `json:"currency"`
	State    string              `json:"state"`
	Amount   string              `json:"amount,omitempty"`
	Evidence *ConversionEvidence `json:"evidence,omitempty"`
}
type ConversionEvidence struct {
	Source      string `json:"source"`
	Rate        string `json:"rate"`
	EffectiveAt string `json:"effective_at"`
	RetrievedAt string `json:"retrieved_at"`
	Reference   string `json:"reference,omitempty"`
}

var ratePattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func balanceQuality(estimated, unresolved int64) string {
	if unresolved > 0 {
		return "incomplete"
	}
	if estimated > 0 {
		return "estimated"
	}
	return "exact"
}
func expensePayment(in *ExpensePayment, originalCurrency, accountCurrency string, amount int64) (ExpensePayment, pgtype.Int8, []byte, error) {
	out := ExpensePayment{Currency: accountCurrency, State: "unresolved"}
	var debit pgtype.Int8
	invalid := func(message string) (ExpensePayment, pgtype.Int8, []byte, error) {
		return out, debit, nil, fmt.Errorf("%w: %s", ErrValidation, message)
	}
	if originalCurrency != "RON" && originalCurrency != "EUR" && originalCurrency != "USD" {
		return invalid("unsupported original currency")
	}
	if originalCurrency == accountCurrency {
		out.State = "exact"
		out.Amount = formatMoney(amount)
		if in != nil {
			supplied, e := parseMoney(in.Amount)
			if in.Currency != accountCurrency || in.State != "exact" || in.Evidence != nil || e != nil || supplied != amount {
				return invalid("same-currency payment must equal original amount without conversion evidence")
			}
		}
		return out, pgtype.Int8{Int64: amount, Valid: true}, nil, nil
	}
	if in == nil {
		return out, debit, nil, nil
	}
	if in.Currency != accountCurrency {
		return invalid("payment currency must match paying account")
	}
	out = *in
	switch in.State {
	case "unresolved":
		if in.Amount != "" || in.Evidence != nil {
			return invalid("unresolved payment cannot contain amount or evidence")
		}
		return out, debit, nil, nil
	case "exact":
		n, e := parseMoney(in.Amount)
		if e != nil || n <= 0 || in.Evidence != nil {
			return invalid("exact bank debit requires positive amount without estimated quote evidence")
		}
		out.Amount = formatMoney(n)
		return out, pgtype.Int8{Int64: n, Valid: true}, nil, nil
	case "estimated":
		ev := in.Evidence
		if ev == nil || strings.TrimSpace(ev.Source) == "" || len(ev.Source) > 200 || len(ev.Reference) > 2000 || len(ev.Rate) > 100 || !ratePattern.MatchString(ev.Rate) {
			return invalid("estimate requires bounded source, decimal rate and quote evidence")
		}
		effective, e := time.Parse(time.RFC3339Nano, ev.EffectiveAt)
		if e != nil || effective.Year() < 1 {
			return invalid("effective_at must be RFC3339")
		}
		retrieved, e := time.Parse(time.RFC3339Nano, ev.RetrievedAt)
		if e != nil || retrieved.Year() < 1 {
			return invalid("retrieved_at must be RFC3339")
		}
		rate, ok := new(big.Rat).SetString(ev.Rate)
		if !ok || rate.Sign() <= 0 {
			return invalid("rate must be positive account currency units per original currency unit")
		}
		// RON/EUR/USD each use two minor units. Multiplying original minor units by
		// the exact decimal rate preserves scale. Round positive halves upward once.
		converted := new(big.Rat).Mul(new(big.Rat).SetInt64(amount), rate)
		n, rem := new(big.Int), new(big.Int)
		n.QuoRem(converted.Num(), converted.Denom(), rem)
		if new(big.Int).Lsh(rem, 1).Cmp(converted.Denom()) >= 0 {
			n.Add(n, big.NewInt(1))
		}
		if !n.IsInt64() || n.Sign() <= 0 {
			return invalid("converted debit rounds to zero or exceeds supported range")
		}
		if in.Amount != "" {
			supplied, e := parseMoney(in.Amount)
			if e != nil || supplied != n.Int64() {
				return invalid("estimated amount must match rounded original amount times rate")
			}
		}
		out.Amount = formatMoney(n.Int64())
		evidence, e := json.Marshal(ev)
		return out, pgtype.Int8{Int64: n.Int64(), Valid: true}, evidence, e
	default:
		return invalid("payment state must be exact, estimated or unresolved")
	}
}
