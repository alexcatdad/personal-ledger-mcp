package ledger

type CreateBuyingIntentInput struct {
	OperationKey string `json:"operation_key"`
	Title        string `json:"title"`
	Notes        string `json:"notes,omitempty"`
}

// UpdateBuyingIntentInput replaces an open intent's title and notes in full.
type UpdateBuyingIntentInput struct {
	OperationKey    string `json:"operation_key"`
	IntentID        string `json:"intent_id"`
	ExpectedVersion int64  `json:"expected_version"`
	Title           string `json:"title"`
	Notes           string `json:"notes"`
}
type AddIntentCandidateInput struct {
	OperationKey     string `json:"operation_key"`
	IntentID         string `json:"intent_id"`
	ExpectedVersion  int64  `json:"expected_version"`
	Reference        string `json:"reference"`
	Notes            string `json:"notes,omitempty"`
	AdvertisedAmount string `json:"advertised_amount,omitempty"`
	Currency         string `json:"currency,omitempty"`
}
type CancelBuyingIntentInput struct {
	OperationKey    string `json:"operation_key"`
	IntentID        string `json:"intent_id"`
	ExpectedVersion int64  `json:"expected_version"`
	OutcomeNote     string `json:"outcome_note,omitempty"`
}
type ConfirmIntentPurchaseInput struct {
	OperationKey           string        `json:"operation_key"`
	IntentID               string        `json:"intent_id"`
	ExpectedVersion        int64         `json:"expected_version"`
	CandidateID            string        `json:"candidate_id,omitempty"`
	OutcomeNote            string        `json:"outcome_note,omitempty"`
	ExpenseID              string        `json:"expense_id,omitempty"`
	ExpectedExpenseVersion int64         `json:"expected_expense_version,omitempty"`
	NewExpense             *ExpenseDraft `json:"new_expense,omitempty"`
}
type IntentCandidate struct {
	ID               string `json:"id"`
	Reference        string `json:"reference"`
	Notes            string `json:"notes"`
	AdvertisedAmount string `json:"advertised_amount,omitempty"`
	Currency         string `json:"currency,omitempty"`
}
type BuyingIntent struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Notes       string            `json:"notes"`
	Status      string            `json:"status"`
	Version     int64             `json:"version"`
	Candidates  []IntentCandidate `json:"candidates"`
	CandidateID string            `json:"candidate_id,omitempty"`
	OutcomeNote string            `json:"outcome_note"`
	Expense     *Expense          `json:"expense,omitempty"`
}
type BuyingIntentQuery struct {
	Search string `json:"search,omitempty"`
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}
type BuyingIntentPage struct {
	Intents    []BuyingIntent `json:"intents"`
	TotalCount int64          `json:"total_count"`
}

type ExpenseDraft struct {
	Payment      *ExpensePayment   `json:"payment,omitempty"`
	AccountID    string            `json:"account_id"`
	Currency     string            `json:"currency"`
	Amount       string            `json:"amount"`
	OccurredDate string            `json:"date"`
	Description  string            `json:"description"`
	Allocations  []AllocationInput `json:"allocations"`
}
