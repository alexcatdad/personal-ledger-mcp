package ledger

import (
	"errors"
)

var ErrValidation = errors.New("validation")
var ErrConflict = errors.New("conflict")
var ErrNotFound = errors.New("not found")

type CreateAccountInput struct {
	OperationKey  string `json:"operation_key"`
	Name          string `json:"name"`
	Currency      string `json:"currency"`
	OpeningAmount string `json:"opening_amount"`
	OpeningDate   string `json:"opening_date,omitempty"`
}
type CreateNamedInput struct {
	OperationKey string `json:"operation_key"`
	Name         string `json:"name"`
}
type RenameNamedInput struct {
	OperationKey    string `json:"operation_key"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	ExpectedVersion int64  `json:"expected_version"`
}
type ArchiveNamedInput struct {
	OperationKey    string `json:"operation_key"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type AllocationInput struct {
	Remainder  bool   `json:"remainder,omitempty"`
	Amount     string `json:"amount,omitempty"`
	CategoryID string `json:"category_id"`
	ProjectID  string `json:"project_id,omitempty"`
}
type CreateExpenseInput struct {
	Payment      *ExpensePayment   `json:"payment,omitempty"`
	OperationKey string            `json:"operation_key"`
	AccountID    string            `json:"account_id"`
	Currency     string            `json:"currency"`
	Amount       string            `json:"amount"`
	OccurredDate string            `json:"date"`
	Description  string            `json:"description"`
	Allocations  []AllocationInput `json:"allocations"`
}
type AmendExpenseInput struct {
	CreateExpenseInput
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type VoidExpenseInput struct {
	OperationKey    string `json:"operation_key"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type SetAccountArchivedInput struct {
	OperationKey    string `json:"operation_key"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
	Archived        bool   `json:"archived"`
}
type Account struct {
	Version                int64  `json:"version,omitempty"`
	Archived               bool   `json:"archived"`
	BalanceQuality         string `json:"balance_quality"`
	EstimatedExpenseCount  int64  `json:"estimated_expense_count"`
	UnresolvedExpenseCount int64  `json:"unresolved_expense_count"`
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Currency               string `json:"currency"`
	OpeningAmount          string `json:"opening_amount"`
	OpeningDate            string `json:"opening_date"`
	Balance                string `json:"balance"`
}
type Named struct {
	Version  int64  `json:"version,omitempty"`
	Archived bool   `json:"archived"`
	ID       string `json:"id"`
	Name     string `json:"name"`
}
type Allocation struct {
	Amount     string `json:"amount"`
	CategoryID string `json:"category_id"`
	ProjectID  string `json:"project_id,omitempty"`
}
type Expense struct {
	Payment      ExpensePayment `json:"payment"`
	ID           string         `json:"id"`
	AccountID    string         `json:"account_id"`
	Amount       string         `json:"amount"`
	Currency     string         `json:"currency"`
	OccurredDate string         `json:"date"`
	Description  string         `json:"description"`
	Status       string         `json:"status"`
	Version      int64          `json:"version"`
	Allocations  []Allocation   `json:"allocations"`
}
type Spending struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	Currency    string `json:"currency"`
	Amount      string `json:"amount"`
}
type Dashboard struct {
	Accounts   []Account  `json:"accounts"`
	Expenses   []Expense  `json:"expenses"`
	Categories []Named    `json:"categories"`
	Projects   []Named    `json:"projects"`
	Spending   []Spending `json:"spending"`
}
type AuditEntry struct {
	Source        string         `json:"source"`
	OperationKey  string         `json:"operation_key"`
	RequestID     string         `json:"request_id"`
	ClientName    string         `json:"client_name"`
	ClientVersion string         `json:"client_version"`
	ID            int64          `json:"id"`
	EntityID      string         `json:"entity_id"`
	Action        string         `json:"action"`
	RecordedAt    string         `json:"recorded_at"`
	Snapshot      map[string]any `json:"snapshot"`
}

// ExpenseQuery selects expenses; category and project must match the same allocation.
// Date bounds are inclusive. Zero limit selects the default page size of 50.
type ExpenseQuery struct {
	PaymentState string `json:"payment_state,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
	Currency     string `json:"currency,omitempty"`
	FromDate     string `json:"from_date,omitempty"`
	ToDate       string `json:"to_date,omitempty"`
	CategoryID   string `json:"category_id,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
	Status       string `json:"status,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	Offset       int    `json:"offset,omitempty"`
}
type CurrencyTotal struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}
type AllocationTotal struct {
	CategoryID string `json:"category_id"`
	ProjectID  string `json:"project_id"`
	Currency   string `json:"currency"`
	Amount     string `json:"amount"`
}
type ExpensePage struct {
	MatchingAllocationTotals []AllocationTotal `json:"matching_allocation_totals"`
	Expenses                 []Expense         `json:"expenses"`
	TotalCount               int64             `json:"total_count"`
	// Totals sum complete posted expense payments across all matches, not just this
	// page or the allocations matching a category/project filter. Void entries never contribute.
	Totals []CurrencyTotal `json:"totals"`
}
