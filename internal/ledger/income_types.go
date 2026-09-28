package ledger

type CreateIncomeInput struct {
	OperationKey string `json:"operation_key"`
	AccountID    string `json:"account_id"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	OccurredDate string `json:"date"`
	Description  string `json:"description"`
}
type AmendIncomeInput struct {
	CreateIncomeInput
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type VoidIncomeInput struct {
	OperationKey    string `json:"operation_key"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type Income struct {
	ID           string `json:"id"`
	AccountID    string `json:"account_id"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	OccurredDate string `json:"date"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	Version      int64  `json:"version"`
}

// IncomeQuery uses inclusive date bounds and defaults to 50 posted records.
type IncomeQuery struct {
	AccountID string `json:"account_id,omitempty"`
	Currency  string `json:"currency,omitempty"`
	FromDate  string `json:"from_date,omitempty"`
	ToDate    string `json:"to_date,omitempty"`
	Status    string `json:"status,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}
type IncomePage struct {
	Incomes    []Income `json:"incomes"`
	TotalCount int64    `json:"total_count"`
	// Totals cover all matching posted income, irrespective of pagination. Voids never contribute.
	Totals []CurrencyTotal `json:"totals"`
}
