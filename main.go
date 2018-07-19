// package main is a simple webservice for hosting Pay Example flow screens.
package main

import (
	"bytes"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	// "time"s
	_ "crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"sort"

	gouuid "github.com/nu7hatch/gouuid"

	_ "github.com/go-sql-driver/mysql"
	shortid "github.com/ventu-io/go-shortid"
)

// These are the possible sale statuses.
const (
	statusAccepted  = "ACCEPTED"
	statusCancelled = "CANCELLED"
	statusDeclined  = "DECLINED"
	statusFailed    = "FAILED"
	statusTimeout   = "TIMEOUT"
	statusUnknown   = "UNKNOWN"
)

// Terminal terminal mapping
type Terminal struct {
	FxlRegisterID       string // Oxipay registerid
	FxlSellerID         string
	FxlDeviceSigningKey string
	Origin              string
	VendRegisterID      string
}

// OxipayPayload Payload used to send to Oxipay
type OxipayPayload struct {
	MerchantID        string `json:"x_merchant_id"`
	DeviceID          string `json:"x_device_id"`
	OperatorID        string `json:"x_operator_id"`
	FirmwareVersion   string `json:"x_firmware_version"`
	PosTransactionRef string `json:"x_pos_transaction_ref"`
	PreApprovalCode   string `json:"x_pre_approval_code"`
	FinanceAmount     string `json:"x_finance_amount"`
	PurchaseAmount    string `json:"x_purchase_amount"`
	Signature         string `json:"signature"`
}

// OxipayResponse is the response returned from Oxipay
type OxipayResponse struct {
	PurchaseNumber string `json:"x_purchase_number"`
	Status         string `json:"x_status"`
	Code           string `json:"x_code"`
	Message        string `json:"x_message"`
}

// Response We build a JSON response object that contains important information for
// which step we should send back to Vend to guide the payment flow.
type Response struct {
	ID           string  `json:"id"`
	Amount       float64 `json:"amount"`
	RegisterID   string  `json:"register_id"`
	Status       string  `json:"status"`
	Signature    string  `json:"signature"`
	TrackingData string  `json:"tracking_data"`
}

var db *sql.DB

func main() {

	// @todo pull from config
	dbUser := "root"
	dbPassword := "t9e3ioz0"
	host := "172.18.0.2"
	dbName := "vend"
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s", dbUser, dbPassword, host, dbName)

	log.Printf("Attempting to connect to database %s \n", dsn)

	// connect to the database
	// @todo grab config
	var err error
	db, err = sql.Open("mysql", dsn)

	//defer db.Close()

	if err != nil {
		log.Printf("Unable to connect")
		log.Fatal(err)
	}

	// test to make sure it's all good
	if err := db.Ping(); err != nil {
		log.Printf("Unable to connect to %s", dbName)
		log.Fatal(err)
	}

	// We are hosting all of the content in ./assets, as the resources are
	// required by the frontend.
	fileServer := http.FileServer(http.Dir("assets"))
	http.Handle("/assets/", http.StripPrefix("/assets/", fileServer))
	http.HandleFunc("/", Index)

	http.HandleFunc("/pay", PaymentHandler)

	if http.ReadRequest().Method)
		http.HandleFunc("/register", RegisterHandler)
	}

	// The default port is 500, but one can be specified as an env var if needed.
	port := "5000"
	if os.Getenv("PORT") != "" {
		port = os.Getenv("PORT")
	}

	log.Printf("Starting webserver on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// RegisterHandler GET request. Prompt for the Merchant ID and Device Token
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Print(r.Method)
	http.ServeFile(w, r, "./assets/templates/register.html")
}

func processAuthorisation(oxipayPayload OxipayPayload) (*OxipayResponse, error) {
	var authorisationURL = "https://sandboxpos.oxipay.com.au/webapi/v1/ProcessAuthorisation"

	var err error

	//var authorisationURL = "http://localhost:4000"

	jsonValue, _ := json.Marshal(oxipayPayload)

	fmt.Println(string(jsonValue))

	request, err := http.NewRequest("POST", authorisationURL, bytes.NewBuffer(jsonValue))
	// @todo don't swallow errors
	if err != nil {
		panic(err)
	}
	request.Header.Set("Content-Type", "application/json")

	client := http.Client{}
	response, responseErr := client.Post(authorisationURL, "application/json", bytes.NewBuffer(jsonValue))

	// response, responseErr := client.Do(request)
	if responseErr != nil {
		panic(responseErr)
	}
	defer response.Body.Close()
	fmt.Println("response Status:", response.Status)
	fmt.Println("response Headers:", response.Header)
	body, _ := ioutil.ReadAll(response.Body)

	// turn {"x_purchase_number":"52011595","x_status":"Success","x_code":"SPRA01","x_message":"Approved","signature":"84b2ed2ec504a0aef134c3da57a060558de1290de7d5055ab8d070dd8354991b","tracking_data":null}
	// into a struct
	oxipayResponse := new(OxipayResponse)
	err = json.Unmarshal(body, oxipayResponse)

	if err != nil {
		return nil, err
	}

	fmt.Println("response Body:", oxipayResponse)
	return oxipayResponse, err
}

// Index displays the main payment processing page, giving the user options of
// which outcome they would like the Pay Example to simulate.
func Index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./assets/templates/index.html")
}

// PaymentHandler receives the payment request from Vend and sends it to the
// payment gateway.
func PaymentHandler(w http.ResponseWriter, r *http.Request) {
	// Vend sends multiple arguments for use by the gateway.
	// "amount" is the subtotal of the sale including tax.
	// "origin" is the Vend store URL that the transaction came from.
	//
	// optional:
	// "register_id" is the ID of the Vend register that sent the transaction.
	// "outcome" is the desired outcome of the payment flow.
	r.ParseForm()
	amount := r.Form.Get("amount")
	outcome := r.Form.Get("outcome") // IMPORTANT: this only applies to this package and is never sent in production.
	origin := r.Form.Get("origin")
	origin, _ = url.PathUnescape(origin) // @todo, do we care about errors , we should validate?
	code := r.Form.Get("paymentcode")

	registerID := r.Form.Get("register_id")

	// Reject requests with required arguments that are empty. By default Vend
	// on both Web and iOS will always send at least amount and origin values.
	// The "DATA" step can be used to obtain extra details like register_id,
	// that should be used to associate a register with a payment terminal.
	if amount == "" || origin == "" {
		log.Printf("received empty param value. required: amount %s origin %s optional: register_id %s outcome %s", amount, origin, registerID, outcome)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	log.Printf("RegisterID is %s ", registerID)

	// If no outcome was specified, then just follow the happy transaction flow.
	if outcome == "" {
		outcome = statusAccepted
	}

	// Convert the amount string to a float.
	amountFloat, err := strconv.ParseFloat(amount, 64)
	if err != nil {
		log.Println("failed to convert amount string to float: ", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Reject zero amounts.
	if amountFloat == 0 {
		log.Println("zero amount received")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	log.Printf("Received %f from %s for register %s", amountFloat, origin, registerID)

	//
	// To suggest this, we simulate waiting for a payment completion for a few
	// seconds. In reality this step can take much longer as the buyer completes
	// the terminal instruction, and the amount is sent to the processor for
	// approval.
	// delay := 4 * time.Second
	// log.Printf("Waiting for %d seconds", delay/time.Second)

	// looks up the database to get the fake Oxipay terminal
	// so that we can issue this against Oxipay

	terminal, err := getRegisteredTerminal(origin)

	if err == nil {
		// redirect
		http.Redirect(w, r, "/register", 302)
	}
	log.Printf("Using Oxipay register %s ", terminal.FxlRegisterID)

	txnRef, err := gouuid.NewV4()

	// send off to Oxipay
	//var oxipayPayload
	var oxipayPayload = OxipayPayload{
		DeviceID:          terminal.FxlRegisterID,
		MerchantID:        terminal.FxlSellerID,
		PosTransactionRef: txnRef.String(),
		FinanceAmount:     amount,
		FirmwareVersion:   "vend_integration_v0.0.1",
		OperatorID:        "Vend",
		PurchaseAmount:    amount,
		PreApprovalCode:   code,
	}

	// generate the plaintext for the signature
	plainText := oxipayPayload.generatePayload()
	log.Printf("Oxipay plain text: %s", plainText)

	// sign the message
	oxipayPayload.Signature = SignMessage(plainText, terminal.FxlDeviceSigningKey)
	log.Printf("Oxipay signature: %s", oxipayPayload.Signature)

	// use
	log.Printf("Use the following payload for Oxipay: %v", oxipayPayload)

	// Here is the point where you have all the information you need to send a
	// request to your payment gateway or terminal to process the transaction
	// amount.
	//var gatewayURL = "https://testpos.oxipay.com.au/webapi/v1/"

	oxipayResponse, err := processAuthorisation(oxipayPayload)

	if err != nil {
		http.Error(w, "There was a problem processing the request", 500)
	}

	// @todo this needs to line up better
	var status string
	switch oxipayResponse.Code {
	case "SPRA01":
		status = statusAccepted
	case "CANCEL":
		status = statusCancelled
	case "FPRA99":
		status = statusDeclined
	case "FAIL":
		status = statusFailed
	case "TIMEOUT":
		status = statusTimeout
	default:
		status = statusUnknown
	}

	// Specify an external transaction ID. This value can be sent back to Vend with
	// the "ACCEPT" step as the JSON key "transaction_id".
	shortID, _ := shortid.Generate()

	// Build our response content, including the amount approved and the Vend
	// register that originally sent the payment.
	response := Response{
		ID:         shortID,
		Amount:     amountFloat,
		Status:     status,
		RegisterID: registerID,
	}

	// Marshal our response into JSON.
	responseJSON, err := json.Marshal(response)
	if err != nil {
		log.Println("failed to marshal response json: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Printf("Response: %s", responseJSON)

	// Respond with status created (201), as we are returning a description of
	// the created object, rather than a representation of it.
	// https://tools.ietf.org/html/rfc7231#section-6.3.2
	w.WriteHeader(http.StatusCreated)
	w.Write(responseJSON)
}

func validateRequest() {

}

func getRegisteredTerminal(origin string) (*Terminal, error) {

	sql := `SELECT 
			 fxl_register_id, 
			 fxl_seller_id,
			 fxl_device_signing_key, 
			 origin_domain,
			 vend_register_id
			FROM oxipay_vend_map WHERE origin_domain = ? AND 1=1`

	rows, err := db.Query(sql, origin)

	if err != nil {
		log.Fatal(err)
	}

	var terminal = new(Terminal)
	noRows := 0

	// @todo error if it can't find the terminal
	for rows.Next() {
		noRows++
		var err = rows.Scan(
			&terminal.FxlRegisterID,
			&terminal.FxlSellerID,
			&terminal.FxlDeviceSigningKey,
			&terminal.Origin,
			&terminal.VendRegisterID,
		)
		if err != nil {
			log.Fatal(err)
		}

		// Outputs: map[columnName:value columnName2:value2 columnName3:value3 ...]
		// fmt.Print(terminal)
	}

	if noRows < 1 {
		return nil, errors.New("Unable to find a matching terminal ")
	}

	return terminal, nil
}

func (payload *OxipayPayload) generatePayload() string {

	var buffer bytes.Buffer

	// create a temporary map so we can sort the keys,
	// go intentionally randomises maps so we need to
	// store the keys in an array which we can sort
	v := reflect.TypeOf(payload).Elem()
	x := reflect.ValueOf(payload).Elem()
	fmt.Print(x.NumField())

	payloadList := make(map[string]string, x.NumField())

	for i := 0; i < x.NumField(); i++ {
		field := x.Field(i)
		ftype := v.Field(i)

		data := field.Interface()
		tag := ftype.Tag.Get("json")
		payloadList[tag] = data.(string)

		fmt.Printf("data %v : %v \n\n", tag, data)
	}

	fmt.Print(payloadList)

	var keys []string
	for k := range payloadList {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, v := range keys {
		// there shouldn't be any nil values
		// Signature needs to be populated with the actual HMAC
		// call
		if v != "signature" {
			buffer.WriteString(fmt.Sprintf("%s%s", v, payloadList[v]))
		}
	}

	return buffer.String()
}

// SignMessage will generate an HMAC of the plaintext
func SignMessage(plainText string, signingKey string) string {

	key := []byte(signingKey)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(plainText))

	return hex.EncodeToString(mac.Sum(nil))
}

// func CheckMAC(message, messageMAC, key []byte) bool {
// 	mac := hmac.New(sha256.New, key)
// 	mac.Write(message)

// 	expectedMAC := mac.Sum(nil)
// 	return hmac.Equal(messageMAC, expectedMAC)
// }
