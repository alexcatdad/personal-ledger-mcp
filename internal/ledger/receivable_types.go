package ledger

type CreateReceivableInput struct {
	OperationKey string `json:"operation_key"`
	Debtor       string `json:"debtor"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	DueDate      string `json:"due_date,omitempty"`
	Notes        string `json:"notes"`
}
type AmendReceivableInput struct {
	CreateReceivableInput
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type VoidReceivableInput struct {
	OperationKey    string `json:"operation_key"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type Receivable struct {
	ID              string `json:"id"`
	Debtor          string `json:"debtor"`
	Amount          string `json:"amount"`
	Currency        string `json:"currency"`
	DueDate         string `json:"due_date,omitempty"`
	Notes           string `json:"notes"`
	RepaidAmount    string `json:"repaid_amount"`
	RemainingAmount string `json:"remaining_amount"`
	Status          string `json:"status"`
	Version         int64  `json:"version"`
}
type CreateRepaymentInput struct {
	OperationKey              string `json:"operation_key"`
	ReceivableID              string `json:"receivable_id"`
	ExpectedReceivableVersion int64  `json:"expected_receivable_version"`
	AccountID                 string `json:"account_id"`
	Amount                    string `json:"amount"`
	OccurredDate              string `json:"date"`
	Description               string `json:"description"`
}
type AmendRepaymentInput struct {
	CreateRepaymentInput
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type VoidRepaymentInput struct {
	OperationKey              string `json:"operation_key"`
	ID                        string `json:"id"`
	ReceivableID              string `json:"receivable_id"`
	ExpectedVersion           int64  `json:"expected_version"`
	ExpectedReceivableVersion int64  `json:"expected_receivable_version"`
}
type Repayment struct {
	ID           string `json:"id"`
	ReceivableID string `json:"receivable_id"`
	AccountID    string `json:"account_id"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	OccurredDate string `json:"date"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	Version      int64  `json:"version"`
}
type RepaymentResult struct {
	Repayment  Repayment  `json:"repayment"`
	Receivable Receivable `json:"receivable"`
}
type ReceivableQuery struct {
	Status   string `json:"status,omitempty"`
	Currency string `json:"currency,omitempty"`
	Debtor   string `json:"debtor,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}
type ReceivablePage struct {
	Receivables       []Receivable    `json:"receivables"`
	TotalCount        int64           `json:"total_count"`
	OutstandingTotals []CurrencyTotal `json:"outstanding_totals"`
}
type RepaymentQuery struct {
	ReceivableID string `json:"receivable_id,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
	Status       string `json:"status,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	Offset       int    `json:"offset,omitempty"`
}
type RepaymentPage struct {
	Repayments []Repayment `json:"repayments"`
	TotalCount int64       `json:"total_count"`
}
