package vend

// PaymentRequest is the originating request from vend
type PaymentRequest struct {
	SaleID      string
	Amount      string
	Origin      string
	RegisterID  string
	Code        string
	AmountFloat float64
}

// RefundRequest is the originating request from vend
type RefundRequest struct {
	SaleID         string
	Amount         string
	Origin         string
	RegisterID     string
	PurchaseNumber string
	AmountFloat    float64
}
