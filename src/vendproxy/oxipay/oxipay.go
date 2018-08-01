package oxipay

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/vend/peg/src/vendproxy/oxipay"
	"github.com/vend/peg/src/vendproxy/vend"
)

// OxpayGateway Default URL for the Oxipay Gateway @todo get from config
var OxpayGateway = "https://testpos.oxipay.com.au/webapi/v1/"

// Db connection to database
var Db *sql.DB

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

// Ping returns pong
func Ping() string {
	return "pong"
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

// RegisterPosDevice is used to register a new vend terminal
func RegisterPosDevice(payload *OxipayRegistrationPayload) (*OxipayResponse, error) {
	// @todo move to configuration
	var registerURL = OxpayGateway + "/CreateKey"
	var err error

	jsonValue, _ := json.Marshal(payload)

	log.Printf("POST to URL %s", registerURL)
	log.Printf("Register POS Device Payload: %s", string(jsonValue))

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

func (t Terminal) save(user string) (bool, error) {

	if Db == nil {
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

	stmt, err := Db.Prepare(query)

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

func getRegisteredTerminal(r *vend.VendPaymentRequest) (*Terminal, error) {

	if Db == nil {
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

	rows, err := Db.Query(sql, r.Origin, r.RegisterID)

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

// SignMessage will generate an HMAC of the plaintext
func SignMessage(plainText string, signingKey string) string {

	key := []byte(signingKey)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(plainText))

	return hex.EncodeToString(mac.Sum(nil))
}

func (payload *oxipay.OxipayRegistrationPayload) validate() error {

	if payload == nil {
		return errors.New("payload is empty")
	}

	return nil
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

// func CheckMAC(message, messageMAC, key []byte) bool {
// 	mac := hmac.New(sha256.New, key)
// 	mac.Write(message)

// 	expectedMAC := mac.Sum(nil)
// 	return hmac.Equal(messageMAC, expectedMAC)
// }
