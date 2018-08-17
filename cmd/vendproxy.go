package main

import (
	_ "crypto/hmac"
	"database/sql"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	colour "github.com/bclicn/color"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"

	//"github.com/gorilla/sessions"
	"github.com/srinathgs/mysqlstore"
	"github.com/vend/peg/internal/pkg/config"
	"github.com/vend/peg/internal/pkg/oxipay"
	"github.com/vend/peg/internal/pkg/terminal"
	"github.com/vend/peg/internal/pkg/vend"

	shortid "github.com/ventu-io/go-shortid"
)

// These are the possible sale statuses. // @todo move to vend
const (
	statusAccepted  = "ACCEPTED"
	statusCancelled = "CANCELLED"
	statusDeclined  = "DECLINED"
	statusFailed    = "FAILED"
	statusTimeout   = "TIMEOUT"
	statusUnknown   = "UNKNOWN"
)

// Response We build a JSON response object that contains important information for
// which step we should send back to Vend to guide the payment flow.
type Response struct {
	ID           string `json:"id,omitempty"`
	Amount       string `json:"amount"`
	RegisterID   string `json:"register_id"`
	Status       string `json:"status"`
	Signature    string `json:"-"`
	TrackingData string `json:"tracking_data,omitempty"`
	Message      string `json:"message,omitempty"`
	HTTPStatus   int    `json:"-"`
	file         string
}

// DbSessionStore is the database session storage manager
var DbSessionStore *mysqlstore.MySQLStore

// SessionStore store of session data
//var SessionStore *sessions.FilesystemStore

func main() {
	// default configuration file for prod
	configurationFile := "/etc/vendproxy/vendproxy.json"
	if os.Getenv("DEV") != "" {
		// default configuration file for dev
		configurationFile = "../configs/vendproxy.json"
	}

	// load config
	appConfig, err := config.ReadApplicationConfig(configurationFile)

	// configure Oxipay Module
	oxipay.GatewayURL = appConfig.Oxipay.GatewayURL

	if err != nil {
		log.Fatalf("Configuration Error: %s ", err)
	}

	db := connectToDatabase(appConfig.Database)
	terminal.Db = db
	DbSessionStore = initSessionStore(db, appConfig.Session)

	// We are hosting all of the content in ./assets, as the resources are
	// required by the frontend.
	fileServer := http.FileServer(http.Dir("../assets"))
	http.Handle("/assets/", http.StripPrefix("/assets/", fileServer))
	http.HandleFunc("/", Index)
	http.HandleFunc("/pay", PaymentHandler)
	http.HandleFunc("/register", RegisterHandler)
	http.HandleFunc("/refund", RefundHandler)

	// The default port is 500, but one can be specified as an env var if needed.
	port := strconv.FormatInt(int64(appConfig.Webserver.Port), 10)

	log.Printf("Starting webserver on port %s \n", port)

	//defer sessionStore.Close()
	log.Fatal(http.ListenAndServe(":"+port, nil))

	// @todo handle shutdowns
}

func initSessionStore(db *sql.DB, sessionConfig config.SessionConfig) *mysqlstore.MySQLStore {

	// @todo support multiple keys from the config so that key rotation is possible
	store, err := mysqlstore.NewMySQLStoreFromConnection(db, "sessions", "/", 3600, []byte(sessionConfig.Secret))
	if err != nil {
		log.Fatal(err)
	}

	store.Options = &sessions.Options{
		Domain:   sessionConfig.Domain,
		Path:     sessionConfig.Path,
		MaxAge:   sessionConfig.MaxAge,   // 8 hours
		HttpOnly: sessionConfig.HTTPOnly, // disable for this demo
	}

	//SessionStore = sessions.NewFilesystemStore("", []byte(secureSessionKey))

	// register the type VendPaymentRequest so that we can use it later in the session
	gob.Register(&vend.PaymentRequest{})
	return store
}

func connectToDatabase(params config.DbConnection) *sql.DB {

	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&loc=Local", params.Username, params.Password, params.Host, params.Name)

	log.Printf("Attempting to connect to database %s \n", dsn)

	// connect to the database
	// @todo grab config

	db, err := sql.Open("mysql", dsn)

	if err != nil {
		log.Printf("Unable to connect")
		log.Fatal(err)
	}

	// test to make sure it's all good
	if err := db.Ping(); err != nil {
		log.Printf("Unable to connect to database: %s on %s", params.Name, params.Host)
		log.Fatal(err)
	}
	db.SetConnMaxLifetime(time.Duration(params.Timeout))
	//log.Print("Db Connection Timeout set to : %", db.MaxLifetime)
	return db
}

func getPaymentRequestFromSession(r *http.Request) (*vend.PaymentRequest, error) {
	var err error
	var session *sessions.Session

	vendPaymentRequest := &vend.PaymentRequest{}
	session, err = getSession(r, "oxipay")
	if err != nil {
		log.Println(err.Error())
		_ = session
		return nil, err
	}
	// get the vendRequest from the session
	vReq := session.Values["vReq"]
	vendPaymentRequest, ok := vReq.(*vend.PaymentRequest)

	if !ok {
		msg := "Can't get vRequest from session"
		log.Println(msg)
		return nil, errors.New(msg)
	}
	return vendPaymentRequest, err
}

func getSession(r *http.Request, sessionName string) (*sessions.Session, error) {
	if DbSessionStore == nil {
		log.Println(colour.Red("Can't get session store"))
		return nil, errors.New("Can't get session store")
	}

	// ensure that we have a session
	session, err := DbSessionStore.Get(r, sessionName)
	if err != nil {
		log.Println(err)
		return session, err
	}
	return session, nil
}

// RegisterHandler GET request. Prompt for the Merchant ID and Device Token
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	// logRequest(r)
	browserResponse := &Response{}
	switch r.Method {
	case http.MethodPost:

		// Bind the request from the browser to an Oxipay Registration Payload
		registrationPayload, err := bindToRegistrationPayload(r)

		if err != nil {
			browserResponse.HTTPStatus = http.StatusBadRequest
			browserResponse.Message = err.Error()
			sendResponse(w, r, browserResponse)
		}

		err = registrationPayload.Validate()
		if err != nil {
			browserResponse.HTTPStatus = http.StatusBadRequest
			browserResponse.Message = err.Error()
			sendResponse(w, r, browserResponse)
			return
		}

		vendPaymentRequest, err := getPaymentRequestFromSession(r)

		if err == nil {

			// sign the message
			registrationPayload.Signature = oxipay.SignMessage(oxipay.GeneratePlainTextSignature(registrationPayload), registrationPayload.DeviceToken)

			// submit to oxipay
			response, err := oxipay.RegisterPosDevice(registrationPayload)

			if err != nil {
				log.Println(err)
				browserResponse.Message = "We are unable to process this request "
				browserResponse.HTTPStatus = http.StatusBadGateway
			}

			// ensure the response came from Oxipay
			if !response.Authenticate(registrationPayload.DeviceToken) {
				browserResponse.Message = "The signature returned from Oxipay does not match the expected signature"
				browserResponse.HTTPStatus = http.StatusBadRequest
			} else {
				// process the response
				browserResponse = processOxipayResponse(response, oxipay.Registration, "")
				if browserResponse.Status == statusAccepted {
					log.Print("Device Successfully Registered in Oxipay")

					terminal := terminal.NewTerminal(
						response.Key,
						registrationPayload.DeviceID,
						registrationPayload.MerchantID,
						vendPaymentRequest.Origin,
						vendPaymentRequest.RegisterID,
					)

					_, err := terminal.Save("vend-proxy")
					if err != nil {
						log.Print(err)
						browserResponse.Message = "Unable to process request"
						browserResponse.HTTPStatus = http.StatusServiceUnavailable

					} else {
						browserResponse.file = "../assets/templates/register_success.html"
					}
				}
			}
		} else {
			log.Print("Error: " + err.Error())
			browserResponse.Message = "Sorry. We are unable to process this registration. Please contact support"
			browserResponse.HTTPStatus = http.StatusBadRequest
		}
	default:
		browserResponse.HTTPStatus = http.StatusOK
		browserResponse.file = "../assets/templates/register.html"
	}

	log.Print(browserResponse.Message)
	sendResponse(w, r, browserResponse)
	return
}

func processOxipayResponse(oxipayResponse *oxipay.OxipayResponse, responseType oxipay.ResponseType, amount string) *Response {

	// Specify an external transaction ID. This value can be sent back to Vend with
	// the "ACCEPT" step as the JSON key "transaction_id".
	// shortID, _ := shortid.Generate()

	// Build our response content, including the amount approved and the Vend
	// register that originally sent the payment.
	response := &Response{}

	var oxipayResponseCode *oxipay.ResponseCode
	switch responseType {
	case oxipay.Authorisation:
		oxipayResponseCode = oxipay.ProcessAuthorisationResponses()(oxipayResponse.Code)
	case oxipay.Adjustment:
		oxipayResponseCode = oxipay.ProcessSalesAdjustmentResponse()(oxipayResponse.Code)
	case oxipay.Registration:
		oxipayResponseCode = oxipay.ProcessRegistrationResponse()(oxipayResponse.Code)
	}

	if oxipayResponseCode == nil || oxipayResponseCode.TxnStatus == "" {

		response.Message = "Unable to estabilish communication with Oxipay"
		response.HTTPStatus = http.StatusBadRequest
		return response
	}

	switch oxipayResponseCode.TxnStatus {
	case oxipay.StatusApproved:
		log.Println(oxipayResponseCode.LogMessage)
		response.Amount = amount
		response.ID = oxipayResponse.PurchaseNumber
		response.Status = statusAccepted
		response.HTTPStatus = http.StatusOK
		response.Message = oxipayResponseCode.CustomerMessage
	case oxipay.StatusDeclined:
		response.HTTPStatus = http.StatusOK
		response.ID = ""
		response.Status = statusDeclined
		response.Message = oxipayResponseCode.CustomerMessage
	case oxipay.StatusFailed:
		response.HTTPStatus = http.StatusOK
		response.ID = ""
		response.Status = statusFailed
		response.Message = oxipayResponseCode.CustomerMessage
	default:
		// default to fail...not sure if this is right
		response.HTTPStatus = http.StatusOK
		response.ID = ""
		response.Status = statusFailed
		response.Message = oxipayResponseCode.CustomerMessage
	}
	return response
}

func bindToRegistrationPayload(r *http.Request) (*oxipay.OxipayRegistrationPayload, error) {

	if err := r.ParseForm(); err != nil {
		log.Fatalf("Error parsing form: %s", err)
		return nil, err
	}
	uniqueID, _ := shortid.Generate()
	deviceToken := r.Form.Get("DeviceToken")
	merchantID := r.Form.Get("MerchantID")
	FxlDeviceID := deviceToken + "-" + uniqueID

	register := &oxipay.OxipayRegistrationPayload{
		MerchantID:      merchantID,
		DeviceID:        FxlDeviceID,
		DeviceToken:     deviceToken,
		OperatorID:      "unknown",
		FirmwareVersion: "version " + oxipay.Version,
		POSVendor:       "Vend-Proxy",
	}

	return register, nil
}

func logRequest(r *http.Request) {
	dump, _ := httputil.DumpRequest(r, true)
	log.Printf("%q ", dump)
	// if r.Body != nil {
	// 	// we need to copy the bytes out of the buffer
	// 	// we we can inspect the contents without draining the buffer

	// 	body, _ := ioutil.ReadAll(r.Body)
	// 	log.Printf(colour.GDarkGray("Body: %s \n"), body)
	// }
	if r.RequestURI != "" {
		query := r.RequestURI
		log.Println(colour.GDarkGray("Query " + query))
	}

}

// Index displays the main payment processing page, giving the user options of
// which outcome they would like the Pay Example to simulate.
func Index(w http.ResponseWriter, r *http.Request) {

	logRequest(r)
	var err error

	if err := r.ParseForm(); err != nil {
		log.Fatalf("Error parsing form: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	origin := r.Form.Get("origin")
	origin, _ = url.PathUnescape(origin)

	// @todo add NewPaymentRequest method
	vReq := &vend.PaymentRequest{
		Amount:     r.Form.Get("amount"),
		Origin:     origin,
		RegisterID: r.Form.Get("register_id"),
	}

	log.Printf("Received %s from %s for register %s", vReq.Amount, vReq.Origin, vReq.RegisterID)
	vReq, err = validPaymentRequest(vReq)

	if err != nil {
		w.Write([]byte("Not a valid request"))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// we just want to ensure there is a terminal available
	_, err = terminal.GetRegisteredTerminal(vReq.Origin, vReq.RegisterID)

	// register the device if needed
	if err != nil {

		saveToSession(w, r, vReq)

		// redirect
		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}

	// refunds are triggered by a negative amount
	if vReq.AmountFloat > 0 {
		// payment
		http.ServeFile(w, r, "../assets/templates/index.html")
	} else {
		// save the details of the original request
		saveToSession(w, r, vReq)

		// refund
		http.ServeFile(w, r, "../assets/templates/refund.html")
	}
}

func saveToSession(w http.ResponseWriter, r *http.Request, vReq *vend.PaymentRequest) {
	// we don't have a valid terminal
	// save the initial request vars in the session
	// and then redirect to the register their terminal
	//session.Values["registerId"] = vReq.registerID
	//session.Values["origin"] = vReq.origin
	session, err := getSession(r, "oxipay")
	if err != nil {
		log.Println(err)
	}

	session.Values["vReq"] = vReq
	err = sessions.Save(r, w)

	if err != nil {
		log.Println(err)
	}
	log.Println("Session initiated")
}

func bindToPaymentPayload(r *http.Request) (*vend.PaymentRequest, error) {
	r.ParseForm()
	origin := r.Form.Get("origin")
	origin, _ = url.PathUnescape(origin)

	vReq := &vend.PaymentRequest{
		Amount:     r.Form.Get("amount"),
		Origin:     origin,
		SaleID:     strings.Trim(r.Form.Get("sale_id"), ""),
		RegisterID: r.Form.Get("register_id"),
		Code:       strings.Trim(r.Form.Get("paymentcode"), ""),
	}

	log.Printf("Payment: %s from %s for register %s", vReq.Amount, vReq.Origin, vReq.RegisterID)
	vReq, err := validPaymentRequest(vReq)

	return vReq, err
}

// RefundHandler handles performing a refund
func RefundHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	browserResponse := new(Response)
	var err error

	// vReq, err = bindToRefundPayload(r)
	x, err := getPaymentRequestFromSession(r)

	if err != nil {
		log.Print(err)
		http.Error(w, "There was a problem processing the request", http.StatusBadRequest)
		return
	}

	vReq := vend.RefundRequest{
		Amount:         x.Amount,
		Origin:         x.Origin,
		SaleID:         strings.Trim(r.Form.Get("sale_id"), ""),
		PurchaseNumber: strings.Trim(r.Form.Get("purchaseno"), ""),
		RegisterID:     x.RegisterID,
		AmountFloat:    x.AmountFloat,
	}

	terminal, err := terminal.GetRegisteredTerminal(vReq.Origin, vReq.RegisterID)
	if err != nil {
		// redirect to registration page
		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}

	txnRef, err := shortid.Generate()
	var oxipayPayload = &oxipay.OxipaySalesAdjustmentPayload{
		Amount:            strings.Replace(vReq.Amount, "-", "", 1),
		MerchantID:        terminal.FxlSellerID,
		DeviceID:          terminal.FxlRegisterID,
		FirmwareVersion:   "vend_integration_v0.0.1",
		OperatorID:        "Vend",
		PurchaseRef:       vReq.PurchaseNumber,
		PosTransactionRef: txnRef, // @todo see if vend has a uniqueID for this also
	}

	// generate the plaintext for the signature
	plainText := oxipay.GeneratePlainTextSignature(oxipayPayload)
	log.Printf("Oxipay plain text: %s \n", plainText)

	// sign the message
	oxipayPayload.Signature = oxipay.SignMessage(plainText, terminal.FxlDeviceSigningKey)
	log.Printf("Oxipay signature: %s \n", oxipayPayload.Signature)

	// send authorisation to the Oxipay POS API
	oxipayResponse, err := oxipay.ProcessSalesAdjustment(oxipayPayload)

	if err != nil {
		http.Error(w, "There was a problem processing the request", http.StatusInternalServerError)
		// log the raw response
		msg := fmt.Sprintf("Error Processing: %s", oxipayResponse)
		log.Printf(msg)
		return
	}

	// ensure the response has come from Oxipay
	if !oxipayResponse.Authenticate(terminal.FxlDeviceSigningKey) {
		browserResponse.Message = "The signature does not match the expected signature"
		browserResponse.HTTPStatus = http.StatusBadRequest
	} else {
		// Return a response to the browser bases on the response from Oxipay
		browserResponse = processOxipayResponse(oxipayResponse, oxipay.Adjustment, oxipayPayload.Amount)
	}

	sendResponse(w, r, browserResponse)
	return
}

// PaymentHandler receives the payment request from Vend and sends it to the
// payment gateway.
func PaymentHandler(w http.ResponseWriter, r *http.Request) {
	var vReq *vend.PaymentRequest
	var err error
	browserResponse := new(Response)

	//logRequest(r)

	vReq, err = bindToPaymentPayload(r)
	if err != nil {
		log.Print(err)
		http.Error(w, "There was a problem processing the request", http.StatusBadRequest)
	}

	// looks up the database to get the fake Oxipay terminal
	// so that we can issue this against Oxipay
	// if the seller has correctly configured the gateway they will not hit this
	// directly but it's here as safeguard

	terminal, err := terminal.GetRegisteredTerminal(vReq.Origin, vReq.RegisterID)

	if err != nil {
		// redirect
		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}
	log.Printf("Processing Payment using Oxipay register %s ", terminal.FxlRegisterID)

	// txnRef, err := shortid.Generate()

	// send off to Oxipay
	//var oxipayPayload
	var oxipayPayload = &oxipay.OxipayPayload{
		DeviceID:          terminal.FxlRegisterID,
		MerchantID:        terminal.FxlSellerID,
		PosTransactionRef: vReq.SaleID,
		FinanceAmount:     vReq.Amount,
		FirmwareVersion:   "vend_integration_v0.0.1",
		OperatorID:        "Vend",
		PurchaseAmount:    vReq.Amount,
		PreApprovalCode:   vReq.Code,
	}

	// generate the plaintext for the signature
	plainText := oxipay.GeneratePlainTextSignature(oxipayPayload)
	log.Printf("Oxipay plain text: %s \n", plainText)

	// sign the message
	oxipayPayload.Signature = oxipay.SignMessage(plainText, terminal.FxlDeviceSigningKey)
	log.Printf("Oxipay signature: %s \n", oxipayPayload.Signature)

	// send authorisation to the Oxipay POS API
	oxipayResponse, err := oxipay.ProcessAuthorisation(oxipayPayload)

	if err != nil {
		http.Error(w, "There was a problem processing the request", http.StatusInternalServerError)
		// log the raw response
		msg := fmt.Sprintf("Error Processing: %s", oxipayResponse)
		log.Printf(msg)
		return
	}

	// ensure the response has come from Oxipay
	if !oxipayResponse.Authenticate(terminal.FxlDeviceSigningKey) {
		browserResponse.Message = "The signature does not match the expected signature"
		browserResponse.HTTPStatus = http.StatusBadRequest
	} else {
		// Return a response to the browser bases on the response from Oxipay
		browserResponse = processOxipayResponse(oxipayResponse, oxipay.Authorisation, oxipayPayload.PurchaseAmount)
	}

	sendResponse(w, r, browserResponse)

	return
}

func sendResponse(w http.ResponseWriter, r *http.Request, response *Response) {

	if len(response.file) > 0 {
		// serve up the success page
		// @todo check file exists
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.ServeFile(w, r, response.file)

		return
	}

	// Marshal our response into JSON.
	responseJSON, err := json.Marshal(response)
	if err != nil {
		log.Println("failed to marshal response json: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Printf("Response: %s \n", responseJSON)

	if response.HTTPStatus == 0 {
		response.HTTPStatus = http.StatusInternalServerError
	}
	w.WriteHeader(response.HTTPStatus)
	w.Write(responseJSON)

	return
}

func validPaymentRequest(req *vend.PaymentRequest) (*vend.PaymentRequest, error) {
	var err error

	// convert the amount to cents and then go back to a string for
	// the checksum
	if len(req.Amount) < 1 {
		return req, errors.New("Amount is required")
	}
	req.AmountFloat, err = strconv.ParseFloat(req.Amount, 64)
	if err != nil {
		return req, err
	}

	// Oxipay deals with cents.
	// Probably not great that we are mutating the value directly
	// If it gets problematic we can return a copy
	req.Amount = strconv.FormatFloat((req.AmountFloat * 100), 'f', 0, 64)
	return req, err
}
