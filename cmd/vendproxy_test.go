package main

import (
	"database/sql"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	uuid "github.com/nu7hatch/gouuid"
	"github.com/vend/peg/internal/pkg/oxipay"
	"github.com/vend/peg/internal/pkg/terminal"
	"github.com/vend/peg/internal/pkg/vend"
	shortid "github.com/ventu-io/go-shortid"
)

var Db *sql.DB

func TestMain(m *testing.M) {
	// we need a database connection for most of the tests
	connectionParams := &DbConnection{
		username: "root",
		password: "t9e3ioz0",
		host:     "172.18.0.2",
		name:     "vend",
		timeout:  3600,
	}
	Db = connectToDatabase(connectionParams)
	defer Db.Close()

	terminal.Db = Db

	oxipay.GatewayURL = "https://testpos.oxipay.com.au/webapi/v1/Test"
	oxipay.CreateKeyURL = oxipay.GatewayURL + "/CreateKey"
	oxipay.ProcessAuthorisationURL = oxipay.GatewayURL + "/ProcessAuthorisation"
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
		Origin:              "http://pos.example.com",
		VendRegisterID:      uniqueID,
	}
	saved, err := terminal.Save("unit-test")

	if err != nil || saved == false {
		t.Fatal(err)
	}
}

// TestTerminalUniqueSave ensures that we get an error if we try to save the same terminal twice
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
	form.Add("DeviceToken", "01SUCCES") // for this to work against sandbox or prod it needs a real token

	req, err := http.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))

	if err != nil {
		t.Fatal(err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	// add vars to the seesion to simulate a redirect
	initSessionStore(Db, "ddddddddddddddddddd")

	session, err := SessionStore.Get(req, "oxipay")
	if err != nil {
		t.Errorf("Unable to get session store: %s ", err.Error())
		return
	}
	guid, _ := uuid.NewV4()
	vReq := &vend.PaymentRequest{
		RegisterID: guid.String(),
		Origin:     "http://pos.oxipay.com.au",
	}
	rr := httptest.NewRecorder()

	session.Values["vReq"] = vReq
	err = session.Save(req, rr)

	if err != nil {
		log.Fatal(err)
	}

	// We create a ResponseRecorder (which satisfies http.ResponseWriter) to record the response.

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

	var uniqueID, _ = uuid.NewV4()
	log.Printf("Generated RegisterID of %s \n", uniqueID)
	terminal := &terminal.Terminal{
		FxlDeviceSigningKey: "1234567890", // use hardcoded signing key for dummy endpoint
		FxlRegisterID:       "Oxipos",
		FxlSellerID:         "30188105",
		Origin:              "http://pos.example.com",
		VendRegisterID:      uniqueID.String(),
	}

	// we do this to ensure that it's registered already,
	// otherwise we are going to get a 302
	saved, err := terminal.Save("unit-test")
	if saved != true {
		t.Error("Unable to save register")
		return
	}

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	form := url.Values{}
	form.Add("amount", "4400")
	form.Add("origin", "http://pos.example.com")
	form.Add("paymentcode", "01APPROV") // needs a real payment code to succeed against sandbox / prod
	form.Add("register_id", uniqueID.String())

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

	response := new(Response)
	body, _ := ioutil.ReadAll(rr.Body)
	err = json.Unmarshal(body, response)

	if response.Status != "ACCEPTED" {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), "ACCEPTED")
	}
}

func TestProcessAuthorisationRedirect(t *testing.T) {

	var uniqueID, _ = uuid.NewV4()

	// Create a request to pass to our handler. We don't have any query parameters for now, so we'll
	// pass 'nil' as the third parameter.
	form := url.Values{}
	form.Add("amount", "4400")
	form.Add("origin", "http://nonexistent.oxipay.com.au")
	form.Add("paymentcode", "012344")
	form.Add("register_id", uniqueID.String())

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
	if status := rr.Code; status != http.StatusFound {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusFound)
	}

	if location := rr.HeaderMap.Get("Location"); location != "/register" {
		t.Errorf("Function redirects but redirects to %s rather than /register", location)
	}
}

func TestGeneratePayload(t *testing.T) {

	log.Print("hello")
	oxipayPayload := oxipay.OxipayPayload{
		DeviceID:        "foobar",
		MerchantID:      "3342342",
		FinanceAmount:   "1000",
		FirmwareVersion: "version 4.0",
		OperatorID:      "John",
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
