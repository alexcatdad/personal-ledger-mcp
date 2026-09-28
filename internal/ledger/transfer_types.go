package ledger

type CreateTransferInput struct {
	OperationKey         string `json:"operation_key"`
	SourceAccountID      string `json:"source_account_id"`
	DestinationAccountID string `json:"destination_account_id"`
	SourceCurrency       string `json:"source_currency"`
	DestinationCurrency  string `json:"destination_currency"`
	SourceAmount         string `json:"source_amount"`
	DestinationAmount    string `json:"destination_amount"`
	OccurredDate         string `json:"date"`
	Description          string `json:"description"`
}
type AmendTransferInput struct {
	CreateTransferInput
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type VoidTransferInput struct {
	OperationKey    string `json:"operation_key"`
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expected_version"`
}
type Transfer struct {
	ID                   string `json:"id"`
	SourceAccountID      string `json:"source_account_id"`
	DestinationAccountID string `json:"destination_account_id"`
	SourceCurrency       string `json:"source_currency"`
	DestinationCurrency  string `json:"destination_currency"`
	SourceAmount         string `json:"source_amount"`
	DestinationAmount    string `json:"destination_amount"`
	OccurredDate         string `json:"date"`
	Description          string `json:"description"`
	Status               string `json:"status"`
	Version              int64  `json:"version"`
}

// TransferQuery uses inclusive date bounds and defaults to 50 posted records.
type TransferQuery struct {
	AccountID string `json:"account_id,omitempty"`
	FromDate  string `json:"from_date,omitempty"`
	ToDate    string `json:"to_date,omitempty"`
	Status    string `json:"status,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}
type TransferPage struct {
	Transfers  []Transfer `json:"transfers"`
	TotalCount int64      `json:"total_count"`
}
