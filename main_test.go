package main

import "testing"

// TestGeneratePayload  generating oxipay payload
func TestGeneratePayload(t *testing.T) {

	var oxipayPayload = OxipayPayload{
		DeviceID:        "foobar",
		MerchantID:      "3342342",
		FinanceAmount:   "1000",
		FirmwareVersion: "version 4.0",
		OperatorID:      "John",
		PurchaseAmount:  "1000",
		PreApprovalCode: "1234",
	}

	var plainText = oxipayPayload.generatePayload()
	t.Log("Plaintext", plainText)

	signature := SignMessage(plainText, "TEST")
	correctSig := "7dfd655530d41cee284b3e4cb7d08a058edf7b5641dffd15fdf1b61ff6a8699b"

	if signature != correctSig {
		t.Fatalf("expected %s but got %s", correctSig, signature)
	}
}
