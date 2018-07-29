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

	// "time"s
	_ "crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/gob"
	"sort"

	colour "github.com/bclicn/color"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"

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

// OxipayResponse is the response returned from Oxipay for both a CreateKey and Sales Adjustment
type OxipayResponse struct {
	PurchaseNumber string `json:"x_purchase_number"`
	Status         string `json:"x_status"`
	Code           string `json:"x_code"`
	Message        string `json:"x_message"`
	Key            string `json:"x_key,omitempty"`
	Signature      string `json:"signature"`
}

// Response We build a JSON response object that contains important information for
// which step we should send back to Vend to guide the payment flow.
type Response struct {
	ID           string `json:"id"`
	Amount       string `json:"amount"`
	RegisterID   string `json:"register_id"`
	Status       string `json:"status"`
	Signature    string `json:"signature"`
	TrackingData string `json:"tracking_data,omitempty"`
	Message      string `json:"message,omitempty"`
	HTTPStatus   int    `json:"-"`
}

// OxipayRegistrationPayload required to register a device with Oxipay
type OxipayRegistrationPayload struct {
	MerchantID      string `json:"x_merchant_id"`
	DeviceID        string `json:"x_device_id"`
	DeviceToken     string `json:"x_device_token"`
	OperatorID      string `json:"x_operator_id"`
	FirmwareVersion string `json:"x_firmware_version"`
	POSVendor       string `json:"x_pos_vendor"`
	TrackingData    string `json:"tracking_data,omitempty"`
	Signature       string `json:"signature"`
}

// VendPaymentRequest is the originating request from vend
type VendPaymentRequest struct {
	Amount     string
	Origin     string
	RegisterID string
	code       string
}

// OxpayGateway Default URL for the Oxipay Gateway @todo get from config
var OxpayGateway = "https://testpos.oxipay.com.au/webapi/v1/"

// @todo load from config
/// var SessionStore *mysqlstore.MySQLStore

// SessionStore store of session data
var SessionStore *sessions.FilesystemStore

var db *sql.DB

func main() {

	db = connectToDatabase()

	var err error
	// SessionStore, err := mysqlstore.NewMySQLStoreFromConnection(db, "sessions", "/", 3600, []byte("@todo_change_me"))
	SessionStore = sessions.NewFilesystemStore("", []byte("some key"))

	_ = SessionStore

	// register the type VendPaymentRequest so that we can use it later in the session
	gob.Register(VendPaymentRequest{})

	// We are hosting all of the content in ./assets, as the resources are
	// required by the frontend.
	fileServer := http.FileServer(http.Dir("assets"))
	http.Handle("/assets/", http.StripPrefix("/assets/", fileServer))
	http.HandleFunc("/", Index)
	http.HandleFunc("/pay", PaymentHandler)
	http.HandleFunc("/register", RegisterHandler)

	// The default port is 500, but one can be specified as an env var if needed.
	port := "5000"
	if os.Getenv("PORT") != "" {
		port = os.Getenv("PORT")
	}

	log.Printf("Starting webserver on port %s \n", port)

	if err != nil {
		panic(err)
	}
	//defer sessionStore.Close()

	log.Fatal(http.ListenAndServe(":"+port, nil))

	// @todo handle shutdowns
}

func connectToDatabase() *sql.DB {
	// @todo pull from config
	dbUser := "root"
	dbPassword := "t9e3ioz0"
	host := "172.18.0.2"
	dbName := "vend"
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s", dbUser, dbPassword, host, dbName)

	log.Printf(colour.BLightBlue("Attempting to connect to database %s \n"), dsn)

	// connect to the database
	// @todo grab config

	db, err := sql.Open("mysql", dsn)

	if err != nil {
		log.Printf(colour.Red("Unable to connect"))
		log.Fatal(err)
	}

	// test to make sure it's all good
	if err := db.Ping(); err != nil {
		log.Printf(colour.Red("Unable to connect to %s"), dbName)
		log.Fatal(err)
	}

	return db
}

// RegisterHandler GET request. Prompt for the Merchant ID and Device Token
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	logRequest(r)

	switch r.Method {
	case http.MethodPost:

		registrationPayload, err := bind(r)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
		}

		err = registrationPayload.validate()

		browserResponse := &Response{}

		if err == nil {
			// @todo move to separate function
			plainText := generatePlainTextSignature(registrationPayload)
			log.Printf(colour.BLightYellow("Oxipay Plain Text: %s \n"), plainText)

			// sign the message
			registrationPayload.Signature = SignMessage(plainText, registrationPayload.DeviceToken)
			log.Printf(colour.BLightYellow("Oxipay Signature: %s \n"), registrationPayload.Signature)

			// submit to oxipay
			response, err := registerPosDevice(registrationPayload)

			if err != nil {

				log.Println(err)
				msg := "We are unable to process this request "
				log.Println(msg, err)
				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte(msg))
				return
			}

			switch response.Code {

			case "SCRK01":
				//success
				// redirect back to transaction
				log.Print("Device Successfully registered")
				terminal := &Terminal{
					FxlDeviceSigningKey: response.Key,
					FxlRegisterID:       registrationPayload.DeviceID,
					FxlSellerID:         registrationPayload.MerchantID, // Oxipay Merchant No
					Origin:              r.Form.Get("Origin"),           // Vend Website
					VendRegisterID:      r.Form.Get("VendRegisterID"),   // Vend Register ID
				}

				_, err := terminal.save("vend-proxy")
				if err != nil {
					log.Fatal(err)
					browserResponse.Message = "Unable to process request"
					browserResponse.HTTPStatus = http.StatusServiceUnavailable
				} else {
					browserResponse.Message = "Created"
					browserResponse.HTTPStatus = http.StatusOK
				}

				break
			case "FCRK01":
				// can't find this device token
				browserResponse.Message = fmt.Sprintf("Device token %s can't be found in the remote service", registrationPayload.DeviceToken)
				browserResponse.HTTPStatus = http.StatusBadRequest
				break
			case "FCRK02":
				// device token already used
				browserResponse.Message = fmt.Sprintf("Device token %s has previously been registered", registrationPayload.DeviceToken)
				browserResponse.HTTPStatus = http.StatusBadRequest
				break

			case "EISE01":
				browserResponse.Message = fmt.Sprintf("Oxipay Internal Server error for device: %s", registrationPayload.DeviceToken)
				browserResponse.HTTPStatus = http.StatusBadGateway
				break
			}
		} else {
			browserResponse.Message = err.Error()
			browserResponse.HTTPStatus = http.StatusBadRequest
		}
		log.Print(browserResponse.Message)
		sendResponse(w, browserResponse)

		break

	default:
		http.ServeFile(w, r, "./assets/templates/register.html")
	}
	return
}

// registerPosDevice is used to register a new vend terminal
func registerPosDevice(payload *OxipayRegistrationPayload) (*OxipayResponse, error) {
	// @todo move to configuration
	var registerURL = OxpayGateway + "/CreateKey"
	var err error

	jsonValue, _ := json.Marshal(payload)

	log.Println(colour.BLightPurple("POST to URL %s"), registerURL)
	log.Println(colour.BLightPurple("Register POS Device Payload:" + string(jsonValue)))

	client := http.Client{}
	response, responseErr := client.Post(registerURL, "application/json", bytes.NewBuffer(jsonValue))

	// response, responseErr := client.Do(request)
	if responseErr != nil {
		panic(responseErr)
	}
	defer response.Body.Close()
	log.Println("Register Response Status:", response.Status)
	log.Println("Register Response Headers:", response.Header)
	body, _ := ioutil.ReadAll(response.Body)
	log.Printf(colour.BGreen("ProcessAuthorisation Response Body: \n %v"), string(body))

	// turn {"x_purchase_number":"52011595","x_status":"Success","x_code":"SPRA01","x_message":"Approved","signature":"84b2ed2ec504a0aef134c3da57a060558de1290de7d5055ab8d070dd8354991b","tracking_data":null}
	// into a struct
	oxipayResponse := new(OxipayResponse)
	err = json.Unmarshal(body, oxipayResponse)

	if err != nil {
		log.Println(err)
		return nil, errors.New("Unable to unmarshall response from server")
	}

	log.Printf(colour.BGreen("Unmarshalled Register POS Response Body: %s \n"), oxipayResponse)
	return oxipayResponse, err

}

func bind(r *http.Request) (*OxipayRegistrationPayload, error) {
	r.ParseForm()

	uniqueID, _ := shortid.Generate()
	deviceToken := r.Form.Get("DeviceToken")
	FxlDeviceID := deviceToken + "-" + uniqueID

	// We get the Device Token from the
	session, err := SessionStore.Get(r, "oxipay")
	if err != nil {
		return nil, err
	}
	val := session.Values["vReq"]
	//val := session.Values["origin"]
	//vReq, ok := val.(string)
	var vendPaymentRequest = VendPaymentRequest{}
	vendPaymentRequest, ok := val.(VendPaymentRequest)

	if !ok {
		_ = vendPaymentRequest
		// Handle the case that it's not an expected type
		log.Println(colour.Red("Can't get vReq from session"))

		return nil, err
	}

	log.Printf(colour.BLightBlue("SESSION: %v \n"), ok)
	log.Printf(colour.BLightBlue("SESSION: %v \n"), vendPaymentRequest)

	register := &OxipayRegistrationPayload{
		MerchantID:      r.Form.Get("MerchantID"),
		DeviceID:        FxlDeviceID,
		DeviceToken:     deviceToken,
		OperatorID:      "unknown",
		FirmwareVersion: "version 1.0",
		POSVendor:       "Vend-Proxy",
	}

	return register, nil
}

func (payload *OxipayRegistrationPayload) validate() error {

	return nil
}

func processAuthorisation(oxipayPayload *OxipayPayload) (*OxipayResponse, error) {
	var authorisationURL = OxpayGateway + "/ProcessAuthorisation"

	var err error

	jsonValue, _ := json.Marshal(oxipayPayload)
	log.Println(colour.BLightPurple("POST to URL %s"), authorisationURL)
	log.Println(colour.BLightPurple("Authorisation Payload: " + string(jsonValue)))

	client := http.Client{}
	response, responseErr := client.Post(authorisationURL, "application/json", bytes.NewBuffer(jsonValue))

	// response, responseErr := client.Do(request)
	if responseErr != nil {
		panic(responseErr)
	}
	defer response.Body.Close()
	log.Println("ProcessAuthorisation Response Status:", response.Status)
	log.Println("ProcessAuthorisation Response Headers:", response.Header)

	body, _ := ioutil.ReadAll(response.Body)
	log.Printf(colour.BGreen("ProcessAuthorisation Response Body: \n %v"), string(body))

	// turn {"x_purchase_number":"52011595","x_status":"Success","x_code":"SPRA01","x_message":"Approved","signature":"84b2ed2ec504a0aef134c3da57a060558de1290de7d5055ab8d070dd8354991b","tracking_data":null}
	// into a struct
	oxipayResponse := new(OxipayResponse)
	err = json.Unmarshal(body, oxipayResponse)

	if err != nil {
		return nil, err
	}

	log.Printf("Unmarshalled Oxipay Response Body: %v \n", oxipayResponse)
	return oxipayResponse, err
}

func logRequest(r *http.Request) {

	if r.Body != nil {
		body, _ := ioutil.ReadAll(r.Body)
		log.Printf(colour.GDarkGray("Body: %s \n"), body)
	}
	if r.RequestURI != "" {
		query := r.RequestURI
		log.Println(colour.GDarkGray("Query " + query))
	}

}

// Index displays the main payment processing page, giving the user options of
// which outcome they would like the Pay Example to simulate.
func Index(w http.ResponseWriter, r *http.Request) {

	logRequest(r)

	if SessionStore == nil {
		log.Println(colour.Red("Can't get session"))
		http.Error(w, "Sorry, something went wrong", http.StatusInternalServerError)
		return
	}

	// ensure that we have a

	session, err := SessionStore.Get(r, "oxipay")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	r.ParseForm()
	origin := r.Form.Get("origin")
	origin, _ = url.PathUnescape(origin)

	vReq := &VendPaymentRequest{
		Amount:     r.Form.Get("amount"),
		Origin:     origin,
		RegisterID: r.Form.Get("register_id"),
	}

	log.Printf("Received %s from %s for register %s", vReq.Amount, vReq.Origin, vReq.RegisterID)

	if err := validPaymentRequest(vReq); err != nil {
		w.Write([]byte("Not a valid request"))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// we just want to ensure there is a terminal available
	_, err = getRegisteredTerminal(vReq)

	if err != nil {
		// we don't have a valid terminal
		// save the initial request vars in the session
		// and then redirect to the register their terminal
		//session.Values["registerId"] = vReq.registerID
		//session.Values["origin"] = vReq.origin
		session.Values["vReq"] = vReq

		err = session.Save(r, w)

		if err != nil {
			log.Fatal(err)
		}
		log.Println("Session initiated")

		// redirect
		http.Redirect(w, r, "/register", 302)
		return
	}

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
	origin := r.Form.Get("origin")
	origin, _ = url.PathUnescape(origin)

	vReq := &VendPaymentRequest{
		Amount:     r.Form.Get("amount"),
		Origin:     origin,
		RegisterID: r.Form.Get("register_id"),
		code:       r.Form.Get("paymentcode"),
	}

	log.Printf("Received %s from %s for register %s", vReq.Amount, vReq.Origin, vReq.RegisterID)

	if err := validPaymentRequest(vReq); err != nil {
		w.Write([]byte("Not a valid request"))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	//
	// To suggest this, we simulate waiting for a payment completion for a few
	// seconds. In reality this step can take much longer as the buyer completes
	// the terminal instruction, and the amount is sent to the processor for
	// approval.
	// delay := 4 * time.Second
	// log.Printf("Waiting for %d seconds", delay/time.Second)

	// looks up the database to get the fake Oxipay terminal
	// so that we can issue this against Oxipay

	terminal, err := getRegisteredTerminal(vReq)

	if err != nil {
		// redirect
		http.Redirect(w, r, "/register", 302)
		return
	}
	log.Printf("Using Oxipay register %s ", terminal.FxlRegisterID)

	txnRef, err := shortid.Generate()

	// send off to Oxipay
	//var oxipayPayload
	var oxipayPayload = &OxipayPayload{
		DeviceID:          terminal.FxlRegisterID,
		MerchantID:        terminal.FxlSellerID,
		PosTransactionRef: txnRef,
		FinanceAmount:     vReq.Amount,
		FirmwareVersion:   "vend_integration_v0.0.1",
		OperatorID:        "Vend",
		PurchaseAmount:    vReq.Amount,
		PreApprovalCode:   vReq.code,
	}

	// generate the plaintext for the signature
	plainText := generatePlainTextSignature(oxipayPayload)
	log.Printf("Oxipay plain text: %s \n", plainText)

	// sign the message
	oxipayPayload.Signature = SignMessage(plainText, terminal.FxlDeviceSigningKey)
	log.Printf("Oxipay signature: %s \n", oxipayPayload.Signature)

	// use
	log.Printf("Use the following payload for Oxipay: %v \n", oxipayPayload)

	// Here is the point where you have all the information you need to send a
	// request to your payment gateway or terminal to process the transaction
	// amount.
	//var gatewayURL = "https://testpos.oxipay.com.au/webapi/v1/"

	oxipayResponse, err := processAuthorisation(oxipayPayload)

	if err != nil {
		http.Error(w, "There was a problem processing the request", 500)
		// log the raw response
		msg := fmt.Sprintf("Error Processing: %s", oxipayResponse)
		log.Printf(colour.Red(msg))
		return
	}

	// Specify an external transaction ID. This value can be sent back to Vend with
	// the "ACCEPT" step as the JSON key "transaction_id".
	// shortID, _ := shortid.Generate()

	// Build our response content, including the amount approved and the Vend
	// register that originally sent the payment.
	response := &Response{}

	switch oxipayResponse.Code {
	case "SPRA01":
		response.Amount = oxipayPayload.PurchaseAmount
		response.ID = oxipayResponse.PurchaseNumber
		response.Status = statusAccepted
		response.HTTPStatus = http.StatusOK
	case "CANCEL":
		response.Status = statusCancelled
		response.HTTPStatus = http.StatusOK
	case "FPRA99":
		response.Status = statusDeclined
		response.HTTPStatus = http.StatusOK
	case "EAUT01":
		// tried to process with an unknown terminal
		// needs registration
		// should we remove from mapping ? then redirect ?
		// need to think this through as we need to authenticate them first otherwise you
		// can remove other peoples transactions
		response.Status = statusFailed
		response.HTTPStatus = http.StatusOK
	case "FAIL":
		response.Status = statusFailed
		response.HTTPStatus = http.StatusOK
	case "TIMEOUT":
		response.Status = statusTimeout
		response.HTTPStatus = http.StatusOK
	default:
		response.Status = statusUnknown
		response.HTTPStatus = http.StatusOK
	}

	sendResponse(w, response)
	return

}

func newNullString(s string) sql.NullString {
	if len(s) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{
		String: s,
		Valid:  true,
	}
}

func (t Terminal) save(user string) (bool, error) {

	if db == nil {
		return false, errors.New("I have no database connection")
	}

	query := `INSERT INTO 
		oxipay_vend_map  
		(
			fxl_register_id,
			fxl_seller_id,
			fxl_device_signing_key,
			origin_domain, 
			vend_register_id,
			created_by
		) VALUES (?, ?, ?, ?, ?, ?) `

	stmt, err := db.Prepare(query)

	if err != nil {
		return false, err
	}

	defer stmt.Close()

	_, err = stmt.Exec(
		newNullString(t.FxlRegisterID),
		newNullString(t.FxlSellerID),
		newNullString(t.FxlDeviceSigningKey),
		newNullString(t.Origin),
		newNullString(t.VendRegisterID),
		newNullString(user),
	)

	if err != nil {
		return false, err
	}

	return true, nil

}

func getRegisteredTerminal(r *VendPaymentRequest) (*Terminal, error) {

	if db == nil {
		return nil, errors.New("I have no database connection")
	}

	sql := `SELECT 
			 fxl_register_id, 
			 fxl_seller_id,
			 fxl_device_signing_key, 
			 origin_domain,
			 vend_register_id
			FROM 
				oxipay_vend_map 
			WHERE 
				origin_domain = ? 
			AND
				vend_register_id = ? 
			AND 1=1`

	rows, err := db.Query(sql, r.Origin, r.RegisterID)

	if err != nil {
		log.Fatal(err)
	}

	var terminal = new(Terminal)
	noRows := 0

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
	}

	if noRows < 1 {
		return nil, errors.New("Unable to find a matching terminal ")
	}

	return terminal, nil
}

func sendResponse(w http.ResponseWriter, response *Response) {

	// Marshal our response into JSON.
	responseJSON, err := json.Marshal(response)
	if err != nil {
		log.Println("failed to marshal response json: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Printf("Response: %s \n", responseJSON)

	if response.HTTPStatus == 0 {
		response.HTTPStatus = 500
	}
	w.WriteHeader(response.HTTPStatus)
	w.Write(responseJSON)
	return
}

//
func generatePlainTextSignature(payload interface{}) string {

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

	}
	var keys []string
	for k := range payloadList {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, v := range keys {
		// there shouldn't be any nil values
		// Signature needs to be populated with the actual HMAC
		// call
		if v[0:2] == "x_" {
			buffer.WriteString(fmt.Sprintf("%s%s", v, payloadList[v]))
		}
	}

	return buffer.String()
}

func validPaymentRequest(req *VendPaymentRequest) error {

	return nil
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
