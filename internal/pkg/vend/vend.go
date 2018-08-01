package vend

// PaymentRequest is the originating request from vend
type PaymentRequest struct {
	Amount     string
	Origin     string
	RegisterID string
	Code       string
}
