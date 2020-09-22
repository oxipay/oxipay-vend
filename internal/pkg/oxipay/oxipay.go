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
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"

	log "github.com/sirupsen/logrus"
)

//HTTPClientTimout default http client timeout
const HTTPClientTimout = 0

const defaultResponseCode = "EISE01"

// Client exposes an interface to Oxipay
type Client interface {
	RegisterPosDevice(*RegistrationPayload) (*Response, error)
	ProcessAuthorisation(oxipayPayload *AuthorisationPayload) (*Response, error)
	ProcessSalesAdjustment(adjustment *SalesAdjustmentPayload) (*Response, error)
	GetVersion() string
}

type oxipay struct {
	GatewayURL string
	Version    string
	Log        *log.Logger
}

//NewOxipay returns a base struct on which all other functions operate
func NewOxipay(gatewayURL string, version string, log *log.Logger) Client {
	return &oxipay{
		GatewayURL: gatewayURL,
		Version:    version,
		Log:        log,
	}
}

// RegistrationPayload required to register a device with Oxipay
type RegistrationPayload struct {
	MerchantID      string `json:"x_merchant_id"`
	DeviceID        string `json:"x_device_id"`
	DeviceToken     string `json:"x_device_token"`
	OperatorID      string `json:"x_operator_id"`
	FirmwareVersion string `json:"x_firmware_version"`
	POSVendor       string `json:"x_pos_vendor"`
	TrackingData    string `json:"tracking_data,omitempty"`
	Signature       string `json:"signature"`
}

// AuthorisationPayload Payload used to send to Oxipay
type AuthorisationPayload struct {
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

// Response is the response returned from Oxipay for both a CreateKey and Sales Adjustment
type Response struct {
	PurchaseNumber string `json:"x_purchase_number,omitempty"`
	Status         string `json:"x_status,omitempty"`
	Code           string `json:"x_code,omitempty"`
	Message        string `json:"x_message"`
	Key            string `json:"x_key,omitempty"`
	Signature      string `json:"signature"`
}

// SalesAdjustmentPayload holds a request to Oxipay for the ProcessAdjustment
type SalesAdjustmentPayload struct {
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

func (oc *oxipay) GetVersion() string {
	return oc.Version
}

// RegisterPosDevice is used to register a new vend terminal
func (oc *oxipay) RegisterPosDevice(payload *RegistrationPayload) (*Response, error) {
	contextLogger := oc.Log.WithFields(log.Fields{
		"module":    "oxipay",
		"call":      "RegisterPosDevice",
		"device_id": payload.DeviceID,
	})

	jsonValue, _ := json.Marshal(payload)
	return post(oc.GatewayURL+"/CreateKey", jsonValue, contextLogger)
}

// ProcessAuthorisation calls the ProcessAuthorisation Method
func (oc *oxipay) ProcessAuthorisation(payload *AuthorisationPayload) (*Response, error) {
	contextLogger := oc.Log.WithFields(log.Fields{
		"module":      "oxipay",
		"call":        "ProcessAuthorisation",
		"device_id":   payload.DeviceID,
		"merchant_id": payload.MerchantID,
	})

	jsonValue, _ := json.Marshal(payload)
	return post(oc.GatewayURL+"/ProcessAuthorisation", jsonValue, contextLogger)
}

func post(url string, jsonValue []byte, contextLogger *logrus.Entry) (*Response, error) {

	var err error
	oxipayResponse := new(Response)

	contextLogger.Debugf("POST to : %s , %s \n", url, string(jsonValue))

	client := http.Client{}
	client.Timeout = HTTPClientTimout
	response, responseErr := client.Post(url, "application/json", bytes.NewBuffer(jsonValue))

	if responseErr != nil {
		return oxipayResponse, responseErr
	}
	defer response.Body.Close()

	contextLogger.Debugf(
		"Response: status =  %s header = %s \n",
		response.Status,
		response.Header,
	)

	body, _ := ioutil.ReadAll(response.Body)
	contextLogger.Debugf("Response Body: \n %s", string(body))

	err = json.Unmarshal(body, oxipayResponse)

	contextLogger.Debugf("Unmarshalled Oxipay Response Body: %v \n", oxipayResponse)

	return oxipayResponse, err
}

// ProcessSalesAdjustment provides a mechansim to perform a sales ajustment on an Oxipay schedule
func (oc *oxipay) ProcessSalesAdjustment(adjustment *SalesAdjustmentPayload) (*Response, error) {

	contextLogger := oc.Log.WithFields(log.Fields{
		"module":      "oxipay",
		"call":        "ProcessSalesAdjustment",
		"device_id":   adjustment.DeviceID,
		"merchant_id": adjustment.MerchantID,
	})

	jsonValue, _ := json.Marshal(adjustment)
	return post(oc.GatewayURL+"/ProcessSalesAdjustment", jsonValue, contextLogger)

}

// SignMessage will generate an HMAC of the plaintext
func SignMessage(plainText string, signingKey string) string {
	key := []byte(signingKey)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(plainText))
	macString := hex.EncodeToString(mac.Sum(nil))
	return macString
}

// Validate will perform validation on a OxipayRegistrationPayload
func (payload *RegistrationPayload) Validate() error {
	// @todo more validation here
	if payload == nil {
		return errors.New("payload is empty")
	}
	return nil
}

//Authenticate validates HMAC
func (r *Response) Authenticate(key string) (bool, error) {
	responsePlainText := GeneratePlainTextSignature(r)

	if len(r.Signature) >= 0 {
		return CheckMAC([]byte(responsePlainText), []byte(r.Signature), []byte(key))
	}
	return false, errors.New("Plaintext is signature is 0 length")
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
	log.Debug("Plaintext Signature is: %s", plainText)
	return plainText
}

// CheckMAC used to validate responses from the remote server
func CheckMAC(message []byte, messageMAC []byte, key []byte) (bool, error) {
	mac := hmac.New(sha256.New, key)
	_, err := mac.Write(message)

	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	// we use hmac.Equal because regular equality (i.e == ) is subject to timing attacks
	isGood := hmac.Equal(messageMAC, []byte(expectedMAC))

	return isGood, err
}

// ProcessRegistrationResponse provides a function to map an Oxipay CreateKey response to something we can pass back to the client
func ProcessRegistrationResponse() func(string) *ResponseCode {
	innerMap := map[string]*ResponseCode{
		"SCRK01": {
			TxnStatus:       StatusApproved,
			LogMessage:      "SUCCESS",
			CustomerMessage: "SUCCESS",
		},
		"FCRK01": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Device token provided could not be found",
			CustomerMessage: "Device token provided could not be found",
		},
		"FCRK02": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Device token provided has already been used",
			CustomerMessage: "Device token provided has already been used",
		},
		"EVAL01": {
			TxnStatus:  StatusFailed,
			LogMessage: "Request is invalid",
			CustomerMessage: `The request to Humm was invalid. 
			You can try again with a different Payment Code. 
			Please contact pit@shophumm.com.au for further support`,
		},
		"ESIG01": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Signature mismatch error. Has the terminal changed, try removing the key for the device? ",
			CustomerMessage: `Please contact pit@shophumm.com.au for further support`,
		},
		"EISE01": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Server Error",
			CustomerMessage: "Please contact pit@shophumm.com.au for further support",
		},
	}

	return func(key string) *ResponseCode {
		// check to make sure we know what the response is
		ret := innerMap[key]

		if ret == nil {
			return innerMap[defaultResponseCode]
		}
		return ret
	}
}

// ProcessAuthorisationResponses provides a guarded response type based on the response code from the Oxipay request
func ProcessAuthorisationResponses() func(string) *ResponseCode {

	innerMap := map[string]*ResponseCode{
		"SPRA01": {
			TxnStatus:       StatusApproved,
			LogMessage:      "APPROVED",
			CustomerMessage: "APPROVED",
		},
		"FPRA01": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined due to internal risk assessment against the customer",
			CustomerMessage: "Do not try again",
		},
		"FPRA02": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined due to insufficient funds for the deposit",
			CustomerMessage: "Please call customer support",
		},
		"FPRA03": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Declined as communication to the bank is currently unavailable",
			CustomerMessage: "Please try again shortly. Communication to the bank is unavailable",
		},
		"FPRA04": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because the customer limit has been exceeded",
			CustomerMessage: "Please contact Humm customer support",
		},
		"FPRA05": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined due to negative payment history for the customer",
			CustomerMessage: "Please contact Humm customer support for more information",
		},
		"FPRA06": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because the credit-card used for the deposit is expired",
			CustomerMessage: "Declined because the credit-card used for the deposit is expired",
		},
		"FPRA07": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because supplied POSTransactionRef has already been processed",
			CustomerMessage: "We have seen this Transaction ID before, please try again",
		},
		"FPRA08": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because the instalment amount was below the minimum threshold",
			CustomerMessage: "Transaction below minimum",
		},
		"FPRA09": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "Declined because purchase amount exceeded pre-approved amount",
			CustomerMessage: "Please contact Humm customer support",
		},
		"FPRA21": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "The Payment Code was not found",
			CustomerMessage: "This is not a valid Payment Code.",
		},
		"FPRA22": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "The Payment Code has already been used",
			CustomerMessage: "The Payment Code has already been used",
		},
		"FPRA23": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "The Payment Code has expired",
			CustomerMessage: "The Payment Code has expired",
		},
		"FPRA24": {
			TxnStatus:  StatusDeclined,
			LogMessage: "The Payment Code has been cancelled",
			CustomerMessage: `Payment Code has been cancelled. 
			Please try again with a new Payment Code`,
		},
		"FPRA99": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "DECLINED by the Humm Gateway",
			CustomerMessage: "Transaction has been declined by the Humm Gateway",
		},
		"EVAL02": {
			TxnStatus:  StatusFailed,
			LogMessage: "Request is invalid",
			CustomerMessage: `The request to Gateway was invalid. 
			You can try again with a different Payment Code. 
			Please contact pit@shophumm.com.au for further support`,
		},
		"ESIG01": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Signature mismatch error. Has the terminal changed, try removing the key for the device? ",
			CustomerMessage: `Please contact pit@shophumm.com.au for further support`,
		},
		"EISE01": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Server Error",
			CustomerMessage: `Please contact pit@shophumm.com.au for further support`,
		},
	}

	return func(key string) *ResponseCode {
		// check to make sure we know what the response is
		ret := innerMap[key]

		if ret == nil {
			return innerMap[defaultResponseCode]
		}
		return ret
	}
}

// ProcessSalesAdjustmentResponse provides a guarded response type based on the response code from the Oxipay request
func ProcessSalesAdjustmentResponse() func(string) *ResponseCode {

	innerMap := map[string]*ResponseCode{
		"SPSA01": {
			TxnStatus:       StatusApproved,
			LogMessage:      "APPROVED",
			CustomerMessage: "APPROVED",
		},
		"FPSA01": {
			TxnStatus:       StatusDeclined,
			LogMessage:      "Unable to find the specified POS transaction reference",
			CustomerMessage: "Unable to find the specified POS transaction reference",
		},
		"FPSA02": {
			TxnStatus:       StatusFailed,
			LogMessage:      "This contract has already been completed",
			CustomerMessage: "This contract has already been completed",
		},
		"FPSA03": {
			TxnStatus:       StatusFailed,
			LogMessage:      "This Oxipay contract has previously been cancelled and all payments collected have been refunded to the customer",
			CustomerMessage: "This Oxipay contract has previously been cancelled and all payments collected have been refunded to the customer",
		},
		"FPSA04": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Sales adjustment cannot be processed for this amount",
			CustomerMessage: "Sales adjustment cannot be processed for this amount",
		},
		"FPSA05": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Unable to process a sales adjustment for this contract. Please contact Merchant Services during business hours for further information",
			CustomerMessage: "Unable to process a sales adjustment for this contract. Please contact Merchant Services during business hours for further information",
		},
		"FPSA06": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Sales adjustment cannot be processed. Please call Oxipay Collections",
			CustomerMessage: "Sales adjustment cannot be processed. Please call Oxipay Collections",
		},
		"FPSA07": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Sales adjustment cannot be processed at this store",
			CustomerMessage: "Sales adjustment cannot be processed at this store",
		},
		"FPSA08": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Sales adjustment cannot be processed for this transaction. Duplicate receipt number found.",
			CustomerMessage: "Sales adjustment cannot be processed for this transaction. Duplicate receipt number found.",
		},
		"FPSA09": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Amount must be greater than 0.",
			CustomerMessage: "Amount must be greater than 0.",
		},
		"EAUT01": {
			TxnStatus:  StatusFailed,
			LogMessage: "Authentication to gateway error",
			CustomerMessage: `The request to Humm was not what we were expecting. 
			You can try again with a different Payment Code. 
			Please contact pit@shophumm.com.au for further support`,
		},
		"EVAL01": {
			TxnStatus:  StatusFailed,
			LogMessage: "Request is invalid",
			CustomerMessage: `The request to Humm was what we were expecting. 
			You can try again with a different Payment Code. 
			Please contact pit@shophumm.com.au for further support`,
		},
		"ESIG01": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Signature mismatch error. Has the terminal changed, try removing the key for the device? ",
			CustomerMessage: `Please contact pit@shophumm.com.au for further support`,
		},
		"EISE01": {
			TxnStatus:       StatusFailed,
			LogMessage:      "Server Error",
			CustomerMessage: `Please contact pit@shophumm.com.au for further support`,
		},
	}

	return func(key string) *ResponseCode {
		// check to make sure we know what the response is
		ret := innerMap[key]

		if ret == nil {
			return innerMap[defaultResponseCode]
		}
		return ret
	}
}
