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
	"time"

	colour "github.com/bclicn/color"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"
	//"github.com/gorilla/sessions"
	"github.com/srinathgs/mysqlstore"
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
	ID           string `json:"id"`
	Amount       string `json:"amount"`
	RegisterID   string `json:"register_id"`
	Status       string `json:"status"`
	Signature    string `json:"-"`
	TrackingData string `json:"tracking_data,omitempty"`
	Message      string `json:"message,omitempty"`
	HTTPStatus   int    `json:"-"`
	file         string
}

// DbConnection stores connection information for the database
type DbConnection struct {
	// @todo pull from config
	username string
	password string
	host     string
	name     string
	timeout  time.Duration
}

// @todo load from config
var DbSessionStore *mysqlstore.MySQLStore

// SessionStore store of session data
//var SessionStore *sessions.FilesystemStore

func main() {
	_ = oxipay.Ping()

	connectionParams := &DbConnection{
		username: "root",
		password: "t9e3ioz0",
		host:     "172.18.0.2",
		name:     "vend",
		timeout:  3600,
	}

	db := connectToDatabase(connectionParams)
	terminal.Db = db
	DbSessionStore = initSessionStore(db, "mykey")

	// We are hosting all of the content in ./assets, as the resources are
	// required by the frontend.
	fileServer := http.FileServer(http.Dir("../assets"))
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

	//defer sessionStore.Close()

	log.Fatal(http.ListenAndServe(":"+port, nil))

	// @todo handle shutdowns
}

func initSessionStore(db *sql.DB, secureSessionKey string) *mysqlstore.MySQLStore {

	store, err := mysqlstore.NewMySQLStoreFromConnection(db, "sessions", "/", 3600, []byte("secretkey"))
	store.Options = &sessions.Options{
		Domain:   "",
		Path:     "/",
		MaxAge:   3600, // 8 hours
		HttpOnly: true, // disable for this demo
	}
	if err != nil {
		log.Fatal(err)
	}
	//SessionStore = sessions.NewFilesystemStore("", []byte(secureSessionKey))

	// register the type VendPaymentRequest so that we can use it later in the session
	gob.Register(&vend.PaymentRequest{})
	return store
}

func connectToDatabase(params *DbConnection) *sql.DB {

	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&loc=Local", params.username, params.password, params.host, params.name)

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
		log.Printf(colour.Red("Unable to connect to database: %s on %"), params.name, params.host)
		log.Fatal(err)
	}
	db.SetConnMaxLifetime(params.timeout)
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
			browserResponse = processRegistrationResponse(response, vendPaymentRequest, registrationPayload)

		} else {
			log.Print("Error: " + err.Error())
			browserResponse.Message = "Sorry. We are unable to process this registration. Please contact support"
			browserResponse.HTTPStatus = http.StatusBadRequest
		}
		break
	default:
		browserResponse.HTTPStatus = http.StatusOK
		browserResponse.file = "../assets/templates/register.html"
		break
	}
	log.Print(browserResponse.Message)
	sendResponse(w, r, browserResponse)
	return
}

func processRegistrationResponse(response *oxipay.OxipayResponse, vReq *vend.PaymentRequest, oxipayRegistration *oxipay.OxipayRegistrationPayload) *Response {
	browserResponse := &Response{}
	switch response.Code {
	case "SCRK01":

		//success
		// redirect back to transaction
		log.Print("Device Successfully registered")

		terminal := terminal.NewTerminal(
			response.Key,
			oxipayRegistration.DeviceID,
			oxipayRegistration.MerchantID,
			vReq.Origin,
			vReq.RegisterID,
		)

		_, err := terminal.Save("vend-proxy")
		if err != nil {
			log.Fatal(err)
			browserResponse.Message = "Unable to process request"
			browserResponse.HTTPStatus = http.StatusServiceUnavailable
		} else {
			browserResponse.Message = "Created"
			browserResponse.HTTPStatus = http.StatusOK
		}

		browserResponse.file = "../assets/templates/register_success.html"
		break
	case "FCRK01":
		// can't find this device token
		browserResponse.Message = fmt.Sprintf("Device token %s can't be found in the remote service", oxipayRegistration.DeviceToken)
		browserResponse.HTTPStatus = http.StatusBadRequest
		break
	case "FCRK02":
		// device token already used
		browserResponse.Message = fmt.Sprintf("Device token %s has previously been registered", oxipayRegistration.DeviceID)
		browserResponse.HTTPStatus = http.StatusBadRequest
		break

	case "EISE01":
		browserResponse.Message = fmt.Sprintf("Oxipay Internal Server error for device: %s", oxipayRegistration.DeviceID)
		browserResponse.HTTPStatus = http.StatusBadGateway
		break
	}
	return browserResponse
}

func processPaymentResponse(oxipayResponse *oxipay.OxipayResponse, terminal *terminal.Terminal, oxipayPayload *oxipay.OxipayPayload) *Response {

	// Specify an external transaction ID. This value can be sent back to Vend with
	// the "ACCEPT" step as the JSON key "transaction_id".
	// shortID, _ := shortid.Generate()

	// Build our response content, including the amount approved and the Vend
	// register that originally sent the payment.
	response := &Response{}
	oxipayResponseCode := oxipay.ProcessAuthorisationResponses()(oxipayResponse.Code)

	// if oxipayResponse != nil || oxipayResponseCode.TxnStatus == "" {

	// 	response.Message = "Unable to estabilish communication with Oxipay"
	// 	response.HTTPStatus = http.StatusBadRequest
	// 	return response
	// }

	switch oxipayResponseCode.TxnStatus {
	case oxipay.StatusApproved:
		log.Println(oxipayResponseCode.LogMessage)
		response.Amount = oxipayPayload.PurchaseAmount
		response.ID = oxipayResponse.PurchaseNumber
		response.Status = statusAccepted
		response.HTTPStatus = http.StatusOK
		response.Message = oxipayResponseCode.CustomerMessage
		break
	case oxipay.StatusDeclined:
		response.HTTPStatus = http.StatusOK
		response.ID = ""
		response.Status = statusDeclined
		response.Message = oxipayResponseCode.CustomerMessage
		break

	case oxipay.StatusFailed:
		response.HTTPStatus = http.StatusOK
		response.ID = ""
		response.Status = statusFailed
		response.Message = oxipayResponseCode.CustomerMessage
		break
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

	//logRequest(r)
	var err error

	if err := r.ParseForm(); err != nil {
		log.Fatalf("Error parsing form: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	origin := r.Form.Get("origin")
	origin, _ = url.PathUnescape(origin)

	// @todo new payment request
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

	if err != nil {

		// we don't have a valid terminal
		// save the initial request vars in the session
		// and then redirect to the register their terminal
		//session.Values["registerId"] = vReq.registerID
		//session.Values["origin"] = vReq.origin
		session, err := getSession(r, "oxipay")
		if err != nil {
			log.Fatal(err)
		}

		session.Values["vReq"] = vReq

		err = sessions.Save(r, w)

		if err != nil {
			log.Fatal(err)
		}
		log.Println("Session initiated")

		// redirect
		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}

	http.ServeFile(w, r, "../assets/templates/index.html")
}

func bindToPaymentPayload(r *http.Request) (*vend.PaymentRequest, error) {
	r.ParseForm()
	origin := r.Form.Get("origin")
	origin, _ = url.PathUnescape(origin)

	vReq := &vend.PaymentRequest{
		Amount:     r.Form.Get("amount"),
		Origin:     origin,
		RegisterID: r.Form.Get("register_id"),
		Code:       r.Form.Get("paymentcode"),
	}

	log.Printf("Payment: %s from %s for register %s", vReq.Amount, vReq.Origin, vReq.RegisterID)
	vReq, err := validPaymentRequest(vReq)

	return vReq, err
}

// PaymentHandler receives the payment request from Vend and sends it to the
// payment gateway.
func PaymentHandler(w http.ResponseWriter, r *http.Request) {
	var vReq *vend.PaymentRequest
	var err error

	if vReq, err = bindToPaymentPayload(r); err != nil {
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

	txnRef, err := shortid.Generate()

	// send off to Oxipay
	//var oxipayPayload
	var oxipayPayload = &oxipay.OxipayPayload{
		DeviceID:          terminal.FxlRegisterID,
		MerchantID:        terminal.FxlSellerID,
		PosTransactionRef: txnRef,
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
		log.Printf(colour.Red(msg))
		return
	}

	//
	response := processPaymentResponse(oxipayResponse, terminal, oxipayPayload)

	sendResponse(w, r, response)
	return

}

func sendResponse(w http.ResponseWriter, r *http.Request, response *Response) {

	if len(response.file) > 0 {
		// serve up the success page
		// @todo check file exists
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

	// convert the amount to cents and then go back to a string for
	// the checksum
	if len(req.Amount) < 1 {
		return req, errors.New("Amount is required")
	}
	amountFloat, err := strconv.ParseFloat(req.Amount, 64)
	if err != nil {
		return req, err
	}

	// probably not great that we are mutating the value directly but it's ok
	// for now. If it gets excessive we can return a copy
	req.Amount = strconv.FormatFloat((amountFloat * 100), 'f', 0, 64)
	return req, err
}
