package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestTerminalSave tests saving a new terminal in the database for the registration phase
func TestTerminalSave(t *testing.T) {
	// { Success SCRK01 Success VK5NGgc7nFJp 481f1e4098465f5229b33d91e0687c6123b91078e5c727b6d8ebf9360af145e7}

	connectToDatabase()

	terminal := &Terminal{
		FxlDeviceSigningKey: "VK5NGgc7nFJp",
		FxlRegisterID:       "Oxipos",
		Origin:              "http://pos.oxipay.com.au",
		VendRegisterID:      "VendDevice01",
	}
	saved, err := terminal.save("andrewm")

	if err != nil && saved == true {
		t.Fatal(err)
	}
}

// TestGeneratePayload  generating oxipay payload
func TestRegisterHandler(t *testing.T) {

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	form := url.Values{}
	form.Add("MerchantID", "30188105")
	form.Add("Origin", "http://pos.oxipay.com.au")
	form.Add("FirmwareVersion", "1.0")
	form.Add("TerminalID", "1234")
	form.Add("DeviceID", "VendDevice01")
	form.Add("DeviceToken", "q3Cn2c55mDzl")
	form.Add("OperatorID", "Vend")

	req, err := http.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))

	if err != nil {
		t.Fatal(err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

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
	// expected := `{ Success SCRK01 Success VK5NGgc7nFJp 481f1e4098465f5229b33d91e0687c6123b91078e5c727b6d8ebf9360af145e7}`
	//  if rr.Body != expected {
	//  	t.Errorf("handler returned unexpected body: got %v want %v",
	//  		rr.Body.String(), expected)
	//  }
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

	var plainText = generatePlainTextSignature(oxipayPayload)
	t.Log("Plaintext", plainText)

	signature := SignMessage(plainText, "TEST")
	correctSig := "7dfd655530d41cee284b3e4cb7d08a058edf7b5641dffd15fdf1b61ff6a8699b"

	if signature != correctSig {
		t.Fatalf("expected %s but got %s", correctSig, signature)
	}
}
