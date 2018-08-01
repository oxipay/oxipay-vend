// package vendproxy
package vendproxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"reflect"
	"strconv"

	"./oxipay"
	// "time"s
	_ "crypto/hmac"
	"database/sql"
	"encoding/gob"
	"sort"

	colour "github.com/bclicn/color"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"
	"github.com/vend/peg/src/vendproxy/vend"

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
	gob.Register(vend.VendPaymentRequest{})

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
	// logRequest(r)

	switch r.Method {
	case http.MethodPost:

		registrationPayload, err := bind(r)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
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

				// We get the Device Token from the
				session, err := SessionStore.Get(r, "oxipay")
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				val := session.Values["vReq"]
				//val := session.Values["origin"]
				//vReq, ok := val.(string)
				var vendPaymentRequest = vend.VendPaymentRequest{}
				vendPaymentRequest, ok := val.(vend.VendPaymentRequest)

				if !ok {
					_ = vendPaymentRequest
					log.Println("Can't get vReq from session")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				//success
				// redirect back to transaction
				log.Print("Device Successfully registered")
				terminal := &Terminal{
					FxlDeviceSigningKey: response.Key,
					FxlRegisterID:       registrationPayload.DeviceID,
					FxlSellerID:         registrationPayload.MerchantID, // Oxipay Merchant No
					Origin:              vendPaymentRequest.Origin,      // Vend Website
					VendRegisterID:      vendPaymentRequest.RegisterID,  // Vend Register ID
				}

				_, err = terminal.save("vend-proxy")
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

	register := &OxipayRegistrationPayload{
		MerchantID:      merchantID,
		DeviceID:        FxlDeviceID,
		DeviceToken:     deviceToken,
		OperatorID:      "unknown",
		FirmwareVersion: "version 1.0",
		POSVendor:       "Vend-Proxy",
	}

	return register, nil
}

func (payload *oxipay.OxipayRegistrationPayload) validate() error {

	if payload == nil {
		return errors.New("payload is empty")
	}

	return nil
}

func processAuthorisation(oxipayPayload *oxipay.OxipayPayload) (*oxipay.OxipayResponse, error) {
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

	r.ParseForm()
	origin := r.Form.Get("origin")
	origin, _ = url.PathUnescape(origin)

	// @todo new payment request
	vReq := &vend.VendPaymentRequest{
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

	vReq := &vend.VendPaymentRequest{
		Amount:     r.Form.Get("amount"),
		Origin:     origin,
		RegisterID: r.Form.Get("register_id"),
		code:       r.Form.Get("paymentcode"),
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

//
func generatePlainTextSignature(payload interface{}) string {

	var buffer bytes.Buffer

	// create a temporary map so we can sort the keys,
	// go intentionally randomises maps so we need to
	// store the keys in an array which we can sort
	v := reflect.TypeOf(payload).Elem()
	y := reflect.ValueOf(payload)
	if y.IsNil() {
		return ""
	}
	x := y.Elem()

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
		// calld
		if v[0:2] == "x_" {
			buffer.WriteString(fmt.Sprintf("%s%s", v, payloadList[v]))
		}
	}

	return buffer.String()
}

func validPaymentRequest(req *vend.VendPaymentRequest) (*vend.VendPaymentRequest, error) {

	// convert the amount to cents and then go back to a string for
	// the checksum
	amountFloat, err := strconv.ParseFloat(req.Amount, 64)
	if err != nil {
		log.Println("failed to convert amount string to float: ", err)
	}
	req.Amount = strconv.FormatFloat((amountFloat * 100), 'f', 0, 64)
	return req, err
}
