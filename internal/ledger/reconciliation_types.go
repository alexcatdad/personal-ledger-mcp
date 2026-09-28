package ledger

type RecordBalanceObservationInput struct {
	OperationKey  string `json:"operation_key"`
	AccountID     string `json:"account_id"`
	Currency      string `json:"currency"`
	AsOfDate      string `json:"as_of_date"`
	ActualBalance string `json:"actual_balance"`
	Basis         string `json:"basis"`
	Notes         string `json:"notes"`
}
type AmendBalanceObservationInput struct {
	RecordBalanceObservationInput
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type VoidBalanceObservationInput struct {
	OperationKey    string `json:"operation_key"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type BalanceObservation struct {
	BalanceQuality         string `json:"balance_quality"`
	EstimatedExpenseCount  int64  `json:"estimated_expense_count"`
	UnresolvedExpenseCount int64  `json:"unresolved_expense_count"`
	ID                     string `json:"id"`
	AccountID              string `json:"account_id"`
	Currency               string `json:"currency"`
	AsOfDate               string `json:"as_of_date"`
	ActualBalance          string `json:"actual_balance"`
	Basis                  string `json:"basis"`
	Notes                  string `json:"notes"`
	RecordedLedgerBalance  string `json:"recorded_ledger_balance"`
	CurrentLedgerBalance   string `json:"current_ledger_balance"`
	Difference             string `json:"difference"`
	Status                 string `json:"status"`
	Version                int64  `json:"version"`
}
type BalanceObservationQuery struct {
	AccountID string `json:"account_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}
type BalanceObservationPage struct {
	Observations []BalanceObservation `json:"observations"`
	TotalCount   int64                `json:"total_count"`
}
type CreateBalanceAdjustmentInput struct {
	OperationKey               string `json:"operation_key"`
	ObservationID              string `json:"observation_id"`
	ExpectedObservationVersion int64  `json:"expected_observation_version"`
	ExpectedLedgerBalance      string `json:"expected_ledger_balance"`
	Amount                     string `json:"amount"`
	Reason                     string `json:"reason"`
}
type BalanceAdjustment struct {
	ID            string `json:"id"`
	ObservationID string `json:"observation_id"`
	AccountID     string `json:"account_id"`
	Currency      string `json:"currency"`
	Amount        string `json:"amount"`
	OccurredDate  string `json:"date"`
	Reason        string `json:"reason"`
	Status        string `json:"status"`
	Version       int64  `json:"version"`
}
type BalanceAdjustmentResult struct {
	Adjustment  BalanceAdjustment  `json:"adjustment"`
	Observation BalanceObservation `json:"observation"`
}
type VoidBalanceAdjustmentInput struct {
	OperationKey    string `json:"operation_key"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type BalanceAdjustmentQuery struct {
	AccountID     string `json:"account_id,omitempty"`
	ObservationID string `json:"observation_id,omitempty"`
	Status        string `json:"status,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	Offset        int    `json:"offset,omitempty"`
}
type BalanceAdjustmentPage struct {
	Adjustments []BalanceAdjustment `json:"adjustments"`
	TotalCount  int64               `json:"total_count"`
}
