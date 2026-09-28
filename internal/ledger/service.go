package ledger

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alexcatdad/personal-ledger-mcp/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
	_ "time/tzdata"
)

type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool, now: time.Now} }
func id() string {
	var b [16]byte
	_, err := rand.Read(b[:])
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func validName(s string) bool { return strings.TrimSpace(s) == s && len(s) > 0 && len(s) <= 200 }
func date(s string) error {
	parsed, e := time.Parse("2006-01-02", s)
	if e != nil || parsed.Year() < 1 {
		return fmt.Errorf("%w: invalid date", ErrValidation)
	}
	return nil
}
func dbError(e error) error {
	var p *pgconn.PgError
	if errors.As(e, &p) {
		switch p.Code {
		case "23505":
			return fmt.Errorf("%w: record already exists", ErrConflict)
		case "23503", "23514", "22003":
			return fmt.Errorf("%w: database constraint", ErrValidation)
		}
	}
	return e
}
func mutate[T any](ctx context.Context, s *Service, key, op string, input any, fn func(pgx.Tx) (T, error)) (out T, err error) {
	if strings.TrimSpace(key) == "" || len(key) > 200 {
		return out, fmt.Errorf("%w: operation_key required, maximum 200 characters", ErrValidation)
	}
	payload, e := json.Marshal(input)
	if e != nil {
		return out, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer func() { _ = tx.Rollback(ctx) }() // Rollback after commit returns pgx.ErrTxClosed.
	// This single-user ledger serializes writes so reconciliation can validate an
	// as-of balance and apply an adjustment without racing any movement or void.
	// The two-key advisory namespace is separate from the retry-key bigint locks.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1346456912,1)"); e != nil {
		return out, e
	}
	// A transaction-scoped lock serializes identical keys, including the first insert.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", key); e != nil {
		return out, e
	}
	q := db.New(tx)
	saved, e := q.GetOperation(ctx, key)
	if e == nil {
		if saved.Operation != op || !bytes.Equal(saved.Payload, payload) {
			return out, fmt.Errorf("%w: operation key already used with different payload", ErrConflict)
		}
		e = json.Unmarshal(saved.Result, &out)
		return out, e
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	provenance, _ := ctx.Value(auditContextKey{}).(auditProvenance)
	if provenance.source != "mcp" {
		provenance = auditProvenance{source: "application"}
	}
	provenance.operationKey = key
	provenance.requestID = id()
	out, e = fn(&auditedTx{Tx: tx, provenance: provenance})
	if e != nil {
		return out, dbError(e)
	}
	result, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	if e = q.SaveOperation(ctx, db.SaveOperationParams{OperationKey: key, Operation: op, Payload: payload, Result: result}); e != nil {
		return out, e
	}
	e = tx.Commit(ctx)
	return out, dbError(e)
}
func audit(ctx context.Context, tx pgx.Tx, entity, action string, value any) error {
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	provenance := auditProvenance{source: "unknown"}
	if t, ok := tx.(*auditedTx); ok {
		provenance = t.provenance
	}
	return db.New(tx).InsertAudit(ctx, db.InsertAuditParams{EntityID: entity, Action: action, Snapshot: b, Source: provenance.source, OperationKey: provenance.operationKey, RequestID: provenance.requestID, ClientName: provenance.clientName, ClientVersion: provenance.clientVersion})
}
func (s *Service) CreateAccount(ctx context.Context, in CreateAccountInput) (Account, error) {
	return mutate(ctx, s, in.OperationKey, "create_account", in, func(tx pgx.Tx) (Account, error) {
		// Resolve defaults after replay lookup so retries retain the original date.
		openingDate := in.OpeningDate
		if openingDate == "" {
			zone, err := time.LoadLocation("Europe/Bucharest")
			if err != nil {
				return Account{}, err
			}
			openingDate = s.now().In(zone).Format("2006-01-02")
		}
		a := Account{Version: 1, BalanceQuality: "exact", ID: id(), Name: in.Name, Currency: in.Currency, OpeningDate: openingDate}
		if !validName(in.Name) || (in.Currency != "RON" && in.Currency != "EUR" && in.Currency != "USD") {
			return a, fmt.Errorf("%w: invalid name or currency", ErrValidation)
		}
		n, e := parseMoney(in.OpeningAmount)
		if e != nil {
			return a, e
		}
		if e = date(a.OpeningDate); e != nil {
			return a, e
		}
		a.OpeningAmount = formatMoney(n)
		a.Balance = a.OpeningAmount
		e = db.New(tx).InsertAccount(ctx, db.InsertAccountParams{ID: a.ID, Name: a.Name, Currency: a.Currency, OpeningMinor: n, Column5: a.OpeningDate})
		if e == nil {
			e = audit(ctx, tx, a.ID, "account_created", a)
		}
		return a, e
	})
}
func (s *Service) named(ctx context.Context, in CreateNamedInput, table string) (Named, error) {
	return mutate(ctx, s, in.OperationKey, "create_"+table, in, func(tx pgx.Tx) (Named, error) {
		n := Named{ID: id(), Name: in.Name, Version: 1}
		if !validName(in.Name) {
			return n, fmt.Errorf("%w: invalid name", ErrValidation)
		}
		var e error
		if table == "categories" {
			e = db.New(tx).InsertCategory(ctx, db.InsertCategoryParams{ID: n.ID, Name: n.Name})
		} else {
			e = db.New(tx).InsertProject(ctx, db.InsertProjectParams{ID: n.ID, Name: n.Name})
		}
		if e == nil {
			e = audit(ctx, tx, n.ID, table+"_created", n)
		}
		return n, e
	})
}
func (s *Service) CreateCategory(ctx context.Context, in CreateNamedInput) (Named, error) {
	return s.named(ctx, in, "categories")
}
func (s *Service) CreateProject(ctx context.Context, in CreateNamedInput) (Named, error) {
	return s.named(ctx, in, "projects")
}
func validateExpense(in CreateExpenseInput) (int64, []int64, error) {
	n, e := parseMoney(in.Amount)
	if e != nil {
		return 0, nil, e
	}
	if n <= 0 || len(in.Allocations) == 0 || len(in.Allocations) > 100 || len(in.Description) > 2000 {
		return 0, nil, fmt.Errorf("%w: positive expense and 1-100 allocations required", ErrValidation)
	}
	if e = date(in.OccurredDate); e != nil {
		return 0, nil, e
	}
	parts := make([]int64, len(in.Allocations))
	remaining := n
	remainderIndex := -1
	for i, a := range in.Allocations {
		if a.CategoryID == "" {
			return 0, nil, fmt.Errorf("%w: category required", ErrValidation)
		}
		if a.Remainder {
			if remainderIndex >= 0 || a.Amount != "" {
				return 0, nil, fmt.Errorf("%w: one remainder without an amount is allowed", ErrValidation)
			}
			remainderIndex = i
			continue
		}
		v, e := parseMoney(a.Amount)
		if e != nil {
			return 0, nil, e
		}
		if v <= 0 || v > remaining || a.CategoryID == "" {
			return 0, nil, fmt.Errorf("%w: allocation exceeds total or invalid category", ErrValidation)
		}
		remaining -= v
		parts[i] = v
	}
	if remainderIndex >= 0 {
		if remaining <= 0 {
			return 0, nil, fmt.Errorf("%w: remainder must be positive", ErrValidation)
		}
		parts[remainderIndex] = remaining
		remaining = 0
	}
	if remaining != 0 {
		return 0, nil, fmt.Errorf("%w: allocations must equal expense", ErrValidation)
	}
	return n, parts, nil
}
func writeExpense(ctx context.Context, tx pgx.Tx, in CreateExpenseInput, expenseID string, version int64) (Expense, error) {
	out := Expense{ID: expenseID, AccountID: in.AccountID, OccurredDate: in.OccurredDate, Description: in.Description, Status: "posted", Version: version, Allocations: make([]Allocation, len(in.Allocations))}
	amount, parts, e := validateExpense(in)
	if e != nil {
		return out, e
	}
	account, e := db.New(tx).LockAccount(ctx, in.AccountID)
	out.Currency = in.Currency
	opening := account.OpeningDate
	if errors.Is(e, pgx.ErrNoRows) {
		return out, fmt.Errorf("%w: account", ErrNotFound)
	}
	if e != nil {
		return out, e
	}
	if account.Archived {
		old, err := db.New(tx).ReadExpense(ctx, expenseID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		if err != nil || old.AccountID != in.AccountID {
			return out, archivedAccountError()
		}
	}
	payment, debit, evidence, e := expensePayment(in.Payment, in.Currency, account.Currency, amount)
	if e != nil {
		return out, e
	}
	out.Payment = payment
	if in.OccurredDate < opening {
		return out, fmt.Errorf("%w: expense precedes opening balance", ErrValidation)
	}
	if e = validateTaxonomy(ctx, tx, in.Allocations, expenseID); e != nil {
		return out, e
	}
	out.Amount = formatMoney(amount)
	e = db.New(tx).UpsertExpense(ctx, db.UpsertExpenseParams{ID: out.ID, AccountID: out.AccountID, AmountMinor: amount, OccurredDate: out.OccurredDate, Description: out.Description, Version: version, Currency: out.Currency, DebitMinor: debit, DebitState: payment.State, ConversionEvidence: evidence})
	if e != nil {
		return out, e
	}
	if e = db.New(tx).DeleteAllocations(ctx, out.ID); e != nil {
		return out, e
	}
	for i, a := range in.Allocations {
		out.Allocations[i] = Allocation{Amount: formatMoney(parts[i]), CategoryID: a.CategoryID, ProjectID: a.ProjectID}
		e = db.New(tx).InsertAllocation(ctx, db.InsertAllocationParams{ExpenseID: out.ID, Ordinal: int32(i), AmountMinor: parts[i], CategoryID: a.CategoryID, Column5: a.ProjectID})
		if e != nil {
			return out, e
		}
	}
	return out, nil
}
func (s *Service) CreateExpense(ctx context.Context, in CreateExpenseInput) (Expense, error) {
	return mutate(ctx, s, in.OperationKey, "create_expense", in, func(tx pgx.Tx) (Expense, error) {
		out, e := writeExpense(ctx, tx, in, id(), 1)
		if e == nil {
			e = audit(ctx, tx, out.ID, "expense_created", out)
		}
		return out, e
	})
}
func lockExpense(ctx context.Context, tx pgx.Tx, expenseID string, expected int64) error {
	row, e := db.New(tx).LockExpense(ctx, expenseID)
	version, status := row.Version, row.Status
	if errors.Is(e, pgx.ErrNoRows) {
		return fmt.Errorf("%w: expense", ErrNotFound)
	}
	if e != nil {
		return e
	}
	if version != expected || status != "posted" {
		return fmt.Errorf("%w: stale version or void expense", ErrConflict)
	}
	return nil
}
func (s *Service) AmendExpense(ctx context.Context, in AmendExpenseInput) (Expense, error) {
	return mutate(ctx, s, in.OperationKey, "amend_expense", in, func(tx pgx.Tx) (Expense, error) {
		if e := lockExpense(ctx, tx, in.ID, in.ExpectedVersion); e != nil {
			return Expense{}, e
		}
		out, e := writeExpense(ctx, tx, in.CreateExpenseInput, in.ID, in.ExpectedVersion+1)
		if e == nil {
			e = audit(ctx, tx, out.ID, "expense_amended", out)
		}
		return out, e
	})
}
func (s *Service) VoidExpense(ctx context.Context, in VoidExpenseInput) (Expense, error) {
	return mutate(ctx, s, in.OperationKey, "void_expense", in, func(tx pgx.Tx) (Expense, error) {
		if e := lockExpense(ctx, tx, in.ID, in.ExpectedVersion); e != nil {
			return Expense{}, e
		}
		e := db.New(tx).VoidExpense(ctx, in.ID)
		if e != nil {
			return Expense{}, e
		}
		out, e := readExpense(ctx, tx, in.ID)
		if e == nil {
			e = audit(ctx, tx, out.ID, "expense_voided", out)
		}
		return out, e
	})
}
