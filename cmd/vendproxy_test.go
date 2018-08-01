package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/vend/peg/internal/pkg/oxipay"
	"github.com/vend/peg/internal/pkg/terminal"
	shortid "github.com/ventu-io/go-shortid"
)

func TestMain(m *testing.M) {
	// we need a database connection for most of the tests
	db := connectToDatabase()
	defer db.Close()
	oxipay.GatewayURL = "https://testpos.oxipay.com.au/webapi/v1/Test"
	oxipay.CreateKeyURL = oxipay.GatewayURL + "/CreateKey"

	returnCode := m.Run()

	os.Exit(returnCode)
}

// TestTerminalSave tests saving a new terminal in the database for the registration phase
func TestTerminalSave(t *testing.T) {
	// { Success SCRK01 Success VK5NGgc7nFJp 481f1e4098465f5229b33d91e0687c6123b91078e5c727b6d8ebf9360af145e7}
	var uniqueID, _ = shortid.Generate()

	terminal := &terminal.Terminal{
		FxlDeviceSigningKey: "VK5NGgc7nFJp",
		FxlRegisterID:       "Oxipos",
		FxlSellerID:         "30188105",
		Origin:              "http://pos.oxipay.com.au",
		VendRegisterID:      uniqueID,
	}
	saved, err := terminal.Save("unit-test")

	if err != nil || saved == false {
		t.Fatal(err)
	}
}

func TestTerminalUniqueSave(t *testing.T) {
	// { Success SCRK01 Success VK5NGgc7nFJp 481f1e4098465f5229b33d91e0687c6123b91078e5c727b6d8ebf9360af145e7}

	terminal := &terminal.Terminal{
		FxlDeviceSigningKey: "VK5NGgc7nFJp",
		FxlRegisterID:       "Oxipos",
		FxlSellerID:         "30188105",
		Origin:              "http://pos.oxipay.com.au",
		VendRegisterID:      "0d33b6af-7d33-4913-a310-7cd187ad4756",
	}
	// insert the same record twice so that we know it's erroring
	saved, err := terminal.Save("unit-test")
	saved, err = terminal.Save("unit-test")

	if err != nil && saved != false {
		t.Fatal(err)

	}
}

// TestRegisterHandler  generating oxipay payload
func TestRegisterHandler(t *testing.T) {

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	form := url.Values{}
	form.Add("MerchantID", "30188105")
	form.Add("Origin", "http://pos.oxipay.com.au")
	form.Add("FirmwareVersion", "1.0")

	form.Add("VendRegisterID", "13f35d8e-a5cf-4df1-b3af-79f045bb3c50")
	form.Add("DeviceToken", "01SUCCES") // for this to work against sandbox or prod it needs a real token
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
		t.Errorf("handler returned wrong status code: got %d want %d",
			status, http.StatusOK)
	}
}

// TestGeneratePayload generating oxipay payload assumes a registered device
// with both Oxipay and the local database
func TestProcessAuthorisationHandler(t *testing.T) {

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	form := url.Values{}
	form.Add("amount", "4400")
	form.Add("origin", "http://pos.oxipay.com.au")
	form.Add("paymentcode", "01APPROV") // needs a real payment code to succeed
	form.Add("register_id", "13f35d8e-a5cf-4df1-b3af-79f045bb3c50")

	req, err := http.NewRequest(http.MethodPost, "/pay", strings.NewReader(form.Encode()))

	if err != nil {
		t.Fatal(err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(PaymentHandler)

	// directly and pass in our Request and ResponseRecorder.
	handler.ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %d want %d",
			status, http.StatusOK)
		return
	}

	// Check the response body is what we expect.
	expected := `{ Success SCRK01 Success VK5NGgc7nFJp 481f1e4098465f5229b33d91e0687c6123b91078e5c727b6d8ebf9360af145e7}`
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}

func TestProcessAuthorisationRedirect(t *testing.T) {

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	form := url.Values{}
	form.Add("amount", "4400")
	form.Add("origin", "http://nonexistent.oxipay.com.au")
	form.Add("paymentcode", "012344")
	form.Add("register_id", "13f35d8e-a5cf-4df1-b3af-79f045bb3c50")

	req, err := http.NewRequest(http.MethodPost, "/pay", strings.NewReader(form.Encode()))

	if err != nil {
		t.Fatal(err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(PaymentHandler)

	// directly and pass in our Request and ResponseRecorder.
	handler.ServeHTTP(rr, req)

	// Check the status code is what we expect.
	if status := rr.Code; status != http.StatusTemporaryRedirect {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	if location := rr.HeaderMap.Get("Location"); location != "/register" {
		t.Errorf("Function redirects but redirects to %s rather than /register", location)
	}
}

func TestGeneratePayload(t *testing.T) {

	var oxipayPayload = oxipay.OxipayPayload{
		DeviceID:      "foobar",
		MerchantID:    "3342342",
		FinanceAmount: "1000",
		// FirmwareVersion: "version 4.0",
		// OperatorID:      "John",
		PurchaseAmount:  "1000",
		PreApprovalCode: "1234",
	}

	var plainText = oxipay.GeneratePlainTextSignature(oxipayPayload)
	t.Log("Plaintext", plainText)

	signature := oxipay.SignMessage(plainText, "TEST")
	correctSig := "7dfd655530d41cee284b3e4cb7d08a058edf7b5641dffd15fdf1b61ff6a8699b"

	if signature != correctSig {
		t.Fatalf("expected %s but got %s", correctSig, signature)
	}
}
