package vend

// VendPaymentRequest is the originating request from vend
type VendPaymentRequest struct {
	Amount     string
	Origin     string
	RegisterID string
	code       string
}
