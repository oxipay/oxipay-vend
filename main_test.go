package main

import "testing"

// TestGeneratePayload  generating oxipay payload
func TestGeneratePayload(t *testing.T) {

	var oxipayPayload = OxipayPayload{
		DeviceID:        "foobar",
		MerchantID:      "3342342",
		FinanceAmount:   1000,
		FirmwareVersion: "version 4.0",
		OperatorID:      "John",
		PurchaseAmount:  1000,
		PreApprovalCode: 1234,
	}

	var x = oxipayPayload.generatePayload()

	t.Log("Something v	", x)
}
