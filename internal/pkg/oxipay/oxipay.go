package oxipay

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"reflect"
	"sort"
)

// Version  which version of the proxy are we using
const Version string = "1.0"

// GatewayURL Default URL for the Oxipay Gateway @todo get from config
var GatewayURL = "https://testpos.oxipay.com.au/webapi/v1/"

// ProcessAuthorisationURL is the URL of the POS API for ProcessAuthoorisation
var ProcessAuthorisationURL = GatewayURL + "/ProcessAuthorisation"

// CreateKeyURL is the URL of the POS API for CreateKey
var CreateKeyURL = GatewayURL + "/CreateKey"

// Db connection to database
// var Db *sql.DB

//HTTPClientTimout default http client timeout
const HTTPClientTimout = 0

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

// Terminal terminal mapping
type Terminal struct {
	FxlRegisterID       string // Oxipay registerid
	FxlSellerID         string
	FxlDeviceSigningKey string
	Origin              string
	VendRegisterID      string
}

// Ping returns pong
func Ping() string {
	return "pong"
}

// RegisterPosDevice is used to register a new vend terminal
func RegisterPosDevice(payload *OxipayRegistrationPayload) (*OxipayResponse, error) {
	var err error

	jsonValue, _ := json.Marshal(payload)

	log.Printf("POST to URL %s", CreateKeyURL)
	log.Printf("Register POS Device Payload: %s", string(jsonValue))

	client := http.Client{}
	client.Timeout = HTTPClientTimout
	response, responseErr := client.Post(CreateKeyURL, "application/json", bytes.NewBuffer(jsonValue))

	if responseErr != nil {
		panic(responseErr)
	}
	defer response.Body.Close()
	log.Println("Register Response Status:", response.Status)
	log.Println("Register Response Headers:", response.Header)
	body, _ := ioutil.ReadAll(response.Body)
	log.Printf("ProcessAuthorisation Response Body: \n %v", string(body))

	// turn {"x_purchase_number":"52011595","x_status":"Success","x_code":"SPRA01","x_message":"Approved","signature":"84b2ed2ec504a0aef134c3da57a060558de1290de7d5055ab8d070dd8354991b","tracking_data":null}
	// into a struct
	oxipayResponse := new(OxipayResponse)
	err = json.Unmarshal(body, oxipayResponse)

	if err != nil {
		log.Println(err)
		return nil, errors.New("Unable to unmarshall response from server")
	}

	log.Printf("Unmarshalled Register POS Response Body: %s \n", oxipayResponse)
	return oxipayResponse, err
}

// ProcessAuthorisation calls the ProcessAuthorisation Method
func ProcessAuthorisation(oxipayPayload *OxipayPayload) (*OxipayResponse, error) {

	var err error

	jsonValue, _ := json.Marshal(oxipayPayload)
	log.Printf("POST to URL %s \n", ProcessAuthorisationURL)
	log.Println("Authorisation Payload: " + string(jsonValue))

	client := http.Client{}
	client.Timeout = HTTPClientTimout
	response, responseErr := client.Post(ProcessAuthorisationURL, "application/json", bytes.NewBuffer(jsonValue))

	if responseErr != nil {
		panic(responseErr)
	}
	defer response.Body.Close()

	log.Println("ProcessAuthorisation Response Status:", response.Status)
	log.Println("ProcessAuthorisation Response Headers:", response.Header)

	body, _ := ioutil.ReadAll(response.Body)
	log.Printf("ProcessAuthorisation Response Body: \n %v", string(body))

	oxipayResponse := new(OxipayResponse)
	err = json.Unmarshal(body, oxipayResponse)

	if err != nil {
		return nil, err
	}

	log.Printf("Unmarshalled Oxipay Response Body: %v \n", oxipayResponse)
	return oxipayResponse, err
}

// SignMessage will generate an HMAC of the plaintext
func SignMessage(plainText string, signingKey string) string {

	key := []byte(signingKey)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(plainText))
	macString := hex.EncodeToString(mac.Sum(nil))
	log.Printf("Oxipay Signature: %s \n", macString)
	return macString
}

// Validate will perform validation on a OxipayRegistrationPayload
func (payload *OxipayRegistrationPayload) Validate() error {

	if payload == nil {
		return errors.New("payload is empty")
	}

	return nil
}

// GeneratePlainTextSignature will generate an Oxipay plain text message ready for signing
func GeneratePlainTextSignature(payload interface{}) string {

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
	plainText := buffer.String()
	log.Printf("Signature Plain Text: %s \n", plainText)
	return plainText
}

// CheckMAC used to validate responses from the remote server
func CheckMAC(message, messageMAC, key []byte) bool {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)

	expectedMAC := mac.Sum(nil)

	// we use hmac.Equal because regular equality (i.e == ) is subject to timing attacks
	return hmac.Equal(messageMAC, expectedMAC)
}

// ResponseCode maps the oxipay response code to a generic ACCEPT/DECLINE
type responseCode struct {
	TxnStatus       string
	LogMessage      string
	CustomerMessage string
}

const (
	// StatusApproved Transaction Successful
	StatusApproved = "APPROVED"
	// StatusDeclined Transaction Declined
	StatusDeclined = "DECLINED"
	// StatusFailed Transaction Failed
	StatusFailed = "FAILED"
)

// ProcessAuthorisationResponses provides a guarded response type based on the response code from the Oxipay request
func ProcessAuthorisationResponses() func(string) *responseCode {

	innerMap := map[string]*responseCode{
		"SPRA01": &responseCode{
			TxnStatus:       StatusApproved,
			LogMessage:      "APPROVED",
			CustomerMessage: "APPROVED",
		},
		"FPRA01": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined due to internal risk assessment against the customer",
			CustomerMessage: "Do not try again",
		},
		"FPRA02": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined due to insufficient funds for the deposit",
			CustomerMessage: "Please call customer support",
		},
		"FPRA03": &responseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Declined as communication to the bank is currently unavailable",
			CustomerMessage: "Please try again shortly. Communication to the bank is unavailable",
		},
		"FPRA04": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because the customer limit has been exceeded",
			CustomerMessage: "Please contact Oxipay customer support",
		},
		"FPRA05": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined due to negative payment history for the customer",
			CustomerMessage: "Please contact Oxipay customer support for more information",
		},
		"FPRA06": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because the credit-card used for the deposit is expired",
			CustomerMessage: "Declined because the credit-card used for the deposit is expired",
		},
		"FPRA07": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because supplied POSTransactionRef has already been processed",
			CustomerMessage: "We have seen this Transaction ID before, please try again",
		},
		"FPRA08": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because the instalment amount was below the minimum threshold",
			CustomerMessage: "Transaction below minimum",
		},
		"FPRA09": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because purchase amount exceeded pre-approved amount",
			CustomerMessage: "Please contact Oxipay customer support",
		},
		"FPRA21": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "The Payment Code was not found",
			CustomerMessage: "This is not a valid Payment Code.",
		},
		"FPRA22": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "The Payment Code has already been used",
			CustomerMessage: "The Payment Code has already been used",
		},
		"FPRA23": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "The Payment Code has expired",
			CustomerMessage: "The Payment Code has expired",
		},
		"FPRA24": &responseCode{
			TxnStatus:  StatusDeclined,
			LogMessage: "The Payment Code has been cancelled",
			CustomerMessage: `Payment Code has been cancelled. 
			Please try again with a new Payment Code`,
		},
		"FPRA99": &responseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "DECLINED by Oxipay Gateway",
			CustomerMessage: "DECLINED",
		},
		"EVAL02": &responseCode{
			TxnStatus:  StatusFailed,
			LogMessage: "Request is invalid",
			CustomerMessage: `The request to Oxipay was invalid. 
			You can try again with a different Payment Code. 
			Please contact pit@oxipay.com.au for further support`,
		},
	}

	return func(key string) *responseCode {
		return innerMap[key]
	}
}
