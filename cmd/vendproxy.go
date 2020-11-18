package main

import (
	_ "crypto/hmac"
	"database/sql"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"
	"github.com/oxipay/oxipay-vend/internal/pkg/config"
	"github.com/oxipay/oxipay-vend/internal/pkg/oxipay"
	"github.com/oxipay/oxipay-vend/internal/pkg/terminal"
	"github.com/oxipay/oxipay-vend/internal/pkg/vend"
	logrus "github.com/sirupsen/logrus"
	"github.com/srinathgs/mysqlstore"
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

var logger *logrus.Logger

// var appConfig *config.HostConfig

var oxipayClient oxipay.Client

var db *sql.DB

var term *terminal.Terminal

func main() {
	// default configuration file for prod
	configurationFile := "/etc/vendproxy/vendproxy.json"
	if os.Getenv("DEV") != "" {
		// default configuration file for dev
		configurationFile = "../configs/vendproxy.json"
	}

	// load config
	appConfig, err := config.ReadApplicationConfig(configurationFile)
	if err != nil {
		logrus.Fatal(err)
	}

	// init our logging framework
	level, err := logrus.ParseLevel(appConfig.LogLevel)
	if err != nil {
		logrus.Fatalf("Level %s is not a valid log level. Try setting 'info' in production ", level)
	}

	logger = initLogger(level)
	if err != nil {
		logger.Fatalf("Configuration Error: %s ", err)
	}

	db = connectToDatabase(appConfig.Database)

	DbSessionStore = initSessionStore(db, appConfig.Session)

	// create a reference to the Oxipay Client
	oxipayClient = oxipay.NewOxipay(
		appConfig.Oxipay.GatewayURL,
		appConfig.Oxipay.Version,
		logger,
	)

	term = terminal.NewTerminal(db)

	// We are hosting all of the content in ./assets, as the resources are
	// required by the frontend.
	fileServer := http.FileServer(http.Dir("../assets"))
	http.Handle("/assets/", http.StripPrefix("/assets/", fileServer))
	http.HandleFunc("/", Index)
	http.HandleFunc("/pay", PaymentHandler)
	http.HandleFunc("/register", RegisterHandler)
	http.HandleFunc("/refund", RefundHandler)

	// The default port is 500, but one can be specified as an env var if needed.
	wsConfig := appConfig.Webserver

	logger.Infof("Starting webserver on port %s \n", wsConfig.Port)

	if len(wsConfig.TLSCert) > 0 && len(wsConfig.TLSKey) > 0 {
		logger.Fatal(http.ListenAndServeTLS(":"+wsConfig.Port, wsConfig.TLSCert, wsConfig.TLSKey, nil))
	} else {
		// start up an http listener
		logger.Fatal(http.ListenAndServe(":"+wsConfig.Port, nil))
	}

	// @todo handle shutdowns
}

func initLogger(logLevel logrus.Level) *logrus.Logger {

	logger := logrus.New()
	logger.Formatter = &logrus.JSONFormatter{}

	logger.SetOutput(os.Stdout)

	// Only log the warning severity or above.
	logger.SetLevel(logLevel)

	return logger
}

func initSessionStore(db *sql.DB, sessionConfig config.SessionConfig) *mysqlstore.MySQLStore {

	// @todo support multiple keys from the config so that key rotation is possible
	store, err := mysqlstore.NewMySQLStoreFromConnection(db, "sessions", "/", 3600, []byte(sessionConfig.Secret))
	if err != nil {
		logger.Warn(err)
	}

	store.Options = &sessions.Options{
		Domain:   sessionConfig.Domain,
		Path:     sessionConfig.Path,
		MaxAge:   sessionConfig.MaxAge,   // 8 hours
		HttpOnly: sessionConfig.HTTPOnly, // disable for this demo
		Secure:   true,
		SameSite: 4,
	}

	// register the type VendPaymentRequest so that we can use it later in the session
	gob.Register(&vend.PaymentRequest{})
	return store
}

func connectToDatabase(params config.DbConnection) *sql.DB {

	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&loc=Local&timeout=%s",
		params.Username,
		params.Password,
		params.Host,
		params.Name,
		params.Timeout,
	)

	logger.Infof("Attempting to connect to database %s\n", dsn)

	// connect to the database
	// @todo grab config

	db, err := sql.Open("mysql", dsn)

	if err != nil {
		logger.Error("Unable to connect")
		logger.Fatal(err)
	}

	err = retry(30, time.Duration(10), db.Ping)

	// test to make sure it's all good
	if err != nil {
		logger.Errorf("Unable to connect to database: %s on %s", params.Name, params.Host)
		logger.Warn(err)
	}

	logger.Info("Database Connected")

	return db
}

func retry(attempts int, sleep time.Duration, f func() error) error {
	logger.Info("Attempting DB connection")
	if err := f(); err != nil {
		logger.Warn(err)
		if attempts--; attempts > 0 {

			jitter := time.Duration(rand.Int63n(int64(sleep)))
			sleep = sleep + jitter/2

			time.Sleep(sleep)

			logger.Warning("Unsuccessful Retrying")
			return retry(attempts, 2*sleep, f)
		}
		return err
	}

	return nil
}

func getPaymentRequestFromSession(r *http.Request) (*vend.PaymentRequest, error) {
	var err error
	var session *sessions.Session

	vendPaymentRequest := &vend.PaymentRequest{}
	session, err = getSession(r, "oxipay")
	if err != nil {
		logger.Infoln(err.Error())
		_ = session
		return nil, err
	}

	// get the vendRequest from the session
	vReq := session.Values["vReq"]
	vendPaymentRequest, ok := vReq.(*vend.PaymentRequest)

	if !ok {
		msg := "Can't get vRequest from session"
		return nil, errors.New(msg)
	}
	return vendPaymentRequest, err
}

func getSession(r *http.Request, sessionName string) (*sessions.Session, error) {
	if DbSessionStore == nil {
		logger.Error("Can't get session store")
		return nil, errors.New("Can't get session store")
	}

	// ensure that we have a session
	session, err := DbSessionStore.Get(r, sessionName)
	if err != nil {
		return session, err
	}
	return session, nil
}

// RegisterHandler GET request. Prompt for the Merchant ID and Device Token
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
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
			response, err := oxipayClient.RegisterPosDevice(registrationPayload)

			if err != nil {
				logger.Error(err)
				browserResponse.Message = "We are unable to process this request "
				browserResponse.HTTPStatus = http.StatusBadGateway
			}

			// ensure the response came from Oxipay
			signedResponse, err := response.Authenticate(registrationPayload.DeviceToken)
			if !signedResponse || err != nil {
				browserResponse.Message = "The signature returned from Oxipay does not match the expected signature"
				browserResponse.HTTPStatus = http.StatusBadRequest
			} else {
				// process the response
				browserResponse = processOxipayResponse(response, oxipay.Registration, "")
				if browserResponse.Status == statusAccepted {
					logger.Info("Device Successfully Registered in Oxipay")

					register := terminal.NewRegister(
						response.Key,
						registrationPayload.DeviceID,
						registrationPayload.MerchantID,
						vendPaymentRequest.Origin,
						vendPaymentRequest.RegisterID,
					)

					_, err := term.Save("vend-proxy", register)
					if err != nil {
						logger.Error(err)
						browserResponse.Message = "Unable to process request"
						browserResponse.HTTPStatus = http.StatusServiceUnavailable

					} else {
						browserResponse.file = "../assets/templates/register_success.html"
					}
				}
			}
		} else {
			logger.Error(err.Error())
			browserResponse.Message = "Sorry. We are unable to process this registration. Please contact support"
			browserResponse.HTTPStatus = http.StatusBadRequest
		}
	default:
		browserResponse.HTTPStatus = http.StatusOK
		browserResponse.file = "../assets/templates/register.html"
	}

	logger.Print(browserResponse.Message)
	sendResponse(w, r, browserResponse)
	return
}

func processOxipayResponse(oxipayResponse *oxipay.Response, responseType oxipay.ResponseType, amount string) *Response {

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
		logger.Infof("Status: %f", oxipayResponseCode.LogMessage)
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

func bindToRegistrationPayload(r *http.Request) (*oxipay.RegistrationPayload, error) {

	if err := r.ParseForm(); err != nil {
		logger.Errorf("Unable to bind registration payload: %s", err)
		return nil, err
	}
	uniqueID, _ := shortid.Generate()
	deviceToken := r.Form.Get("DeviceToken")
	merchantID := r.Form.Get("MerchantID")
	FxlDeviceID := deviceToken + "-" + uniqueID

	register := &oxipay.RegistrationPayload{
		MerchantID:      merchantID,
		DeviceID:        FxlDeviceID,
		DeviceToken:     deviceToken,
		OperatorID:      "unknown",
		FirmwareVersion: "version " + oxipayClient.GetVersion(),
		POSVendor:       "Vend-Proxy",
	}

	return register, nil
}

func logRequest(r *http.Request) {
	dump, _ := httputil.DumpRequest(r, true)
	logger.Debugf("%q ", dump)
	// if r.Body != nil {
	// 	// we need to copy the bytes out of the buffer
	// 	// we we can inspect the contents without draining the buffer

	// 	body, _ := ioutil.ReadAll(r.Body)
	// 	log.Printf(colour.GDarkGray("Body: %s \n"), body)
	// }
	if r.RequestURI != "" {
		query := r.RequestURI
		logger.Debugf("Query  %s", query)
	}

}

// Index displays the main payment processing page, giving the user options of
// which outcome they would like the Pay Example to simulate.
func Index(w http.ResponseWriter, r *http.Request) {

	logRequest(r)
	var err error

	if err := r.ParseForm(); err != nil {
		logger.Errorf("Index error parsing form: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	origin := r.Form.Get("origin")
	origin, _ = url.PathUnescape(origin)

	vReq := &vend.PaymentRequest{
		Amount:     r.Form.Get("amount"),
		Origin:     origin,
		RegisterID: r.Form.Get("register_id"),
	}

	logger.Debugf("Received %s from %s for register %s", vReq.Amount, vReq.Origin, vReq.RegisterID)
	vReq, err = validPaymentRequest(vReq)

	if err != nil {
		w.Write([]byte("Not a valid request"))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// we just want to ensure there is a terminal available
	_, err = term.GetRegister(vReq.Origin, vReq.RegisterID)

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

	session, err := getSession(r, "oxipay")
	session.Options.SameSite = 4
	if err != nil {
		logger.Error(err)
	}

	session.Values["vReq"] = vReq
	err = sessions.Save(r, w)

	if err != nil {
		logger.Error(err)
	}
	logger.Infof("Session initiated: %s ", session.ID)
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

	logger.Debugf("Payment: %s from %s for register %s", vReq.Amount, vReq.Origin, vReq.RegisterID)
	vReq, err := validPaymentRequest(vReq)

	return vReq, err
}

// RefundHandler handles performing a refund
func RefundHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	cxFields := logrus.Fields{
		"module": "proxy",
		"call":   "RefundHandler",
	}
	cxLog := logger.WithFields(cxFields)

	// create the default response so that we always have something to send to the browser
	browserResponse := new(Response)
	var err error

	x, err := getPaymentRequestFromSession(r)
	if err != nil {
		cxLog.Error("Unable to get the Refund Request from the session")
		cxLog.Error(err)
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
	cxFields["register_id"] = x.RegisterID
	cxFields["origin"] = x.Origin

	terminal := terminal.NewTerminal(db)

	register, err := terminal.GetRegister(vReq.Origin, vReq.RegisterID)
	if err != nil {
		cxLog.Info("Register Not Found, redirecting to /register")
		// redirect to registration page
		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}
	cxFields["merchant_id"] = register.FxlSellerID

	txnRef, err := shortid.Generate()
	var oxipayPayload = &oxipay.SalesAdjustmentPayload{
		Amount:            strings.Replace(vReq.Amount, "-", "", 1),
		MerchantID:        register.FxlSellerID,
		DeviceID:          register.FxlRegisterID,
		FirmwareVersion:   "vend_integration_v0.0.1",
		OperatorID:        "Vend",
		PurchaseRef:       vReq.PurchaseNumber,
		PosTransactionRef: txnRef, // @todo see if vend has a uniqueID for this also
	}

	// generate the plaintext for the signature
	plainText := oxipay.GeneratePlainTextSignature(oxipayPayload)
	logger.Infof("Humm plain text: %s \n", plainText)

	// sign the message
	oxipayPayload.Signature = oxipay.SignMessage(plainText, register.FxlDeviceSigningKey)
	logger.Infof("Humm signature: %s \n", oxipayPayload.Signature)

	// send authorisation to oxipay
	oxipayResponse, err := oxipayClient.ProcessSalesAdjustment(oxipayPayload)

	if err != nil {
		// log the raw response
		logger.Errorf("Error Processing: %s", oxipayResponse)
		return
	}

	// ensure the response has come from Oxipay
	var validSignature bool
	validSignature, err = oxipayResponse.Authenticate(register.FxlDeviceSigningKey)

	if !validSignature || err != nil {
		browserResponse.Message = "The signature does not match the expected signature"
		browserResponse.HTTPStatus = http.StatusBadRequest
	} else {
		// Return a response to the browser bases on the response from Oxipay
		browserResponse = processOxipayResponse(oxipayResponse, oxipay.Adjustment, oxipayPayload.Amount)
		browserResponse.Amount = "0" // this is set because the payload
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

	logRequest(r)

	vReq, err = bindToPaymentPayload(r)
	if err != nil {
		logger.Error(err)
		http.Error(w, "There was a problem processing the request", http.StatusBadRequest)
	}

	// looks up the database to get the fake Oxipay terminal
	// so that we can issue this against Oxipay
	// if the seller has correctly configured the gateway they will not hit this
	// directly but it's here as safeguard
	terminal, err := term.GetRegister(vReq.Origin, vReq.RegisterID)
	if err != nil {
		// redirect
		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}
	logger.Infof("Processing Payment using Oxipay register %s ", terminal.FxlRegisterID)

	// send off to Oxipay
	//var oxipayPayload
	var oxipayPayload = &oxipay.AuthorisationPayload{
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
	logger.Debugf("Humm plain text: %s \n", plainText)

	// sign the message
	oxipayPayload.Signature = oxipay.SignMessage(plainText, terminal.FxlDeviceSigningKey)
	logger.Debugf("Humm signature: %s \n", oxipayPayload.Signature)

	// send authorisation to the Oxipay POS API
	oxipayResponse, err := oxipayClient.ProcessAuthorisation(oxipayPayload)

	if err != nil {
		http.Error(w, "There was a problem processing the request", http.StatusInternalServerError)
		// log the raw response

		msg := fmt.Sprintf("Error Processing: %s", oxipayResponse)
		logger.Error(msg)
		return
	}

	// ensure the response has come from Oxipay
	validSignature, err := oxipayResponse.Authenticate(terminal.FxlDeviceSigningKey)
	if !validSignature || err != nil {
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
		absFile, err := filepath.Abs(response.file)
		if err != nil {
			logger.Warnf("Unable to find file %s: %e", response.file, err)
		} else {
			logger.Infof("Serving file : %s ", absFile)
		}

		http.ServeFile(w, r, response.file)

		return
	}

	// Marshal our response into JSON.
	responseJSON, err := json.Marshal(response)
	if err != nil {
		logger.Errorf("Failed to marshal response json: %s ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	logger.Debugf("Sending Response: %s \n to the browser \n", responseJSON)

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
