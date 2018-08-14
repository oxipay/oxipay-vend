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
	"strings"
)

// Version  which version of the proxy are we using
const Version string = "1.0"

// GatewayURL Default URL for the Oxipay Gateway @todo get from config
var GatewayURL = ""

// var GatewayURL = "https://testpos.oxipay.com.au/webapi/v1/"

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
	PurchaseNumber string `json:"x_purchase_number,omitempty"`
	Status         string `json:"x_status,omitempty"`
	Code           string `json:"x_code,omitempty"`
	Message        string `json:"x_message"`
	Key            string `json:"x_key,omitempty"`
	Signature      string `json:"signature"`
}

// OxipaySalesAdjustmentPayload holds a request to Oxipay for the ProcessAdjustment
type OxipaySalesAdjustmentPayload struct {
	PosTransactionRef string `json:"x_pos_transaction_ref"`
	PurchaseRef       string `json:"x_purchase_ref"`
	MerchantID        string `json:"x_merchant_id"`
	Amount            string `json:"x_amount,omitempty"`
	DeviceID          string `json:"x_device_id,omitempty"`
	OperatorID        string `json:"x_operator_id,omitempty"`
	FirmwareVersion   string `json:"x_firmware_version,omitempty"`
	TrackingData      string `json:"tracking_data,omitempty"`
	Signature         string `json:"signature"`
}

// ResponseCode maps the oxipay response code to a generic ACCEPT/DECLINE
type ResponseCode struct {
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

// ResponseType The type of response received from Oxipay
type ResponseType int

const (
	// Adjustment ProcessSalesAdjustment
	Adjustment ResponseType = iota
	// Authorisation ProcessAuthorisation
	Authorisation ResponseType = iota

	// Registration Result of CreateKey
	Registration ResponseType = iota
)

// Ping returns pong
func Ping() string {
	return "pong"
}

// RegisterPosDevice is used to register a new vend terminal
func RegisterPosDevice(payload *OxipayRegistrationPayload) (*OxipayResponse, error) {
	var err error
	var CreateKeyURL = GatewayURL + "/CreateKey"

	oxipayResponse := new(OxipayResponse)

	jsonValue, _ := json.Marshal(payload)

	log.Printf("POST to URL %s", CreateKeyURL)
	log.Printf("Register POS Device Payload: %s", string(jsonValue))

	client := http.Client{}
	client.Timeout = HTTPClientTimout
	response, responseErr := client.Post(CreateKeyURL, "application/json", bytes.NewBuffer(jsonValue))

	if responseErr != nil {
		return oxipayResponse, responseErr
	}

	defer response.Body.Close()
	log.Println("Register Response Status:", response.Status)
	log.Println("Register Response Headers:", response.Header)
	body, _ := ioutil.ReadAll(response.Body)
	log.Printf("ProcessAuthorisation Response Body: \n %v", string(body))

	// turn {"x_purchase_number":"52011595","x_status":"Success","x_code":"SPRA01","x_message":"Approved","signature":"84b2ed2ec504a0aef134c3da57a060558de1290de7d5055ab8d070dd8354991b","tracking_data":null}
	// into a struct
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

	// ProcessAuthorisationURL is the URL of the POS API for ProcessAuthoorisation
	var ProcessAuthorisationURL = GatewayURL + "/ProcessAuthorisation"

	var err error
	oxipayResponse := new(OxipayResponse)

	jsonValue, _ := json.Marshal(oxipayPayload)
	log.Printf("POST to URL %s \n", ProcessAuthorisationURL)
	log.Println("Authorisation Payload: " + string(jsonValue))

	client := http.Client{}
	client.Timeout = HTTPClientTimout
	response, responseErr := client.Post(ProcessAuthorisationURL, "application/json", bytes.NewBuffer(jsonValue))

	if responseErr != nil {
		return oxipayResponse, responseErr
	}
	defer response.Body.Close()

	log.Println("ProcessAuthorisation Response Status:", response.Status)
	log.Println("ProcessAuthorisation Response Headers:", response.Header)

	body, _ := ioutil.ReadAll(response.Body)
	log.Printf("ProcessAuthorisation Response Body: \n %v", string(body))

	err = json.Unmarshal(body, oxipayResponse)

	if err != nil {
		return nil, err
	}

	log.Printf("Unmarshalled Oxipay Response Body: %v \n", oxipayResponse)
	return oxipayResponse, err
}

// ProcessSalesAdjustment provides a mechansim to perform a sales ajustment on an Oxipay schedule
func ProcessSalesAdjustment(adjustment *OxipaySalesAdjustmentPayload) (*OxipayResponse, error) {

	var err error

	// ProcessSalesAdjustmentURL is the URL of the POS API for refunds
	var ProcessSalesAdjustmentURL = GatewayURL + "/ProcessSalesAdjustment"

	oxipayResponse := new(OxipayResponse)

	jsonValue, _ := json.Marshal(adjustment)
	log.Printf("POST to URL %s \n", ProcessSalesAdjustmentURL)
	log.Println(": " + string(jsonValue))

	client := http.Client{}
	client.Timeout = HTTPClientTimout
	response, responseErr := client.Post(ProcessSalesAdjustmentURL, "application/json", bytes.NewBuffer(jsonValue))

	if responseErr != nil {
		return oxipayResponse, responseErr
	}
	defer response.Body.Close()

	log.Println("ProcessSalesAdjustment Response Status:", response.Status)
	log.Println("ProcessSalesAdjustment Response Headers:", response.Header)

	body, _ := ioutil.ReadAll(response.Body)
	log.Printf("ProcessAuthorisation Response Body: \n %v", string(body))

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

//Authenticate validates HMAC
func (r *OxipayResponse) Authenticate(key string) bool {
	responsePlainText := GeneratePlainTextSignature(r)

	if len(r.Signature) >= 0 {
		return CheckMAC([]byte(responsePlainText), []byte(r.Signature), []byte(key))
	}
	return false
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
		idx := strings.Index(tag, ",")
		if idx > 0 {
			tag = tag[:idx]
		}

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
		if v[0:2] == "x_" && payloadList[v] != "" {
			buffer.WriteString(fmt.Sprintf("%s%s", v, payloadList[v]))
		}
	}
	plainText := buffer.String()
	log.Printf("Signature Plain Text: %s \n", plainText)
	return plainText
}

// CheckMAC used to validate responses from the remote server
func CheckMAC(message []byte, messageMAC []byte, key []byte) bool {
	mac := hmac.New(sha256.New, key)
	_, err := mac.Write(message)

	if err != nil {
		log.Println(err)
		return false
	}

	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	// we use hmac.Equal because regular equality (i.e == ) is subject to timing attacks
	isGood := hmac.Equal(messageMAC, []byte(expectedMAC))

	if isGood == false {
		log.Printf("Signature mismatch: expected %s, got %s \n", messageMAC, expectedMAC)
	}
	return isGood
}

// ProcessAuthorisationResponses provides a guarded response type based on the response code from the Oxipay request
func ProcessAuthorisationResponses() func(string) *ResponseCode {

	innerMap := map[string]*ResponseCode{
		"SPRA01": &ResponseCode{
			TxnStatus:       StatusApproved,
			LogMessage:      "APPROVED",
			CustomerMessage: "APPROVED",
		},
		"FPRA01": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined due to internal risk assessment against the customer",
			CustomerMessage: "Do not try again",
		},
		"FPRA02": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined due to insufficient funds for the deposit",
			CustomerMessage: "Please call customer support",
		},
		"FPRA03": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Declined as communication to the bank is currently unavailable",
			CustomerMessage: "Please try again shortly. Communication to the bank is unavailable",
		},
		"FPRA04": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because the customer limit has been exceeded",
			CustomerMessage: "Please contact Oxipay customer support",
		},
		"FPRA05": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined due to negative payment history for the customer",
			CustomerMessage: "Please contact Oxipay customer support for more information",
		},
		"FPRA06": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because the credit-card used for the deposit is expired",
			CustomerMessage: "Declined because the credit-card used for the deposit is expired",
		},
		"FPRA07": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because supplied POSTransactionRef has already been processed",
			CustomerMessage: "We have seen this Transaction ID before, please try again",
		},
		"FPRA08": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because the instalment amount was below the minimum threshold",
			CustomerMessage: "Transaction below minimum",
		},
		"FPRA09": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because purchase amount exceeded pre-approved amount",
			CustomerMessage: "Please contact Oxipay customer support",
		},
		"FPRA21": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "The Payment Code was not found",
			CustomerMessage: "This is not a valid Payment Code.",
		},
		"FPRA22": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "The Payment Code has already been used",
			CustomerMessage: "The Payment Code has already been used",
		},
		"FPRA23": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "The Payment Code has expired",
			CustomerMessage: "The Payment Code has expired",
		},
		"FPRA24": &ResponseCode{
			TxnStatus:  StatusDeclined,
			LogMessage: "The Payment Code has been cancelled",
			CustomerMessage: `Payment Code has been cancelled. 
			Please try again with a new Payment Code`,
		},
		"FPRA99": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "DECLINED by Oxipay Gateway",
			CustomerMessage: "DECLINED",
		},
		"EVAL02": &ResponseCode{
			TxnStatus:  StatusFailed,
			LogMessage: "Request is invalid",
			CustomerMessage: `The request to Oxipay was invalid. 
			You can try again with a different Payment Code. 
			Please contact pit@oxipay.com.au for further support`,
		},
		"ESIG01": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Signature mismatch error. Has the terminal changed, try removing the key for the device? ",
			CustomerMessage: `Please contact pit@oxipay.com.au for further support`,
		},
		"EISE01": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Server Error",
			CustomerMessage: `Please contact pit@oxipay.com.au for further support`,
		},
	}

	return func(key string) *ResponseCode {
		// check to make sure we know what the response is
		ret := innerMap[key]

		if ret == nil {
			return innerMap["EISE01"]
		}
		return ret
	}
}

// ProcessSalesAdjustmentResponse provides a guarded response type based on the response code from the Oxipay request
func ProcessSalesAdjustmentResponse() func(string) *ResponseCode {

	innerMap := map[string]*ResponseCode{
		"SPSA01": &ResponseCode{
			TxnStatus:       StatusApproved,
			LogMessage:      "APPROVED",
			CustomerMessage: "APPROVED",
		},
		"FPSA01": &ResponseCode{
			TxnStatus:       StatusDeclined,
			LogMessage:      "Unable to find the specified POS transaction reference",
			CustomerMessage: "Unable to find the specified POS transaction reference",
		},
		"FPSA02": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "This contract has already been completed",
			CustomerMessage: "This contract has already been completed",
		},
		"FPSA03": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "This Oxipay contract has previously been cancelled and all payments collected have been refunded to the customer",
			CustomerMessage: "This Oxipay contract has previously been cancelled and all payments collected have been refunded to the customer",
		},
		"FPSA04": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Sales adjustment cannot be processed for this amount",
			CustomerMessage: "Sales adjustment cannot be processed for this amount",
		},
		"FPSA05": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Unable to process a sales adjustment for this contract. Please contact Merchant Services during business hours for further information",
			CustomerMessage: "Unable to process a sales adjustment for this contract. Please contact Merchant Services during business hours for further information",
		},
		"FPSA06": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Sales adjustment cannot be processed. Please call Oxipay Collections",
			CustomerMessage: "Sales adjustment cannot be processed. Please call Oxipay Collections",
		},
		"FPSA07": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Sales adjustment cannot be processed at this store",
			CustomerMessage: "Sales adjustment cannot be processed at this store",
		},
		"FPSA08": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Sales adjustment cannot be processed for this transaction. Duplicate receipt number found.",
			CustomerMessage: "Sales adjustment cannot be processed for this transaction. Duplicate receipt number found.",
		},
		"FPSA09": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Amount must be greater than 0.",
			CustomerMessage: "Amount must be greater than 0.",
		},
		"EAUT01": &ResponseCode{
			TxnStatus:  StatusFailed,
			LogMessage: "Authentication to gateway error",
			CustomerMessage: `The request to Oxipay was not what we were expecting. 
			You can try again with a different Payment Code. 
			Please contact pit@oxipay.com.au for further support`,
		},
		"EVAL01": &ResponseCode{
			TxnStatus:  StatusFailed,
			LogMessage: "Request is invalid",
			CustomerMessage: `The request to Oxipay was what we were expecting. 
			You can try again with a different Payment Code. 
			Please contact pit@oxipay.com.au for further support`,
		},
		"ESIG01": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Signature mismatch error. Has the terminal changed, try removing the key for the device? ",
			CustomerMessage: `Please contact pit@oxipay.com.au for further support`,
		},
		"EISE01": &ResponseCode{
			TxnStatus:       StatusFailed,
			LogMessage:      "Server Error",
			CustomerMessage: `Please contact pit@oxipay.com.au for further support`,
		},
	}

	return func(key string) *ResponseCode {
		// check to make sure we know what the response is
		ret := innerMap[key]

		if ret == nil {
			return innerMap["EISE01"]
		}
		return ret
	}
}
