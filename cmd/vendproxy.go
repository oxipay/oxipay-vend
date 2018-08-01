package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"

	// "time"s
	_ "crypto/hmac"
	"database/sql"
	"encoding/gob"

	colour "github.com/bclicn/color"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"
	"github.com/vend/peg/internal/pkg/oxipay"
	"github.com/vend/peg/internal/pkg/terminal"
	"github.com/vend/peg/internal/pkg/vend"

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
}

// @todo load from config
/// var SessionStore *mysqlstore.MySQLStore

// SessionStore store of session data
var SessionStore *sessions.FilesystemStore

func main() {

	_ = oxipay.Ping()
	oxipay.Db = connectToDatabase()

	var err error
	// SessionStore, err := mysqlstore.NewMySQLStoreFromConnection(db, "sessions", "/", 3600, []byte("@todo_change_me"))
	SessionStore = sessions.NewFilesystemStore("", []byte("some key"))

	_ = SessionStore

	// register the type VendPaymentRequest so that we can use it later in the session
	gob.Register(vend.PaymentRequest{})

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
	db.SetConnMaxLifetime(3600)
	return db
}

// RegisterHandler GET request. Prompt for the Merchant ID and Device Token
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	// logRequest(r)

	switch r.Method {
	case http.MethodPost:

		browserResponse := &Response{}

		registrationPayload, err := bind(r)

		if err != nil {
			browserResponse.HTTPStatus = http.StatusBadRequest
			return
		}

		err = registrationPayload.Validate()

		if err == nil {
			// @todo move to separate function
			plainText := oxipay.GeneratePlainTextSignature(registrationPayload)
			log.Printf(colour.BLightYellow("Oxipay Plain Text: %s \n"), plainText)

			// sign the message
			registrationPayload.Signature = oxipay.SignMessage(plainText, registrationPayload.DeviceToken)
			log.Printf(colour.BLightYellow("Oxipay Signature: %s \n"), registrationPayload.Signature)

			// submit to oxipay
			response, err := oxipay.RegisterPosDevice(registrationPayload)

			if err != nil {

				log.Println(err)
				msg := "We are unable to process this request "
				browserResponse.HTTPStatus = http.StatusBadGateway

				w.WriteHeader(http.StatusBadGateway)
				w.Write([]byte(msg))
				return
			}

			switch response.Code {

			case "SCRK01":

				// We get the Device Token from the
				// @todo session panics if it's not there
				session, err := SessionStore.Get(r, "oxipay")
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				val := session.Values["vReq"]
				//val := session.Values["origin"]
				//vReq, ok := val.(string)
				var vendPaymentRequest = vend.PaymentRequest{}
				vendPaymentRequest, ok := val.(vend.PaymentRequest)

				if !ok {
					_ = vendPaymentRequest
					log.Println("Can't get vReq from session")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				//success
				// redirect back to transaction
				log.Print("Device Successfully registered")

				terminal := terminal.NewTerminal(
					response.Key,
					registrationPayload.DeviceID,
					registrationPayload.MerchantID,
					vendPaymentRequest.Origin,
					vendPaymentRequest.RegisterID,
				)

				_, err = terminal.Save("vend-proxy")
				if err != nil {
					log.Fatal(err)
					browserResponse.Message = "Unable to process request"
					browserResponse.HTTPStatus = http.StatusServiceUnavailable
				} else {
					browserResponse.Message = "Created"
					browserResponse.HTTPStatus = http.StatusOK
				}
				// serve up the success page
				http.ServeFile(w, r, "./assets/templates/register_success.html")
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

func bind(r *http.Request) (*oxipay.OxipayRegistrationPayload, error) {

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

	if err := r.ParseForm(); err != nil {
		log.Fatalf("Error parsing form: %s", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
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

	vReq := &vend.PaymentRequest{
		Amount:     r.Form.Get("amount"),
		Origin:     origin,
		RegisterID: r.Form.Get("register_id"),
		Code:       r.Form.Get("paymentcode"),
	}

	log.Printf("Received %s from %s for register %s", vReq.Amount, vReq.Origin, vReq.RegisterID)
	vReq, err := validPaymentRequest(vReq)
	if err != nil {
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

	terminal, err := terminal.GetRegisteredTerminal(vReq.Origin, vReq.RegisterID)

	if err != nil {
		// redirect
		http.Redirect(w, r, "/register", 302)
		return
	}
	log.Printf("Using Oxipay register %s ", terminal.FxlRegisterID)

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

	// use
	log.Printf("Use the following payload for Oxipay: %v \n", oxipayPayload)

	// Here is the point where you have all the information you need to send a
	// request to your payment gateway or terminal to process the transaction
	// amount.
	//var gatewayURL = "https://testpos.oxipay.com.au/webapi/v1/"

	oxipayResponse, err := oxipay.ProcessAuthorisation(oxipayPayload)

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
		response.RegisterID = terminal.VendRegisterID
		response.Status = statusAccepted
		response.HTTPStatus = http.StatusOK
		break
	case "CANCEL":
		response.Status = statusCancelled
		response.HTTPStatus = http.StatusOK
		break
	case "ESIG01":
		// oxipay signature mismatch
		response.Status = statusFailed
		response.HTTPStatus = http.StatusOK
		break
	case "FPRA99":
		response.Status = statusDeclined
		response.HTTPStatus = http.StatusOK
		break
	case "EAUT01":
		// tried to process with an unknown terminal
		// needs registration
		// should we remove from mapping ? then redirect ?
		// need to think this through as we need to authenticate them first otherwise you
		// can remove other peoples transactions
		response.Status = statusFailed
		response.HTTPStatus = http.StatusOK
		break
	case "FAIL":
		response.Status = statusFailed
		response.HTTPStatus = http.StatusOK
		break
	case "TIMEOUT":
		response.Status = statusTimeout
		response.HTTPStatus = http.StatusOK
		break
	default:
		response.Status = statusUnknown
		response.HTTPStatus = http.StatusOK
		break
	}

	sendResponse(w, response)
	return

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
