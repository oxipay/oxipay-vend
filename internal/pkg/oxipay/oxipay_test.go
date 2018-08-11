package oxipay

import (
	"encoding/json"
	"fmt"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

func TestProcessAuthorisationResponse(t *testing.T) {
	x := ProcessAuthorisationResponses()("EVAL02")
	if x == nil || x.TxnStatus != StatusFailed {
		t.Errorf("unexpected response: got %v want %v", x.TxnStatus, StatusFailed)
	}

	y := ProcessAuthorisationResponses()("SPRA01")
	if y == nil || y.TxnStatus != StatusApproved {
		t.Errorf("unexpected response: got %v want %v", y.TxnStatus, StatusFailed)
	}
}

func TestGenerateSignature(t *testing.T) {

	responsePayload := `{"x_key":"hEz3dnWwEWuo","x_status":"Success","x_code":"SCRK01","x_message":"Success","signature":"5385041e76753e1b6e7ac09d52c6363854f1df4e79a7aa01c44f2d4618063483","tracking_data":null}`
	oxipayResponse := new(OxipayResponse)

	err := json.Unmarshal([]byte(responsePayload), oxipayResponse)
	if err != nil {
		t.Error("Unable to unmarshall response")
	}

	plainText := GeneratePlainTextSignature(oxipayResponse)
	fmt.Println(plainText)
	signature := SignMessage(plainText, "szUb4YwzQNXn")
	fmt.Printf("Generated Signature: %s ", signature)

	expectedSignature := "5385041e76753e1b6e7ac09d52c6363854f1df4e79a7aa01c44f2d4618063483"

	if signature != expectedSignature {
		t.Errorf("Expected %s, got %s", expectedSignature, signature)
	}
}

func TestAuthenticate(t *testing.T) {

	responsePayload := `{"x_key":"hEz3dnWwEWuo","x_status":"Success","x_code":"SCRK01","x_message":"Success","signature":"5385041e76753e1b6e7ac09d52c6363854f1df4e79a7aa01c44f2d4618063483","tracking_data":null}`
	oxipayResponse := new(OxipayResponse)

	err := json.Unmarshal([]byte(responsePayload), oxipayResponse)
	if err != nil {
		t.Error("Unable to unmarshall response")
	}
	if oxipayResponse.Authenticate("szUb4YwzQNXn") == false {
		t.Error("Authenticate failed and should be true")
	}
}
