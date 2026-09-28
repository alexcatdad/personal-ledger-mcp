// Package transport exposes the same ledger through MCP mutations and a read-only API.
package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexcatdad/personal-ledger-mcp/internal/ledger"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Backend interface {
	QueryProjectReport(context.Context, ledger.ProjectReportQuery) (ledger.ProjectReport, error)
	QueryMonthlyReport(context.Context, ledger.MonthlyReportQuery) (ledger.MonthlyReport, error)
	RecordBalanceObservation(context.Context, ledger.RecordBalanceObservationInput) (ledger.BalanceObservation, error)
	AmendBalanceObservation(context.Context, ledger.AmendBalanceObservationInput) (ledger.BalanceObservation, error)
	VoidBalanceObservation(context.Context, ledger.VoidBalanceObservationInput) (ledger.BalanceObservation, error)
	CreateBalanceAdjustment(context.Context, ledger.CreateBalanceAdjustmentInput) (ledger.BalanceAdjustmentResult, error)
	VoidBalanceAdjustment(context.Context, ledger.VoidBalanceAdjustmentInput) (ledger.BalanceAdjustmentResult, error)
	QueryBalanceObservations(context.Context, ledger.BalanceObservationQuery) (ledger.BalanceObservationPage, error)
	QueryBalanceAdjustments(context.Context, ledger.BalanceAdjustmentQuery) (ledger.BalanceAdjustmentPage, error)
	CreateReceivable(context.Context, ledger.CreateReceivableInput) (ledger.Receivable, error)
	AmendReceivable(context.Context, ledger.AmendReceivableInput) (ledger.Receivable, error)
	VoidReceivable(context.Context, ledger.VoidReceivableInput) (ledger.Receivable, error)
	CreateRepayment(context.Context, ledger.CreateRepaymentInput) (ledger.RepaymentResult, error)
	AmendRepayment(context.Context, ledger.AmendRepaymentInput) (ledger.RepaymentResult, error)
	VoidRepayment(context.Context, ledger.VoidRepaymentInput) (ledger.RepaymentResult, error)
	QueryReceivables(context.Context, ledger.ReceivableQuery) (ledger.ReceivablePage, error)
	QueryRepayments(context.Context, ledger.RepaymentQuery) (ledger.RepaymentPage, error)
	BulkTransactions(context.Context, ledger.BulkTransactionsInput) (ledger.BulkTransactionsResult, error)
	CreateTransfer(context.Context, ledger.CreateTransferInput) (ledger.Transfer, error)
	AmendTransfer(context.Context, ledger.AmendTransferInput) (ledger.Transfer, error)
	VoidTransfer(context.Context, ledger.VoidTransferInput) (ledger.Transfer, error)
	QueryTransfers(context.Context, ledger.TransferQuery) (ledger.TransferPage, error)
	CreateIncome(context.Context, ledger.CreateIncomeInput) (ledger.Income, error)
	AmendIncome(context.Context, ledger.AmendIncomeInput) (ledger.Income, error)
	VoidIncome(context.Context, ledger.VoidIncomeInput) (ledger.Income, error)
	QueryIncome(context.Context, ledger.IncomeQuery) (ledger.IncomePage, error)
	BulkExpenses(context.Context, ledger.BulkExpensesInput) (ledger.BulkExpensesResult, error)
	CreateBuyingIntent(context.Context, ledger.CreateBuyingIntentInput) (ledger.BuyingIntent, error)
	UpdateBuyingIntent(context.Context, ledger.UpdateBuyingIntentInput) (ledger.BuyingIntent, error)
	AddIntentCandidate(context.Context, ledger.AddIntentCandidateInput) (ledger.BuyingIntent, error)
	CancelBuyingIntent(context.Context, ledger.CancelBuyingIntentInput) (ledger.BuyingIntent, error)
	ConfirmIntentPurchase(context.Context, ledger.ConfirmIntentPurchaseInput) (ledger.BuyingIntent, error)
	QueryBuyingIntents(context.Context, ledger.BuyingIntentQuery) (ledger.BuyingIntentPage, error)
	RenameCategory(context.Context, ledger.RenameNamedInput) (ledger.Named, error)
	RenameProject(context.Context, ledger.RenameNamedInput) (ledger.Named, error)
	ArchiveCategory(context.Context, ledger.ArchiveNamedInput) (ledger.Named, error)
	ArchiveProject(context.Context, ledger.ArchiveNamedInput) (ledger.Named, error)
	QueryExpenses(context.Context, ledger.ExpenseQuery) (ledger.ExpensePage, error)
	RenameAccount(context.Context, ledger.RenameNamedInput) (ledger.Account, error)
	SetAccountArchived(context.Context, ledger.SetAccountArchivedInput) (ledger.Account, error)
	CreateAccount(context.Context, ledger.CreateAccountInput) (ledger.Account, error)
	CreateCategory(context.Context, ledger.CreateNamedInput) (ledger.Named, error)
	CreateProject(context.Context, ledger.CreateNamedInput) (ledger.Named, error)
	CreateExpense(context.Context, ledger.CreateExpenseInput) (ledger.Expense, error)
	AmendExpense(context.Context, ledger.AmendExpenseInput) (ledger.Expense, error)
	VoidExpense(context.Context, ledger.VoidExpenseInput) (ledger.Expense, error)
	Dashboard(context.Context) (ledger.Dashboard, error)
	Audit(context.Context, string) ([]ledger.AuditEntry, error)
}

type AuditInput struct {
	ID string `json:"id" jsonschema:"Entity ID to inspect"`
}
type AuditOutput struct {
	Entries []ledger.AuditEntry `json:"entries"`
}
type Empty struct{}

func domainError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ledger.ErrValidation) || errors.Is(err, ledger.ErrConflict) || errors.Is(err, ledger.ErrNotFound) {
		return err
	}
	return errors.New("operation failed; retry with the same operation_key")
}
func addTool[I, O any](s *mcp.Server, name, description string, readOnly, destructive bool, fn func(context.Context, I) (O, error)) {
	closed := false
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, IdempotentHint: true, DestructiveHint: &destructive, OpenWorldHint: &closed}}, func(ctx context.Context, req *mcp.CallToolRequest, in I) (*mcp.CallToolResult, O, error) {
		name, version := "", ""
		if req != nil {
			if client := req.ClientInfo(); client != nil {
				name, version = client.Name, client.Version
			}
		}
		ctx = ledger.WithMCPClient(ctx, name, version)
		out, err := fn(ctx, in)
		var batchError *ledger.BulkValidationError
		if errors.As(err, &batchError) {
			payload, _ := json.Marshal(batchError)
			return nil, out, errors.New(string(payload))
		}
		return nil, out, domainError(err)
	})
}
func MCP(b Backend) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "pa-mcp", Version: "0.1.0"}, nil)
	addTool(s, "query_project_report", "Read project metadata, original-currency project allocation spending and category totals with posted expense history in one snapshot. Totals span all matching rows regardless of pagination. History contains complete purchases and other-project allocations; history.totals are whole-purchase totals, not project spending. Inclusive optional dates. Archived projects remain readable.", true, false, b.QueryProjectReport)
	addTool(s, "query_monthly_report", "Read a YYYY-MM report in one snapshot: original-currency spending and income, category/project allocations, and separate native-account movements. Unknown debits are excluded from numeric movement subtotals and counted as incomplete. Transfers and debt repayments are not income or spending.", true, false, b.QueryMonthlyReport)
	addTool(s, "record_balance_observation", "Compare a user-supplied actual balance with posted ledger movements through as_of_date. Explicit basis must be posted_end_of_day. Records a snapshot without changing money; pending balances are unsupported.", false, false, b.RecordBalanceObservation)
	addTool(s, "amend_balance_observation", "Correct an observation with expected_version when no active adjustment is linked. Does not move money.", false, true, b.AmendBalanceObservation)
	addTool(s, "void_balance_observation", "Void an erroneous observation with expected_version when no active adjustment is linked. Preserves audit history.", false, true, b.VoidBalanceObservation)
	addTool(s, "create_balance_adjustment", "Only on explicit user request, record the unexplained signed balance difference with reason, expected_observation_version and exact expected_ledger_balance from a fresh query. Never use for known fees or purchases. Stale comparisons are rejected.", false, true, b.CreateBalanceAdjustment)
	addTool(s, "void_balance_adjustment", "Void an adjustment with expected_version, restoring its previous balance effect and preserving audit. Use bulk_transactions adjustment void plus a known transaction to replace it atomically.", false, true, b.VoidBalanceAdjustment)
	addTool(s, "query_balance_observations", "Read recorded observations and current as-of ledger comparisons. Later corrections can change the current difference; recorded snapshots remain preserved. Uses bounded pagination.", true, false, b.QueryBalanceObservations)
	addTool(s, "query_balance_adjustments", "Inspect explicit balance adjustments by account, observation or status with bounded pagination. Adjustments are separate from income and spending.", true, false, b.QueryBalanceAdjustments)
	addTool(s, "create_receivable", "Record an existing debt owed to you: debtor, amount, currency, optional due_date and notes. Does not create an outgoing payment or income.", false, false, b.CreateReceivable)
	addTool(s, "amend_receivable", "Correct debt details with expected_version. Principal cannot fall below posted repayments; currency changes require no posted repayments.", false, true, b.AmendReceivable)
	addTool(s, "void_receivable", "Void a debt with expected_version only when it has no posted repayments. Explicitly correct or void repayments first; never silently remove received money.", false, true, b.VoidReceivable)
	addTool(s, "create_repayment", "Record principal received into a same-currency account with expected_receivable_version. Credits balance and reduces debt atomically without income. Rejects overpayment.", false, false, b.CreateRepayment)
	addTool(s, "amend_repayment", "Correct a repayment in the same receivable using both expected_version and expected_receivable_version. Updates debt and account effects atomically.", false, true, b.AmendRepayment)
	addTool(s, "void_repayment", "Void a repayment using both record and receivable expected versions. Removes account credit and restores outstanding debt atomically.", false, true, b.VoidRepayment)
	addTool(s, "query_receivables", "Read bounded debt records, defaulting to open debts, with full-filter outstanding totals separated by currency. Does not schedule reminders.", true, false, b.QueryReceivables)
	addTool(s, "query_repayments", "Inspect repayments and their versions by receivable/account/status with bounded pagination. Principal repayments are not income.", true, false, b.QueryRepayments)
	addTool(s, "create_transfer", "Record money moved between two different own accounts. Supply exact positive source debit and destination credit with account currencies; same-currency principal amounts must match. Fees are separate expenses. Never counts as income or spending.", false, false, b.CreateTransfer)
	addTool(s, "amend_transfer", "Correct both sides of a transfer atomically using expected_version and a retry key. Supply actual amounts; no automatic currency conversion.", false, true, b.AmendTransfer)
	addTool(s, "void_transfer", "Void both balance effects of a transfer atomically using expected_version, retaining audit history.", false, true, b.VoidTransfer)
	addTool(s, "query_transfers", "Inspect transfers using inclusive dates, status and either-leg account filter, with bounded pagination. Source and destination amounts retain their own currencies.", true, false, b.QueryTransfers)
	addTool(s, "create_income", "Record actual income received in an account. Amount is a positive decimal string in the account currency. Opening balances, transfers and loan principal repayments are not income.", false, false, b.CreateIncome)
	addTool(s, "amend_income", "Correct an income record using its expected_version. Replaces its fields atomically and preserves audit history.", false, true, b.AmendIncome)
	addTool(s, "void_income", "Void income using expected_version, removing its balance effect while preserving history.", false, true, b.VoidIncome)
	addTool(s, "query_income", "Query income by account, currency, inclusive dates and status with bounded pagination. Currency totals cover all matching posted records, excluding opening balances, independently of the page.", true, false, b.QueryIncome)
	addTool(s, "bulk_transactions", "Atomically create, amend or void 1-100 explicit expense, income, transfer or repayment items; repayment items also require receivable_id and expected_receivable_version. Adjustment kind supports void only to replace an unexplained adjustment with a real transaction. Set kind and action; supply only the matching draft for create/amend, or id and expected_version for void. One operation_key covers the batch. Indexed errors mean no writes committed. The agent owns statement parsing and duplicate reconciliation.", false, true, b.BulkTransactions)
	addTool(s, "bulk_expenses", "Atomically create, amend or void 1-100 expenses from explicit structured items. One operation_key protects the entire batch. No writes persist if any item fails; errors identify zero-based item indices. Reconcile existing records before submitting; newly keyed duplicate creates are not detected.", false, true, b.BulkExpenses)
	addTool(s, "create_buying_intent", "Record a potential purchase and its context. Does not move money or assume a purchase.", false, false, b.CreateBuyingIntent)
	addTool(s, "update_buying_intent", "Replace title and notes of an open buying intent using expected_version. Supply both fields; empty notes clears them. Preserves offers and does not move money. Closed decisions cannot be edited.", false, true, b.UpdateBuyingIntent)
	addTool(s, "add_intent_candidate", "Add an offer/ad reference and optional advertised amount/currency to an open intent using expected_version. The reference is stored, never fetched.", false, false, b.AddIntentCandidate)
	addTool(s, "cancel_buying_intent", "Cancel an open intent with expected_version and outcome context. Does not change balances.", false, true, b.CancelBuyingIntent)
	addTool(s, "confirm_intent_purchase", "Record a user-confirmed purchase with expected_version. Supply exactly one explicit new_expense or an existing expense_id with expected_expense_version. Atomically links the expense and marks purchased; never infer actual price from an offer.", false, true, b.ConfirmIntentPurchase)
	addTool(s, "query_buying_intents", "Read buying intents, candidate offers and current linked expense state. Optional search matches title or notes using a case-insensitive literal substring (maximum 200 characters). Defaults to open pending intents for assistant check-ins. Does not schedule or initiate purchases.", true, false, b.QueryBuyingIntents)
	addTool(s, "rename_account", "Rename an account using expected_version; currency and opening balance remain immutable. Query current account state before editing legacy retry results.", false, true, b.RenameAccount)
	addTool(s, "set_account_archived", "Archive or unarchive an account using expected_version. Archived accounts retain history and permit corrections to existing associations, but reject new financial transactions. Transfers must retain archived accounts on the same source or destination leg.", false, true, b.SetAccountArchived)
	addTool(s, "create_account", "Create a currency account with its opening amount. opening_date is optional and defaults to today in Europe/Bucharest; supply it for historical balances. Reuse operation_key only for retries.", false, false, b.CreateAccount)
	addTool(s, "create_category", "Create a flat expense category. Query existing categories first.", false, false, b.CreateCategory)
	addTool(s, "create_project", "Create a project for grouping expense allocations.", false, false, b.CreateProject)
	addTool(s, "rename_category", "Rename a category using its expected_version. Preserves expense references and records audit history.", false, true, b.RenameCategory)
	addTool(s, "rename_project", "Rename a project using its expected_version. Preserves expense references and records audit history.", false, true, b.RenameProject)
	addTool(s, "archive_category", "Archive a category using its expected_version. Prevents new assignments and retains historical reports.", false, true, b.ArchiveCategory)
	addTool(s, "archive_project", "Archive a project using its expected_version. Prevents new assignments and retains historical reports.", false, true, b.ArchiveProject)
	addTool(s, "create_expense", "Record an expense in its original currency with allocations totaling its original amount. Same-currency account debit is derived. For cross-currency payment supply exact bank amount or estimated rate evidence; omitted payment is unresolved and leaves the account balance incomplete. Amounts and rates are decimal strings.", false, false, b.CreateExpense)
	addTool(s, "amend_expense", "Replace an expense, its payment conversion and all allocations using expected_version. Supply the full current payment when retaining a cross-currency debit; omission makes it unresolved. Preserves audit history; retry with the same operation_key.", false, true, b.AmendExpense)
	addTool(s, "void_expense", "Void an expense using expected_version; preserve history and remove it from balances and spending.", false, true, b.VoidExpense)
	addTool(s, "query_expenses", "Inspect filtered expenses with bounded pagination. payment_state accepts exact, estimated, unresolved or needs_attention (estimated/unresolved) for assistant follow-ups. Totals cover original purchase amounts; matching_allocation_totals cover selected category/project lines. Both span the full filter scope, exclude voids, and separate currencies. Dates are inclusive; combined category/project filters match the same allocation.", true, false, b.QueryExpenses)
	addTool(s, "get_dashboard", "Read account balances, expenses, categories, projects and exact project spending separated by currency.", true, false, func(ctx context.Context, _ Empty) (ledger.Dashboard, error) { return b.Dashboard(ctx) })
	addTool(s, "get_audit", "Read the immutable recorded history of an entity.", true, false, func(ctx context.Context, in AuditInput) (AuditOutput, error) {
		v, e := b.Audit(ctx, in.ID)
		return AuditOutput{v}, e
	})
	return s
}

func API(mux *http.ServeMux, b Backend) huma.API {
	config := huma.DefaultConfig("Personal ledger", "0.1.0")
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{"bearer": {Type: "http", Scheme: "bearer"}}
	config.Security = []map[string][]string{{"bearer": {}}}
	api := humago.New(mux, config)
	huma.Get(api, "/api/projects/{id}/report", func(ctx context.Context, in *struct {
		ID       string `path:"id"`
		FromDate string `query:"from_date"`
		ToDate   string `query:"to_date"`
		Limit    int    `query:"limit"`
		Offset   int    `query:"offset"`
	}) (*struct{ Body ledger.ProjectReport }, error) {
		out, err := b.QueryProjectReport(ctx, ledger.ProjectReportQuery{ProjectID: in.ID, FromDate: in.FromDate, ToDate: in.ToDate, Limit: in.Limit, Offset: in.Offset})
		if errors.Is(err, ledger.ErrValidation) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if errors.Is(err, ledger.ErrNotFound) {
			return nil, huma.Error404NotFound("Project not found")
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to read project report")
		}
		return &struct{ Body ledger.ProjectReport }{out}, nil
	})
	huma.Get(api, "/api/reports/monthly", func(ctx context.Context, in *struct {
		Month     string `query:"month" required:"true"`
		AccountID string `query:"account_id"`
	}) (*struct{ Body ledger.MonthlyReport }, error) {
		out, err := b.QueryMonthlyReport(ctx, ledger.MonthlyReportQuery{Month: in.Month, AccountID: in.AccountID})
		if errors.Is(err, ledger.ErrValidation) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to read monthly report")
		}
		return &struct{ Body ledger.MonthlyReport }{out}, nil
	})
	huma.Get(api, "/api/balance-observations", func(ctx context.Context, in *balanceObservationParams) (*struct{ Body ledger.BalanceObservationPage }, error) {
		out, err := b.QueryBalanceObservations(ctx, ledger.BalanceObservationQuery{AccountID: in.AccountID, Limit: in.Limit, Offset: in.Offset})
		if errors.Is(err, ledger.ErrValidation) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to query observations")
		}
		return &struct{ Body ledger.BalanceObservationPage }{out}, nil
	})
	huma.Get(api, "/api/balance-adjustments", func(ctx context.Context, in *balanceAdjustmentParams) (*struct{ Body ledger.BalanceAdjustmentPage }, error) {
		out, err := b.QueryBalanceAdjustments(ctx, ledger.BalanceAdjustmentQuery{AccountID: in.AccountID, ObservationID: in.ObservationID, Status: in.Status, Limit: in.Limit, Offset: in.Offset})
		if errors.Is(err, ledger.ErrValidation) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to query adjustments")
		}
		return &struct{ Body ledger.BalanceAdjustmentPage }{out}, nil
	})
	huma.Get(api, "/api/receivables", func(ctx context.Context, in *receivableQueryParams) (*struct{ Body ledger.ReceivablePage }, error) {
		out, err := b.QueryReceivables(ctx, ledger.ReceivableQuery{Status: in.Status, Currency: in.Currency, Debtor: in.Debtor, Limit: in.Limit, Offset: in.Offset})
		if errors.Is(err, ledger.ErrValidation) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to query receivables")
		}
		return &struct{ Body ledger.ReceivablePage }{out}, nil
	})
	huma.Get(api, "/api/repayments", func(ctx context.Context, in *repaymentQueryParams) (*struct{ Body ledger.RepaymentPage }, error) {
		out, err := b.QueryRepayments(ctx, ledger.RepaymentQuery{ReceivableID: in.ReceivableID, AccountID: in.AccountID, Status: in.Status, Limit: in.Limit, Offset: in.Offset})
		if errors.Is(err, ledger.ErrValidation) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to query repayments")
		}
		return &struct{ Body ledger.RepaymentPage }{out}, nil
	})
	huma.Get(api, "/api/transfers", func(ctx context.Context, in *transferQueryParams) (*struct{ Body ledger.TransferPage }, error) {
		out, err := b.QueryTransfers(ctx, ledger.TransferQuery{AccountID: in.AccountID, FromDate: in.FromDate, ToDate: in.ToDate, Status: in.Status, Limit: in.Limit, Offset: in.Offset})
		if errors.Is(err, ledger.ErrValidation) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to query transfers")
		}
		return &struct{ Body ledger.TransferPage }{out}, nil
	})
	huma.Get(api, "/api/income", func(ctx context.Context, in *incomeQueryParams) (*struct{ Body ledger.IncomePage }, error) {
		out, err := b.QueryIncome(ctx, ledger.IncomeQuery{AccountID: in.AccountID, Currency: in.Currency, FromDate: in.FromDate, ToDate: in.ToDate, Status: in.Status, Limit: in.Limit, Offset: in.Offset})
		if errors.Is(err, ledger.ErrValidation) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to query income")
		}
		return &struct{ Body ledger.IncomePage }{out}, nil
	})
	huma.Get(api, "/api/dashboard", func(ctx context.Context, _ *struct{}) (*struct{ Body ledger.Dashboard }, error) {
		out, err := b.Dashboard(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to read ledger")
		}
		return &struct{ Body ledger.Dashboard }{out}, nil
	})
	huma.Get(api, "/api/audit/{id}", func(ctx context.Context, in *struct {
		ID string `path:"id"`
	}) (*struct{ Body AuditOutput }, error) {
		out, err := b.Audit(ctx, in.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to read history")
		}
		return &struct{ Body AuditOutput }{AuditOutput{out}}, nil
	})
	huma.Get(api, "/api/expenses", func(ctx context.Context, in *expenseQueryParams) (*struct{ Body ledger.ExpensePage }, error) {
		out, err := b.QueryExpenses(ctx, ledger.ExpenseQuery{PaymentState: in.PaymentState, AccountID: in.AccountID, Currency: in.Currency, FromDate: in.FromDate, ToDate: in.ToDate, CategoryID: in.CategoryID, ProjectID: in.ProjectID, Status: in.Status, Limit: in.Limit, Offset: in.Offset})
		if errors.Is(err, ledger.ErrValidation) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to query expenses")
		}
		return &struct{ Body ledger.ExpensePage }{out}, nil
	})
	huma.Get(api, "/api/buying-intents", func(ctx context.Context, in *struct {
		Search string `query:"search"`
		Status string `query:"status"`
		Limit  int    `query:"limit"`
		Offset int    `query:"offset"`
	}) (*struct{ Body ledger.BuyingIntentPage }, error) {
		out, err := b.QueryBuyingIntents(ctx, ledger.BuyingIntentQuery{Search: in.Search, Status: in.Status, Limit: in.Limit, Offset: in.Offset})
		if errors.Is(err, ledger.ErrValidation) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if err != nil {
			return nil, huma.Error500InternalServerError("Unable to query buying intents")
		}
		return &struct{ Body ledger.BuyingIntentPage }{out}, nil
	})
	return api
}

// Handler rejects missing configuration and checks credentials on every API/MCP request.
// Static assets contain no private data; the browser keeps its entered token in memory.
func Handler(b Backend, token, staticDir string, ping func(context.Context) error) (http.Handler, error) {
	return HandlerWithAuth(b, AuthConfig{Mode: "bearer", Token: token}, staticDir, ping)
}

func HandlerWithAuth(b Backend, config AuthConfig, staticDir string, ping func(context.Context) error) (http.Handler, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	protected := http.NewServeMux()
	API(protected, b)
	server := MCP(b)
	// Serve preserves the public Host on its loopback hop. Tailscale mode replaces
	// the SDK localhost heuristic with an exact canonical Host and proxy allowlist.
	protected.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: config.Mode == "tailscale"}))
	auth := config.protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		protected.ServeHTTP(w, r.WithContext(ctx))
	}))
	mux := http.NewServeMux()
	for _, pattern := range []string{"/api/", "/mcp", "/openapi.json", "/openapi.yaml", "/docs", "/schemas/"} {
		mux.Handle(pattern, auth)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if ping != nil && ping(ctx) != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n")) // The client may have disconnected.
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if staticDir == "" {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		file := filepath.Join(staticDir, filepath.FromSlash(name))
		stat, err := os.Stat(file)
		if err != nil || stat.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, file)
	})
	mux.HandleFunc("GET /auth/session", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		session := map[string]string{"mode": config.Mode}
		if config.Mode == "tailscale" {
			session["login"] = config.AllowedLogin
		}
		_ = json.NewEncoder(w).Encode(session) // A response write failure cannot be recovered here.
	})
	var root http.Handler = mux
	if config.Mode == "tailscale" {
		verified := config.protect(mux)
		root = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" && (r.Method == "GET" || r.Method == "HEAD") {
				mux.ServeHTTP(w, r)
				return
			}
			verified.ServeHTTP(w, r)
		})
	}
	crossOrigin := http.NewCrossOriginProtection().Handler(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		crossOrigin.ServeHTTP(w, r)
	}), nil
}

// Query parameters deliberately mirror the MCP/domain input, with domain validation authoritative.
type expenseQueryParams struct {
	PaymentState string `query:"payment_state"`
	AccountID    string `query:"account_id"`
	Currency     string `query:"currency"`
	FromDate     string `query:"from_date"`
	ToDate       string `query:"to_date"`
	CategoryID   string `query:"category_id"`
	ProjectID    string `query:"project_id"`
	Status       string `query:"status"`
	Limit        int    `query:"limit"`
	Offset       int    `query:"offset"`
}

type incomeQueryParams struct {
	AccountID string `query:"account_id"`
	Currency  string `query:"currency"`
	FromDate  string `query:"from_date"`
	ToDate    string `query:"to_date"`
	Status    string `query:"status"`
	Limit     int    `query:"limit"`
	Offset    int    `query:"offset"`
}

type transferQueryParams struct {
	AccountID string `query:"account_id"`
	FromDate  string `query:"from_date"`
	ToDate    string `query:"to_date"`
	Status    string `query:"status"`
	Limit     int    `query:"limit"`
	Offset    int    `query:"offset"`
}

type receivableQueryParams struct {
	Status   string `query:"status"`
	Currency string `query:"currency"`
	Debtor   string `query:"debtor"`
	Limit    int    `query:"limit"`
	Offset   int    `query:"offset"`
}
type repaymentQueryParams struct {
	ReceivableID string `query:"receivable_id"`
	AccountID    string `query:"account_id"`
	Status       string `query:"status"`
	Limit        int    `query:"limit"`
	Offset       int    `query:"offset"`
}

type balanceObservationParams struct {
	AccountID string `query:"account_id"`
	Limit     int    `query:"limit"`
	Offset    int    `query:"offset"`
}
type balanceAdjustmentParams struct {
	AccountID     string `query:"account_id"`
	ObservationID string `query:"observation_id"`
	Status        string `query:"status"`
	Limit         int    `query:"limit"`
	Offset        int    `query:"offset"`
}
