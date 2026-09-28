package ledger

import (
	"errors"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"reflect"
	"testing"
)

func quote(rate string) *ConversionEvidence {
	return &ConversionEvidence{Source: "User supplied quote", Rate: rate, EffectiveAt: "2026-09-27T10:00:00Z", RetrievedAt: "2026-09-27T10:01:00Z", Reference: "Quote A"}
}
func foreignExpense(a Account, c, p Named) CreateExpenseInput {
	in := expenseInput(a, c, p)
	in.Currency = "EUR"
	in.Amount = "10"
	in.Allocations = []AllocationInput{{Amount: "6", CategoryID: c.ID, ProjectID: p.ID}, {Amount: "4", CategoryID: c.ID}}
	return in
}
func TestPaymentValidationAndRounding(t *testing.T) {
	for _, tc := range []struct {
		amount     int64
		rate, want string
	}{{1, "1.5", "0.02"}, {1, "1.499999999999999999", "0.01"}, {12345, "5.123456", "632.49"}} {
		p, _, _, e := expensePayment(&ExpensePayment{Currency: "RON", State: "estimated", Evidence: quote(tc.rate)}, "EUR", "RON", tc.amount)
		if e != nil || p.Amount != tc.want {
			t.Fatal(tc, p, e)
		}
	}
	for _, rate := range []string{"0", "-1", "1/2", "1e2", "NaN", "99999999999999999999999999999999999999999999", "0.0000001"} {
		if _, _, _, e := expensePayment(&ExpensePayment{Currency: "RON", State: "estimated", Evidence: quote(rate)}, "EUR", "RON", 1); !errors.Is(e, ErrValidation) {
			t.Fatal(rate, e)
		}
	}
	invalid := []ExpensePayment{{Currency: "EUR", State: "exact", Amount: "10"}, {Currency: "RON", State: "exact", Amount: "-1"}, {Currency: "RON", State: "exact", Amount: "10", Evidence: quote("1")}, {Currency: "RON", State: "unresolved", Amount: "10"}, {Currency: "RON", State: "estimated"}, {Currency: "RON", State: "estimated", Amount: "9", Evidence: quote("1")}}
	for _, in := range invalid {
		if _, _, _, e := expensePayment(&in, "EUR", "RON", 1000); !errors.Is(e, ErrValidation) {
			t.Fatal(in, e)
		}
	}
	bad := quote("5")
	bad.EffectiveAt = "2026-09-27"
	if _, _, _, e := expensePayment(&ExpensePayment{Currency: "RON", State: "estimated", Evidence: bad}, "EUR", "RON", 1000); !errors.Is(e, ErrValidation) {
		t.Fatal(e)
	}
}
func TestCrossCurrencyExpenseLifecycle(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	in := foreignExpense(a, c, p)
	in.Payment = &ExpensePayment{Currency: "RON", State: "exact", Amount: "51.23"}
	x, e := s.CreateExpense(ctx, in)
	if e != nil || x.Currency != "EUR" || x.Payment.Amount != "51.23" {
		t.Fatal(x, e)
	}
	replay, e := s.CreateExpense(ctx, in)
	if e != nil || !reflect.DeepEqual(x, replay) {
		t.Fatal(replay, e)
	}
	d, e := s.Dashboard(ctx)
	if e != nil || d.Accounts[0].Balance != "948.77" || d.Accounts[0].BalanceQuality != "exact" || d.Spending[0].Currency != "EUR" || d.Spending[0].Amount != "6.00" {
		t.Fatal(d, e)
	}
	page, e := s.QueryExpenses(ctx, ExpenseQuery{Currency: "EUR", ProjectID: p.ID})
	if e != nil || page.TotalCount != 1 || page.Totals[0].Amount != "10.00" || page.Totals[0].Currency != "EUR" || page.MatchingAllocationTotals[0].Amount != "6.00" {
		t.Fatal(page, e)
	}
	none, e := s.QueryExpenses(ctx, ExpenseQuery{Currency: "RON"})
	if e != nil || none.TotalCount != 0 {
		t.Fatal(none, e)
	}
	b, e := s.CreateAccount(ctx, CreateAccountInput{OperationKey: "eur", Name: "Euro", Currency: "EUR", OpeningAmount: "100", OpeningDate: "2026-09-01"})
	if e != nil {
		t.Fatal(e)
	}
	in.AccountID = b.ID
	in.OperationKey = "move"
	if _, e = s.AmendExpense(ctx, AmendExpenseInput{CreateExpenseInput: in, ID: x.ID, ExpectedVersion: 1}); !errors.Is(e, ErrValidation) {
		t.Fatal("old RON debit accepted", e)
	}
	in.Payment = nil
	x, e = s.AmendExpense(ctx, AmendExpenseInput{CreateExpenseInput: in, ID: x.ID, ExpectedVersion: 1})
	if e != nil || x.Payment.Currency != "EUR" || x.Payment.Amount != "10.00" {
		t.Fatal(x, e)
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "1000.00", b.ID: "90.00"})
	if _, e = s.VoidExpense(ctx, VoidExpenseInput{OperationKey: "void", ID: x.ID, ExpectedVersion: 2}); e != nil {
		t.Fatal(e)
	}
	transferBalances(t, s, ctx, map[string]string{a.ID: "1000.00", b.ID: "100.00"})
}
func TestCrossCurrencyQualityAndReconciliation(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	in := foreignExpense(a, c, p)
	unresolved, e := s.CreateExpense(ctx, in)
	if e != nil || unresolved.Payment.State != "unresolved" || unresolved.Payment.Amount != "" {
		t.Fatal(unresolved, e)
	}
	in.OperationKey = "estimate"
	in.Payment = &ExpensePayment{Currency: "RON", State: "estimated", Evidence: quote("5.0125")}
	estimated, e := s.CreateExpense(ctx, in)
	if e != nil || estimated.Payment.Amount != "50.13" {
		t.Fatal(estimated, e)
	}
	d, e := s.Dashboard(ctx)
	if e != nil || d.Accounts[0].Balance != "949.87" || d.Accounts[0].BalanceQuality != "incomplete" || d.Accounts[0].UnresolvedExpenseCount != 1 || d.Accounts[0].EstimatedExpenseCount != 1 {
		t.Fatal(d, e)
	}
	for state, want := range map[string]int64{"exact": 0, "estimated": 1, "unresolved": 1, "needs_attention": 2} {
		page, err := s.QueryExpenses(ctx, ExpenseQuery{PaymentState: state, AccountID: a.ID, Currency: "EUR", ProjectID: p.ID, Limit: 1})
		if err != nil || page.TotalCount != want {
			t.Fatal(state, page, err)
		}
		if want > 0 && (len(page.Totals) != 1 || page.Totals[0].Currency != "EUR" || len(page.MatchingAllocationTotals) != 1) {
			t.Fatal(state, page)
		}
		if state == "needs_attention" && (page.Totals[0].Amount != "20.00" || page.MatchingAllocationTotals[0].Amount != "12.00") {
			t.Fatal(page)
		}
	}
	o, e := s.RecordBalanceObservation(ctx, RecordBalanceObservationInput{OperationKey: "obs", AccountID: a.ID, Currency: "RON", AsOfDate: "2026-09-27", ActualBalance: "949.87", Basis: "posted_end_of_day"})
	if e != nil || o.Status == "matched" || o.Difference != "0.00" || o.BalanceQuality != "incomplete" {
		t.Fatal(o, e)
	}
	if _, e = s.CreateBalanceAdjustment(ctx, CreateBalanceAdjustmentInput{OperationKey: "adjust", ObservationID: o.ID, ExpectedObservationVersion: 1, ExpectedLedgerBalance: o.CurrentLedgerBalance, Amount: "1", Reason: "No"}); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	before, e := s.RecordBalanceObservation(ctx, RecordBalanceObservationInput{OperationKey: "before", AccountID: a.ID, Currency: "RON", AsOfDate: "2026-09-26", ActualBalance: "1000", Basis: "posted_end_of_day"})
	if e != nil || before.BalanceQuality != "exact" || before.Status != "matched" {
		t.Fatal(before, e)
	}
	in.OperationKey = "resolve"
	in.Payment = &ExpensePayment{Currency: "RON", State: "exact", Amount: "50"}
	if _, e = s.AmendExpense(ctx, AmendExpenseInput{CreateExpenseInput: in, ID: unresolved.ID, ExpectedVersion: 1}); e != nil {
		t.Fatal(e)
	}
	d, e = s.Dashboard(ctx)
	if e != nil || d.Accounts[0].BalanceQuality != "estimated" {
		t.Fatal(d, e)
	}
	if _, e = s.VoidExpense(ctx, VoidExpenseInput{OperationKey: "void-estimate", ID: estimated.ID, ExpectedVersion: 1}); e != nil {
		t.Fatal(e)
	}
	d, e = s.Dashboard(ctx)
	if e != nil || d.Accounts[0].BalanceQuality != "exact" || d.Accounts[0].Balance != "950.00" {
		t.Fatal(d, e)
	}
	page, e := s.QueryBalanceObservations(ctx, BalanceObservationQuery{})
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range page.Observations {
		if v.ID == o.ID {
			r, e := s.CreateBalanceAdjustment(ctx, CreateBalanceAdjustmentInput{OperationKey: "adjust", ObservationID: o.ID, ExpectedObservationVersion: 1, ExpectedLedgerBalance: v.CurrentLedgerBalance, Amount: "-0.13", Reason: "Confirmed difference"})
			if e != nil || r.Observation.Status != "matched" {
				t.Fatal(r, e)
			}
		}
	}
}
func TestCrossCurrencyBulkAndIntent(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	draft := intentDraft(a, c, p)
	draft.Currency = "USD"
	draft.Payment = &ExpensePayment{Currency: "RON", State: "estimated", Evidence: quote("4.5")}
	batch := BulkExpensesInput{OperationKey: "batch", Items: []BulkExpenseItem{{Action: "create", Expense: draft}, {Action: "create", Expense: &ExpenseDraft{AccountID: a.ID, Currency: "EUR", Amount: "10", OccurredDate: "2026-09-27", Allocations: []AllocationInput{{Amount: "9", CategoryID: c.ID}}}}}}
	if _, e := s.BulkExpenses(ctx, batch); !errors.Is(e, ErrValidation) {
		t.Fatal(e)
	}
	page, e := s.QueryExpenses(ctx, ExpenseQuery{})
	if e != nil || page.TotalCount != 0 {
		t.Fatal(page, e)
	}
	batch.Items = batch.Items[:1]
	r, e := s.BulkExpenses(ctx, batch)
	if e != nil || r.Expenses[0].Payment.Amount != "1935.00" {
		t.Fatal(r, e)
	}
	intent, e := s.CreateBuyingIntent(ctx, CreateBuyingIntentInput{OperationKey: "intent", Title: "Foreign purchase"})
	if e != nil {
		t.Fatal(e)
	}
	draft.Payment = &ExpensePayment{Currency: "RON", State: "exact", Amount: "2000"}
	bought, e := s.ConfirmIntentPurchase(ctx, ConfirmIntentPurchaseInput{OperationKey: "buy", IntentID: intent.ID, ExpectedVersion: 1, NewExpense: draft})
	if e != nil || bought.Expense.Payment.Amount != "2000.00" || bought.Expense.Currency != "USD" {
		t.Fatal(bought, e)
	}
}
func TestExpensePaymentMigrationBackfill(t *testing.T) {
	s, ctx := testService(t)
	migrationDB := stdlib.OpenDB(*s.pool.Config().ConnConfig)
	defer func() { _ = migrationDB.Close() }()
	if e := goose.DownToContext(ctx, migrationDB, "../../migrations", 8); e != nil {
		t.Fatal(e)
	}
	_, e := s.pool.Exec(ctx, `INSERT INTO accounts(id,name,currency,opening_minor,opening_date) VALUES('legacy-account','Legacy','RON',100000,'2026-09-01'); INSERT INTO categories(id,name) VALUES('legacy-cat','Legacy'); INSERT INTO expenses(id,account_id,amount_minor,occurred_date,description,version,status) VALUES('legacy-expense','legacy-account',1234,'2026-09-27','Legacy',1,'posted'),('legacy-void','legacy-account',500,'2026-09-27','Legacy void',2,'void'); INSERT INTO allocations(expense_id,ordinal,amount_minor,category_id) VALUES('legacy-expense',0,1234,'legacy-cat'),('legacy-void',0,500,'legacy-cat')`)
	if e != nil {
		t.Fatal(e)
	}
	if e = goose.UpContext(ctx, migrationDB, "../../migrations"); e != nil {
		t.Fatal(e)
	}
	d, e := s.Dashboard(ctx)
	if e != nil || d.Accounts[0].Balance != "987.66" || d.Accounts[0].BalanceQuality != "exact" {
		t.Fatal(d, e)
	}
	page, e := s.QueryExpenses(ctx, ExpenseQuery{Status: "all"})
	if e != nil || page.TotalCount != 2 || page.Totals[0].Amount != "12.34" {
		t.Fatal(page, e)
	}
	for _, x := range page.Expenses {
		if x.Payment.State != "exact" || x.Payment.Amount != x.Amount || x.Payment.Currency != "RON" {
			t.Fatal(x)
		}
	}
}

func TestExpensePaymentDatabaseConstraints(t *testing.T) {
	s, ctx := testService(t)
	a, c, p := seed(t, s, ctx)
	x, e := s.CreateExpense(ctx, expenseInput(a, c, p))
	if e != nil {
		t.Fatal(e)
	}
	for _, statement := range []string{
		`UPDATE expenses SET debit_minor=NULL WHERE id=$1`,
		`UPDATE expenses SET debit_state='estimated',debit_minor=NULL,conversion_evidence='{}'::jsonb WHERE id=$1`,
		`UPDATE expenses SET debit_state='unresolved' WHERE id=$1`,
		`UPDATE expenses SET debit_minor=0 WHERE id=$1`,
	} {
		if _, e = s.pool.Exec(ctx, statement, x.ID); !errors.Is(dbError(e), ErrValidation) {
			t.Fatalf("constraint accepted %s: %v", statement, e)
		}
	}
	page, e := s.QueryExpenses(ctx, ExpenseQuery{})
	if e != nil || page.Expenses[0].Payment.Amount != "430.00" {
		t.Fatal(page, e)
	}
}
