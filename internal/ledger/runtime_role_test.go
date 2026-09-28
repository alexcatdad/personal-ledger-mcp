package ledger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exercise fresh writes using the shipped grants, not a superuser pool or saved
// idempotency results. SELECT FOR UPDATE also requires an UPDATE column grant.
func TestRuntimeRoleFreshLedgerLifecycle(t *testing.T) {
	owner, ctx := testService(t)
	role := "ledger_runtime_" + id()
	quotedRole := pgx.Identifier{role}.Sanitize()
	if _, err := owner.pool.Exec(ctx, "CREATE ROLE "+quotedRole+" NOLOGIN NOINHERIT"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := owner.pool.Exec(ctx, "DROP OWNED BY "+quotedRole); err != nil {
			t.Errorf("cleanup role grants: %v", err)
		}
		if _, err := owner.pool.Exec(ctx, "DROP ROLE "+quotedRole); err != nil {
			t.Errorf("cleanup role: %v", err)
		}
	})
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "grant-runtime.sql"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := owner.pool.Config().Copy()
	schema := cfg.ConnConfig.RuntimeParams["search_path"]
	var lines []string
	for _, line := range strings.Split(string(script), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "\\") {
			continue
		}
		lines = append(lines, line)
	}
	grants := strings.Join(lines, "\n")
	grants = strings.ReplaceAll(grants, `:"runtime_role"`, quotedRole)
	grants = strings.ReplaceAll(grants, ":DBNAME", pgx.Identifier{cfg.ConnConfig.Database}.Sanitize())
	grants = strings.ReplaceAll(grants, "SCHEMA public", "SCHEMA "+pgx.Identifier{schema}.Sanitize())
	if _, err = owner.pool.Exec(ctx, grants); err != nil {
		t.Fatal(err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+quotedRole)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var currentRole string
	if err = pool.QueryRow(ctx, "SELECT current_user").Scan(&currentRole); err != nil || currentRole != role {
		t.Fatalf("runtime role %q %v", currentRole, err)
	}
	runtime := New(pool)
	a, c, p := seed(t, runtime, ctx)
	intent, err := runtime.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "runtime-intent", Title: "Headphones"})
	if err != nil {
		t.Fatal(err)
	}
	intent, err = runtime.AddIntentCandidate(ctx, AddIntentCandidateInput{OperationKey: "runtime-candidate", IntentID: intent.ID, ExpectedVersion: 1, Reference: "Ad A", AdvertisedAmount: "500", Currency: "RON"})
	if err != nil {
		t.Fatal(err)
	}
	intent, err = runtime.UpdateBuyingIntent(ctx, UpdateBuyingIntentInput{OperationKey: "runtime-intent-update", IntentID: intent.ID, ExpectedVersion: intent.Version, Title: "Coffee shop headphones", Notes: "Prefer isolation"})
	if err != nil {
		t.Fatal(err)
	}
	intent, err = runtime.ConfirmIntentPurchase(ctx, ConfirmIntentPurchaseInput{OperationKey: "runtime-purchase", IntentID: intent.ID, ExpectedVersion: intent.Version, CandidateID: intent.Candidates[0].ID, NewExpense: intentDraft(a, c, p)})
	if err != nil {
		t.Fatal(err)
	}
	amended := expenseInput(a, c, p)
	amended.OperationKey = "runtime-amend"
	amended.Amount = "400"
	amended.Allocations = []AllocationInput{{Amount: "400", CategoryID: c.ID, ProjectID: p.ID}}
	expense, err := runtime.AmendExpense(ctx, AmendExpenseInput{CreateExpenseInput: amended, ID: intent.Expense.ID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.VoidExpense(ctx, VoidExpenseInput{OperationKey: "runtime-void", ID: expense.ID, ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	incomeIn := CreateIncomeInput{OperationKey: "runtime-income", AccountID: a.ID, Currency: "RON", Amount: "100", OccurredDate: "2026-09-27", Description: "Income"}
	income, err := runtime.CreateIncome(ctx, incomeIn)
	if err != nil {
		t.Fatal(err)
	}
	incomeIn.OperationKey = "runtime-income-amend"
	incomeIn.Amount = "200"
	income, err = runtime.AmendIncome(ctx, AmendIncomeInput{CreateIncomeInput: incomeIn, ID: income.ID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	page, err := runtime.QueryIncome(ctx, IncomeQuery{})
	if err != nil || page.TotalCount != 1 || page.Totals[0].Amount != "200.00" {
		t.Fatalf("runtime income: %+v %v", page, err)
	}
	if _, err = runtime.VoidIncome(ctx, VoidIncomeInput{OperationKey: "runtime-income-void", ID: income.ID, ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	cash, err := runtime.CreateAccount(ctx, CreateAccountInput{OperationKey: "runtime-cash", Name: "Cash", Currency: "RON", OpeningAmount: "0", OpeningDate: "2026-09-01"})
	if err != nil {
		t.Fatal(err)
	}
	transferIn := CreateTransferInput{OperationKey: "runtime-transfer", SourceAccountID: a.ID, DestinationAccountID: cash.ID, SourceCurrency: "RON", DestinationCurrency: "RON", SourceAmount: "50", DestinationAmount: "50", OccurredDate: "2026-09-27", Description: "Withdrawal"}
	transfer, err := runtime.CreateTransfer(ctx, transferIn)
	if err != nil {
		t.Fatal(err)
	}
	transferIn.OperationKey = "runtime-transfer-amend"
	transferIn.SourceAmount = "60"
	transferIn.DestinationAmount = "60"
	transfer, err = runtime.AmendTransfer(ctx, AmendTransferInput{CreateTransferInput: transferIn, ID: transfer.ID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	transfers, err := runtime.QueryTransfers(ctx, TransferQuery{})
	if err != nil || transfers.TotalCount != 1 {
		t.Fatalf("runtime transfers %+v %v", transfers, err)
	}
	if _, err = runtime.VoidTransfer(ctx, VoidTransferInput{OperationKey: "runtime-transfer-void", ID: transfer.ID, ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	mixed, err := runtime.BulkTransactions(ctx, BulkTransactionsInput{OperationKey: "runtime-mixed", Items: []BulkTransactionItem{
		{Kind: "income", Action: "create", Income: &IncomeDraft{AccountID: a.ID, Currency: "RON", Amount: "50", OccurredDate: "2026-09-27", Description: "Sample"}},
		{Kind: "expense", Action: "create", Expense: &ExpenseDraft{AccountID: a.ID, Currency: "RON", Amount: "50", OccurredDate: "2026-09-27", Description: "Sample", Allocations: []AllocationInput{{Remainder: true, CategoryID: c.ID}}}},
	}})
	if err != nil || len(mixed.Results) != 2 {
		t.Fatalf("runtime mixed %+v %v", mixed, err)
	}
	debt, err := runtime.CreateReceivable(ctx, CreateReceivableInput{OperationKey: "runtime-debt", Debtor: "Sample debtor", Amount: "100", Currency: "RON", Notes: "Existing"})
	if err != nil {
		t.Fatal(err)
	}
	repaymentIn := CreateRepaymentInput{OperationKey: "runtime-repayment", ReceivableID: debt.ID, ExpectedReceivableVersion: 1, AccountID: a.ID, Amount: "50", OccurredDate: "2026-09-27", Description: "Partial"}
	payment, err := runtime.CreateRepayment(ctx, repaymentIn)
	if err != nil {
		t.Fatal(err)
	}
	repaymentIn.OperationKey = "runtime-repayment-amend"
	repaymentIn.ExpectedReceivableVersion = 2
	repaymentIn.Amount = "60"
	payment, err = runtime.AmendRepayment(ctx, AmendRepaymentInput{CreateRepaymentInput: repaymentIn, ID: payment.Repayment.ID, ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	debts, err := runtime.QueryReceivables(ctx, ReceivableQuery{})
	if err != nil || debts.TotalCount != 1 || debts.Receivables[0].RemainingAmount != "40.00" {
		t.Fatalf("runtime debt %+v %v", debts, err)
	}
	if _, err = runtime.VoidRepayment(ctx, VoidRepaymentInput{OperationKey: "runtime-repayment-void", ID: payment.Repayment.ID, ReceivableID: debt.ID, ExpectedVersion: 2, ExpectedReceivableVersion: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.VoidReceivable(ctx, VoidReceivableInput{OperationKey: "runtime-debt-void", ID: debt.ID, ExpectedVersion: 4}); err != nil {
		t.Fatal(err)
	}
	observation, err := runtime.RecordBalanceObservation(ctx, RecordBalanceObservationInput{OperationKey: "runtime-observation", AccountID: a.ID, Currency: "RON", AsOfDate: "2026-09-27", ActualBalance: "990", Basis: "posted_end_of_day", Notes: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	adjustment, err := runtime.CreateBalanceAdjustment(ctx, CreateBalanceAdjustmentInput{OperationKey: "runtime-adjustment", ObservationID: observation.ID, ExpectedObservationVersion: 1, ExpectedLedgerBalance: "1000.00", Amount: "-10", Reason: "Test unexplained"})
	if err != nil {
		t.Fatal(err)
	}
	observations, err := runtime.QueryBalanceObservations(ctx, BalanceObservationQuery{})
	if err != nil || observations.TotalCount != 1 || observations.Observations[0].Difference != "0.00" {
		t.Fatalf("runtime reconciliation %+v %v", observations, err)
	}
	if _, err = runtime.VoidBalanceAdjustment(ctx, VoidBalanceAdjustmentInput{OperationKey: "runtime-adjustment-void", ID: adjustment.Adjustment.ID, ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.VoidBalanceObservation(ctx, VoidBalanceObservationInput{OperationKey: "runtime-observation-void", ID: observation.ID, ExpectedVersion: 3}); err != nil {
		t.Fatal(err)
	}
	dashboard, err := runtime.Dashboard(ctx)
	if err != nil || dashboard.Accounts[0].Balance != "1000.00" {
		t.Fatalf("runtime balance %+v %v", dashboard, err)
	}
	a, err = runtime.RenameAccount(ctx, RenameNamedInput{OperationKey: "runtime-account-rename", ID: a.ID, Name: "Renamed bank", ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	a, err = runtime.SetAccountArchived(ctx, SetAccountArchivedInput{OperationKey: "runtime-account-archive", ID: a.ID, ExpectedVersion: a.Version, Archived: true})
	if err != nil || !a.Archived {
		t.Fatal(a, err)
	}
	a, err = runtime.SetAccountArchived(ctx, SetAccountArchivedInput{OperationKey: "runtime-account-unarchive", ID: a.ID, ExpectedVersion: a.Version, Archived: false})
	if err != nil || a.Archived {
		t.Fatal(a, err)
	}
	for _, query := range []string{
		"UPDATE accounts SET currency='EUR'",
		"UPDATE accounts SET opening_minor=0",
		"UPDATE accounts SET opening_date='2000-01-01'",

		"UPDATE audit_entries SET action='tampered'",
		"DELETE FROM audit_entries",
		"CREATE TABLE " + pgx.Identifier{schema, "forbidden"}.Sanitize() + " (id integer)",
	} {
		_, err = pool.Exec(ctx, query)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("expected insufficient privilege for %s, got %v", query, err)
		}
	}
}
