package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGeneratePayload  generating oxipay payload
func TestRegisterHandler(t *testing.T) {

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	req, err := http.NewRequest(http.MethodPost, "/register", nil)
	if err != nil {
		t.Fatal(err)
	}

	req.Form.Add("MerchantID", "1234")
	req.Form.Add("Origin", "http://pos.oxipay.com.au")
	req.Form.Add("TerminalID", "1234")
	req.Form.Add("DeviceToken", "ABC 123")

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(RegisterHandler)

	// directly and pass in our Request and ResponseRecorder.
	handler.ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	// Check the response body is what we expect.
	// expected := `{"alive": true}`
	// if rr.Body.String() != expected {
	// 	t.Errorf("handler returned unexpected body: got %v want %v",
	// 		rr.Body.String(), expected)
	// }
}

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
